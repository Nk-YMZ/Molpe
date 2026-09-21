package server

import (
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

// gapExpiredResult 歌曲间隔到期；seq 用于丢弃切歌/点歌后的残留定时器。
type gapExpiredResult struct {
	seq int
}

// playSong 拉取歌曲播放地址（异步）。每次发起都递增请求序号，
// 快速连续切歌时先发出的慢响应会被 handleSongURL 按序号丢弃。
func (s *Server) playSong(song netease.Song) {
	s.playSeq++
	seq := s.playSeq
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
	if r.err != nil {
		s.pushNote(r.err.Error(), true)
		return
	}
	if s.player == nil {
		p, err := player.Start()
		if err != nil {
			s.pushNote(err.Error(), true)
			return
		}
		p.SetEndCallback(func() {
			select {
			case s.endCh <- struct{}{}:
			default:
			}
		})
		if err := p.SetVolume(s.volume); err != nil {
			s.pushNote(err.Error(), true)
		}
		s.player = p
	}
	if err := s.player.Play(r.url.URL); err != nil {
		s.pushNote(err.Error(), true)
		return
	}
	// mpv 的 pause 属性跨 loadfile 保持，这里无条件把暂停状态同步给 mpv：
	// 暂停中切歌后新曲目仍为暂停，明确开播时解除上一首遗留的暂停。
	if err := s.player.SetPause(s.paused); err != nil {
		s.pushNote(err.Error(), true)
	}
	s.playing = &ipc.Playing{Song: r.song, Level: r.url.Level}
	s.anchor.set(0, s.paused)
	// 切歌后重置歌词缓存并异步拉取新歌词。
	s.lyrics = nil
	s.lyricsSongID = 0
	s.publishState()
	s.pushState()
	s.fetchLyrics(r.song.ID)
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
func (s *Server) handleLyrics(r lyricsResult) {
	if s.playing == nil || r.songID != s.playing.Song.ID {
		return
	}
	if r.err != nil {
		s.pushNote("获取歌词失败："+r.err.Error(), false)
		return
	}
	s.lyrics = r.lines
	s.lyricsSongID = r.songID
	s.push(ipc.NewMessage(0, ipc.TLyrics, ipc.LyricsMsg{SongID: r.songID, Lines: r.lines}))
}

// toggle 切换播放/暂停；恢复待播状态下播放器尚未启动时，先拉取地址开始播放。
func (s *Server) toggle() {
	if s.playing == nil {
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

// publishState 向 MPRIS 推送当前曲目元数据与播放状态。
func (s *Server) publishState() {
	if s.mpris == nil {
		return
	}
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
	case s.paused:
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
		if s.playing != nil {
			s.setPaused(true)
		}
	case mpris.EventNext:
		s.nextSong()
	case mpris.EventPrevious:
		s.prevSong()
	}
}
