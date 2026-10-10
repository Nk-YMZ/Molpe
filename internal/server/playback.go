package server

import (
	"errors"
	"strings"
	"sync"
	"time"

	"molpe/internal/ipc"
	"molpe/internal/mpris"
	"molpe/internal/netease"
	"molpe/internal/player"
)

// posAnchor 播放位置锚点：记录最近一次已知位置与其时刻，
// 之后按墙钟外推。播放中无需任何定时器或 mpv 轮询，
// 仅在播放/暂停/切歌等事件点刷新，桌面环境轮询 MPRIS Position
// 时纯内存计算，不打醒 mpv。
type posAnchor struct {
	mu     sync.Mutex
	pos    float64 // 锚点位置（秒）
	at     time.Time
	paused bool
}

// set 刷新锚点。
func (a *posAnchor) set(pos float64, paused bool) {
	a.mu.Lock()
	a.pos = pos
	a.at = time.Now()
	a.paused = paused
	a.mu.Unlock()
}

// get 返回当前播放位置（秒），供 MPRIS Position 查询与状态快照使用。
func (a *posAnchor) get() (float64, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.paused || a.at.IsZero() {
		return a.pos, nil
	}
	return a.pos + time.Since(a.at).Seconds(), nil
}

// songURLResult 播放地址请求结果；seq 用于丢弃快速切歌时的过期响应。
type songURLResult struct {
	seq  int
	song netease.Song
	url  *netease.SongURL
	err  error
}

// lyricsResult 歌词获取结果；songID 用于丢弃切歌后的过期响应。
type lyricsResult struct {
	songID int64
	lines  []netease.LyricLine
	err    error
}

// clampDur 将 v 钳制到 [lo, hi]。
func clampDur(v, lo, hi time.Duration) time.Duration {
	return min(max(v, lo), hi)
}

// seekBy 相对跳转播放位置：钳制在曲目时长范围内（越过开头从头播放，
// 不切换曲目；越过末尾由 mpv 自然播完进入下一首）。加载中/待播重试中
// 没有可定位的音频流，直接忽略；歌曲间隔中视为对当前曲的主动操作：
// 累计目标偏移（多次按键叠加），取消间隔立即开播并从该位置开始。
func (s *Server) seekBy(delta time.Duration) {
	if s.playing == nil || s.playing.Song.Duration <= 0 {
		return
	}
	if s.gapSong != nil {
		s.startOffset = clampDur(s.startOffset+delta, 0, s.playing.Song.Duration)
		s.playGapSong()
		s.startOffsetSeq = s.playSeq // 仅本次开播消费该定位
		return
	}
	if s.loading || s.retrySong != nil {
		return
	}
	cur, _ := s.anchor.get()
	s.seekTo(time.Duration(cur*float64(time.Second)) + delta)
}

// seekTo 跳转到绝对播放位置（钳制在曲目时长范围内）。
func (s *Server) seekTo(pos time.Duration) {
	if s.playing == nil || s.playing.Song.Duration <= 0 {
		return
	}
	if s.gapSong != nil {
		s.startOffset = clampDur(pos, 0, s.playing.Song.Duration)
		s.playGapSong()
		s.startOffsetSeq = s.playSeq
		return
	}
	if s.loading || s.retrySong != nil || s.player == nil {
		return
	}
	pos = clampDur(pos, 0, s.playing.Song.Duration)
	if err := s.player.Seek(pos); err != nil {
		s.pushNote("跳转失败: "+err.Error(), true)
		return
	}
	// 更新锚点：前端进度条与 MPRIS 位置立即对齐；暂停中跳转保持暂停。
	s.anchor.set(pos.Seconds(), s.paused)
	if s.mpris != nil {
		s.mpris.Seeked(pos.Microseconds())
	}
	s.pushState()
}

// mprisSeekResult 桌面组件的相对跳转请求（Seek 方法，微秒偏移）。
type mprisSeekResult struct{ offsetMicros int64 }

// mprisSetPositionResult 桌面组件的绝对定位请求（SetPosition 方法）。
type mprisSetPositionResult struct {
	trackID   string
	posMicros int64
}

// gapExpiredResult 歌曲间隔到期；seq 用于丢弃切歌/点歌后的残留定时器。
type gapExpiredResult struct {
	seq int
}

// retryResult 播放重试定时器到期；seq 用于丢弃切歌/点歌后的残留定时器。
type retryResult struct {
	seq int
}

// playerErrorResult mpv 报告流播放中途失败（end-file reason=error）。
type playerErrorResult struct{}

// retryBackoff 播放失败后的自动重试间隔；用尽后固定为最后一个值，
// 持续重试直到成功或用户切歌。重试本身即网络探测：恢复后最多一个
// 间隔内自动开播。
var retryBackoff = []time.Duration{
	2 * time.Second, 5 * time.Second, 10 * time.Second, 20 * time.Second, 30 * time.Second,
}

// playSong 切换到指定歌曲并拉取播放地址（异步）。切歌立即生效：旧曲停止
// 出声、当前曲切换到位并推送（loading 加载中，位置冻结在 0），界面与
// MPRIS 所见与实际出声严格一致；地址就绪后按意图位（paused）开播，
// 失败由 handlePlayFailure 收敛为待播重试。每次发起都递增请求序号，
// 快速连续切歌时先发出的慢响应会被 handleSongURL 按序号丢弃。
func (s *Server) playSong(song netease.Song) {
	s.playSeq++
	seq := s.playSeq
	s.loading = true
	s.playing = &ipc.Playing{Song: song, Level: s.quality}
	s.anchor.set(0, true) // 加载期间位置冻结
	s.lyrics = nil
	s.lyricsSongID = 0
	// 切歌终止旧曲目的歌词重试。
	s.lyricsAttempt = 0
	if s.lyricsTimer != nil {
		s.lyricsTimer.Stop()
		s.lyricsTimer = nil
	}
	if s.player != nil {
		// 旧曲立即静音；播放器已不可用时关闭，开播时重建。
		if err := s.player.Stop(); err != nil {
			s.player.Close()
			s.player = nil
			s.playerExited = nil
		}
	}
	s.publishState()
	s.pushState()
	quality := s.quality
	go func() {
		u, err := s.client.SongURL(song.ID, quality)
		s.results <- songURLResult{seq: seq, song: song, url: u, err: err}
	}()
}

// handleSongURL 处理播放地址获取结果：启动播放器（如需要）并播放。
func (s *Server) handleSongURL(r songURLResult) {
	if r.seq != s.playSeq {
		return // 过期响应
	}
	s.loading = false
	if r.err != nil {
		s.handlePlayFailure(r.song, r.err)
		return
	}
	// 开播定位仅属于发起它的那次切歌（序号匹配才生效，用后即焚）。
	startOffset := time.Duration(0)
	if s.startOffset > 0 && r.seq == s.startOffsetSeq {
		startOffset = s.startOffset
	}
	s.startOffset, s.startOffsetSeq = 0, 0
	if s.player == nil {
		p, err := player.Start()
		if err != nil {
			s.handlePlayFailure(r.song, err)
			return
		}
		p.SetEndCallback(func(reason string) {
			if reason == "error" {
				// 流播放中途失败（网络中断、CDN 不可达等）。
				select {
				case s.results <- playerErrorResult{}:
				default:
				}
				return
			}
			select {
			case s.endCh <- struct{}{}:
			default:
			}
		})
		s.playerExited = p.Exited()
		if err := p.SetVolume(s.volume); err != nil {
			s.pushNote(err.Error(), true)
		}
		s.player = p
	}
	var playErr error
	if startOffset > 0 {
		// 间隔中快进/快退产生的开播定位：加载即从目标位置开始。
		playErr = s.player.PlayFrom(r.url.URL, startOffset)
	} else {
		playErr = s.player.Play(r.url.URL)
	}
	if playErr != nil {
		s.handlePlayFailure(r.song, playErr)
		return
	}
	// 按切歌时定下的播放意图同步暂停状态（加载中用户可能按了暂停；
	// mpv 的 pause 属性跨 loadfile 保持，需无条件同步）。
	if err := s.player.SetPause(s.paused); err != nil {
		s.pushNote(err.Error(), true)
	}
	// 开播成功：清除可能存在的失败重试状态；此前处于重试流程时提示恢复。
	recovered := s.retryAttempt > 0
	s.cancelRetry()
	s.lyricsAttempt = 0 // 新曲目：歌词重试计数归零
	// 切歌时已就位：回填实际音质，并以加载时刻重置位置锚点。
	s.playing.Level = r.url.Level
	s.anchor.set(startOffset.Seconds(), s.paused)
	s.publishState()
	s.pushState()
	s.fetchLyrics(r.song.ID)
	if recovered {
		s.pushNote("已恢复播放："+r.song.Name, false)
	}
}

// fetchLyrics 异步拉取歌曲歌词。
func (s *Server) fetchLyrics(songID int64) {
	translation := s.lyricTr
	go func() {
		lines, err := s.client.Lyrics(songID, translation)
		s.results <- lyricsResult{songID: songID, lines: lines, err: err}
	}()
}

// handleLyrics 处理歌词获取结果；切歌后的过期响应直接丢弃。
// 失败时自动重试（固定间隔、次数上限），成功或切歌后终止。
func (s *Server) handleLyrics(r lyricsResult) {
	if s.playing == nil || r.songID != s.playing.Song.ID {
		return
	}
	if r.err != nil {
		if s.lyricsAttempt == 0 {
			s.pushNote("获取歌词失败："+r.err.Error(), false)
		}
		s.scheduleLyricsRetry(r.songID)
		return
	}
	s.lyricsAttempt = 0
	s.lyrics = r.lines
	s.lyricsSongID = r.songID
	s.push(ipc.NewMessage(0, ipc.TLyrics, ipc.LyricsMsg{SongID: r.songID, Lines: r.lines}))
}

// lyricsRetryInterval 歌词重试间隔；lyricsMaxRetries 为重试次数上限
// （超过后等待用户全局刷新或切歌）。
const (
	lyricsRetryInterval = 5 * time.Second
	lyricsMaxRetries    = 3
)

// lyricsRetryResult 歌词重试定时器到期；songID 用于丢弃切歌后的残留。
type lyricsRetryResult struct{ songID int64 }

// scheduleLyricsRetry 安排一次歌词自动重试。
func (s *Server) scheduleLyricsRetry(songID int64) {
	if s.lyricsAttempt >= lyricsMaxRetries {
		return
	}
	s.lyricsAttempt++
	if s.lyricsTimer != nil {
		s.lyricsTimer.Stop()
	}
	s.lyricsTimer = time.AfterFunc(lyricsRetryInterval, func() {
		s.results <- lyricsRetryResult{songID: songID}
	})
}

// handleLyricsRetry 歌词重试定时器到期：仍缺歌词且未切歌、未处于
// 加载/待播重试中时重新拉取。
func (s *Server) handleLyricsRetry(r lyricsRetryResult) {
	if s.playing == nil || s.loading || s.retrySong != nil ||
		r.songID != s.playing.Song.ID || s.lyricsSongID == r.songID {
		return
	}
	s.fetchLyrics(r.songID)
}

// toggle 切换播放/暂停；恢复待播状态下播放器尚未启动时，先拉取地址开始播放。
func (s *Server) toggle() {
	if s.playing == nil {
		return
	}
	if s.retrySong != nil {
		// 等待网络重试中：播放键即跳过等待，立即重试。
		s.retryNow()
		return
	}
	if s.loading {
		// 加载中没有可操作的音频：仅翻转播放意图，地址就绪后按意图开播。
		s.paused = !s.paused
		s.pushState()
		return
	}
	if s.gapSong != nil {
		// 歌曲间隔中没有可暂停的内容：播放键即跳过等待，立即开播下一首。
		s.playGapSong()
		return
	}
	if s.player == nil {
		s.paused = false
		s.playSong(s.playing.Song)
		return
	}
	s.setPaused(!s.paused)
}

// setPaused 切换暂停状态并同步桌面环境。
func (s *Server) setPaused(paused bool) {
	if s.player == nil {
		return
	}
	if err := s.player.SetPause(paused); err != nil {
		s.pushNote(err.Error(), true)
		return
	}
	// 在事件点向 mpv 查询一次精确位置作为新锚点，之后纯内存外推。
	pos, err := s.player.Position()
	if err != nil {
		pos, _ = s.anchor.get()
	}
	s.paused = paused
	s.anchor.set(pos, paused)
	s.publishState()
	s.pushState()
}

// nextSong 播放队列中的下一首（历史前进 → 下一首队列 → 播放模式）。
// 切歌是明确的播放意图，暂停状态与歌曲间隔随之解除。
func (s *Server) nextSong() {
	s.cancelGap()
	s.cancelRetry()
	song, ok := s.queue.Next()
	if !ok {
		s.pushNote("没有可播放的下一首", false)
		return
	}
	s.paused = false
	s.saveQueue()
	s.playSong(song)
}

// prevSong 沿历史队列回退一首；暂停状态与歌曲间隔随之解除。
func (s *Server) prevSong() {
	s.cancelGap()
	s.cancelRetry()
	song, ok := s.queue.Prev()
	if !ok {
		s.pushNote("没有更早的播放记录", false)
		return
	}
	s.paused = false
	s.saveQueue()
	s.playSong(song)
}

// handleEnded 当前曲目自然播完：按队列规则连播；
// 无可播歌曲（顺序模式到末尾）时停止。
func (s *Server) handleEnded() {
	s.cancelRetry() // 防御：重试等待期间不应收到自然播完事件
	song, ok := s.queue.Next()
	s.saveQueue()
	if !ok {
		s.playing = nil
		s.paused = false
		s.anchor.set(0, true)
		s.lyrics = nil
		s.lyricsSongID = 0
		s.publishState()
		s.pushState()
		return
	}
	if s.songGap > 0 {
		s.startGap(song)
		return
	}
	s.playSong(song)
}

// startGap 进入歌曲间隔：下一首置为待播（暂停）状态，并安排到期自动开播。
// 间隔期间除该一次性定时器外无其他后台活动；任何播放意图（播放键/切歌/
// 点歌）都会取消间隔立即开播。
func (s *Server) startGap(song netease.Song) {
	s.gapSong = &song
	s.gapSeq++
	seq := s.gapSeq
	s.gapAt = time.Now()
	s.playing = &ipc.Playing{Song: song, Level: s.quality}
	s.paused = true
	s.anchor.set(0, true)
	s.lyrics = nil
	s.lyricsSongID = 0
	s.publishState()
	s.pushState()
	s.gapTimer = time.AfterFunc(time.Duration(s.songGap)*time.Second, func() {
		s.results <- gapExpiredResult{seq: seq}
	})
}

// cancelGap 取消进行中的歌曲间隔：递增序号并停止定时器。
func (s *Server) cancelGap() {
	s.gapSong = nil
	s.gapSeq++
	if s.gapTimer != nil {
		s.gapTimer.Stop()
		s.gapTimer = nil
	}
}

// gapExpired 间隔到期：开播待播的下一首；过期定时器（序号不符）直接丢弃。
func (s *Server) gapExpired(seq int) {
	if seq != s.gapSeq || s.gapSong == nil {
		return
	}
	s.playGapSong()
}

// playGapSong 跳过剩余间隔，立即开播待播的下一首。
func (s *Server) playGapSong() {
	song := *s.gapSong
	s.cancelGap()
	s.paused = false
	s.playSong(song)
}

// handlePlayFailure 播放启动失败（拉取播放地址或加载播放器出错）：
// 停止旧曲出声，当前曲收敛为待播（暂停）状态，保证界面、MPRIS 与队列
// 三者一致，并安排自动重试。首次失败给出明确提示，后续重试静默进行
// （状态栏持续显示“等待网络”），成功开播时提示恢复。
func (s *Server) handlePlayFailure(song netease.Song, err error) {
	if s.player != nil {
		if stopErr := s.player.Stop(); stopErr != nil {
			// 播放器已不可用：关闭，重试时重建。
			s.player.Close()
			s.player = nil
			s.playerExited = nil
		}
	}
	first := s.retryAttempt == 0 && s.retrySong == nil
	s.retrySong = &song
	s.playing = &ipc.Playing{Song: song, Level: s.quality}
	s.paused = true
	s.anchor.set(0, true)
	s.lyrics = nil
	s.lyricsSongID = 0
	s.publishState()
	s.pushState()
	if first {
		s.pushNote("播放失败："+err.Error()+"；等待网络，将自动重试", true)
	}
	s.scheduleRetry()
}

// scheduleRetry 安排下一次自动重试，间隔按 retryBackoff 退避。
func (s *Server) scheduleRetry() {
	if s.retryTimer != nil {
		s.retryTimer.Stop()
	}
	wait := retryBackoff[min(s.retryAttempt, len(retryBackoff)-1)]
	s.retryAttempt++
	seq := s.retrySeq
	s.retryTimer = time.AfterFunc(wait, func() {
		s.results <- retryResult{seq: seq}
	})
}

// cancelRetry 取消自动重试：递增序号丢弃残留定时器，清空退避进度。
func (s *Server) cancelRetry() {
	s.retrySong = nil
	s.retryAttempt = 0
	s.retrySeq++
	if s.retryTimer != nil {
		s.retryTimer.Stop()
		s.retryTimer = nil
	}
}

// handleRetry 重试定时器到期：立即重试待播曲目；过期定时器直接丢弃。
func (s *Server) handleRetry(r retryResult) {
	if r.seq != s.retrySeq || s.retrySong == nil {
		return
	}
	s.retryNow()
}

// retryNow 跳过剩余等待，立即重试待播曲目（恢复为播放意图）；
// 失败会重新进入待播重试状态并安排下一次重试。
func (s *Server) retryNow() {
	if s.retrySong == nil {
		return
	}
	song := *s.retrySong
	s.retrySong = nil // 重试进行中；失败时由 handlePlayFailure 重新置回
	if s.retryTimer != nil {
		s.retryTimer.Stop()
		s.retryTimer = nil
	}
	s.paused = false
	s.playSong(song)
}

// handlePlayerError 流播放中途失败（end-file reason=error，如网络中断、
// CDN 不可达）：收敛为待播重试，网络恢复后自动重新拉取地址开播。
// 切歌/加载中的旧流事件、已在重试管道中、或用户暂停期间的事件不处理。
func (s *Server) handlePlayerError() {
	if s.playing == nil || s.loading || s.retrySong != nil || s.paused {
		return
	}
	s.handlePlayFailure(s.playing.Song, errors.New("播放中断（连接失败）"))
}

// handlePlayerExited mpv 进程异常退出：清理并按当前状态接入恢复路径——
// 播放中异常退出接入待播重试管道（自动重建播放器恢复播放）；
// 加载中或已在重试时无需处理（开播/重试时自会重建）；
// 已暂停时仅提示，下次播放时重建。
func (s *Server) handlePlayerExited() {
	s.playerExited = nil
	if s.player != nil {
		s.player.Close() // 进程已退出，仅做状态与 socket 清理
		s.player = nil
	}
	switch {
	case s.retrySong != nil, s.loading:
		// 重试/开播路径会重建播放器。
	case s.playing != nil && !s.paused:
		s.handlePlayFailure(s.playing.Song, errors.New("播放器异常退出"))
	case s.playing != nil:
		s.pushNote("播放器异常退出，按播放键重新开始", true)
	}
}

// publishState 向 MPRIS 推送当前曲目元数据与播放状态。
// CanSeek 随状态联动：只有存在已加载音频流（播放中或暂停）时才接受跳转。
func (s *Server) publishState() {
	if s.mpris == nil {
		return
	}
	s.mpris.SetCanSeek(s.playing != nil && !s.loading &&
		s.retrySong == nil && s.gapSong == nil && s.player != nil)
	if s.playing != nil {
		song := s.playing.Song
		s.mpris.SetTrack(mpris.Track{
			ID:       song.ID,
			Title:    song.Name,
			Artists:  strings.Split(song.Artists, "/"),
			Album:    song.Album,
			Duration: song.Duration,
			ArtURL:   song.CoverURL,
		})
	}
	switch {
	case s.playing == nil:
		s.mpris.SetStatus("Stopped")
	case s.loading || s.paused:
		// 加载中没有音频出声（位置冻结），与暂停一样对外报 Paused。
		s.mpris.SetStatus("Paused")
	default:
		s.mpris.SetStatus("Playing")
	}
}

// handleMprisEvent 处理桌面环境（媒体键、KDE 媒体组件）发来的控制事件。
func (s *Server) handleMprisEvent(ev mpris.Event) {
	switch ev {
	case mpris.EventPlayPause:
		s.toggle()
	case mpris.EventPlay:
		if s.retrySong != nil {
			// 等待网络重试中：播放键即跳过等待，立即重试。
			s.retryNow()
			return
		}
		if s.loading {
			// 加载中：仅确立播放意图，地址就绪后开播。
			s.paused = false
			s.pushState()
			return
		}
		if s.gapSong != nil {
			// 歌曲间隔中：播放键即跳过等待，立即开播下一首。
			s.playGapSong()
			return
		}
		if s.playing != nil {
			if s.player == nil {
				// 待播状态下尚未启动播放器，直接拉取地址开始播放。
				s.paused = false
				s.playSong(s.playing.Song)
				return
			}
			s.setPaused(false)
		}
	case mpris.EventPause, mpris.EventStop:
		if s.retrySong != nil {
			// 等待重试期间收到暂停：放弃自动恢复，保持待播状态。
			s.cancelRetry()
			s.pushState()
			return
		}
		if s.loading {
			// 加载中：仅记录暂停意图，地址就绪后保持暂停。
			s.paused = true
			s.pushState()
			return
		}
		if s.playing != nil {
			s.setPaused(true)
		}
	case mpris.EventNext:
		s.nextSong()
	case mpris.EventPrevious:
		s.prevSong()
	}
}
