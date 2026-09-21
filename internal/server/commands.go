package server

import (
	"errors"

	"molpe/internal/ipc"
	"molpe/internal/netease"
	"molpe/internal/queue"
)

// playlistsResult 歌单列表请求结果；id 与请求配对。
type playlistsResult struct {
	id        int
	playlists []netease.Playlist
	err       error
}

// songsResult 歌单歌曲请求结果；id 与请求配对。
type songsResult struct {
	id         int
	playlistID int64
	songs      []netease.Song
	err        error
}

// handleMessage 分发前端命令与数据请求。
func (s *Server) handleMessage(msg ipc.Message) {
	switch msg.Type {
	case ipc.TGetPlaylists:
		s.handleGetPlaylists(msg.ID)
	case ipc.TGetSongs:
		var c ipc.GetSongsCmd
		if msg.DecodeData(&c) == nil {
			s.handleGetSongs(msg.ID, c.PlaylistID)
		}
	case ipc.TPlaySong:
		var c ipc.PlaySongCmd
		if msg.DecodeData(&c) == nil {
			// 手动点歌：更新歌单队列并记录历史；下一首播放队列保留。
			// 点歌是明确的播放意图，解除暂停状态与歌曲间隔。
			s.cancelGap()
			s.queue.SetPlaylist(c.Playlist)
			s.queue.Play(c.Song)
			s.paused = false
			s.saveQueue()
			s.playSong(c.Song)
		}
	case ipc.TPlayNext:
		var c ipc.PlayNextCmd
		if msg.DecodeData(&c) == nil {
			s.queue.PlayNext(c.Song)
			s.saveQueue()
			s.pushNote("已加入下一首播放："+c.Song.Name, false)
			s.pushState()
		}
	case ipc.TToggle:
		s.toggle()
	case ipc.TNext:
		s.nextSong()
	case ipc.TPrev:
		s.prevSong()
	case ipc.TCycleMode:
		s.cycleMode()
	case ipc.TVolumeDelta:
		var c ipc.VolumeCmd
		if msg.DecodeData(&c) == nil {
			s.adjustVolume(c.Delta)
		}
	case ipc.TRemove:
		var c ipc.RemoveCmd
		if msg.DecodeData(&c) == nil {
			s.handleRemove(c)
		}
	case ipc.TQRNew:
		s.newQR()
	case ipc.TSetTheme:
		var c ipc.ThemeCmd
		if msg.DecodeData(&c) == nil {
			// 主题名由后端随配置统一落盘，避免前后端双写配置互相覆盖。
			s.cfg.Theme = c.Name
			s.saveConfig()
		}
	case ipc.TShutdown:
		s.shutdown()
	}
}

// handleGetPlaylists 异步拉取用户歌单并应答。
func (s *Server) handleGetPlaylists(id int) {
	account := s.account
	go func() {
		if account == nil {
			s.results <- playlistsResult{id: id, err: errors.New("未登录")}
			return
		}
		playlists, err := s.client.UserPlaylists(account.ID)
		s.results <- playlistsResult{id: id, playlists: playlists, err: err}
	}()
}

// handleGetSongs 异步拉取歌单歌曲并应答。
func (s *Server) handleGetSongs(id int, playlistID int64) {
	go func() {
		songs, err := s.client.PlaylistSongs(playlistID)
		s.results <- songsResult{id: id, playlistID: playlistID, songs: songs, err: err}
	}()
}

// cycleMode 在顺序播放 → 列表循环 → 随机播放之间切换。
func (s *Server) cycleMode() {
	switch s.queue.Mode() {
	case queue.ModeSequential:
		s.queue.SetMode(queue.ModeLoop)
	case queue.ModeLoop:
		s.queue.SetMode(queue.ModeRandom)
	default:
		s.queue.SetMode(queue.ModeSequential)
	}
	s.saveQueue()
	s.pushState()
}

// adjustVolume 按步进调整音量（0-100），应用到播放器与桌面环境；
// 配置统一在退出时落盘，运行期间不写盘。
func (s *Server) adjustVolume(delta int) {
	v := max(0, min(100, s.volume+delta))
	if v == s.volume {
		return
	}
	s.volume = v
	s.cfg.Volume = &v
	if s.player != nil {
		if err := s.player.SetVolume(v); err != nil {
			s.pushNote(err.Error(), true)
		}
	}
	if s.mpris != nil {
		s.mpris.SetVolume(float64(v) / 100)
	}
	s.pushState()
}

// handleRemove 删除队列条目：历史/下一首队列/歌单后续仅从对应队列移除；
// 删除当前曲目则自动跳转下一首，无可播歌曲时停止播放。
func (s *Server) handleRemove(c ipc.RemoveCmd) {
	switch c.Section {
	case ipc.SectionHistory:
		s.queue.RemoveHistory(c.Index)
	case ipc.SectionNextUp:
		s.queue.RemoveNextUp(c.Index)
	case ipc.SectionPlaylist:
		s.queue.RemovePlaylist(c.Index)
	case ipc.SectionCurrent:
		s.removeCurrent()
		return
	default:
		return
	}
	s.saveQueue()
	s.pushState()
}

// removeCurrent 移除当前播放曲并自动跳转下一首；暂停状态随之解除。
func (s *Server) removeCurrent() {
	s.cancelGap()
	s.queue.RemoveCurrent()
	song, ok := s.queue.Next()
	s.saveQueue()
	if !ok {
		// 没有可播放的下一首：停止播放并关闭播放器。
		if s.player != nil {
			s.player.Close()
			s.player = nil
		}
		s.playing = nil
		s.paused = false
		s.anchor.set(0, true)
		s.lyrics = nil
		s.lyricsSongID = 0
		s.publishState()
		s.pushState()
		s.pushNote("队列已播完", false)
		return
	}
	s.paused = false // 删除当前曲即切歌，暂停状态随之解除
	s.playSong(song)
}

// handleResult 处理异步请求结果。
func (s *Server) handleResult(res any) {
	switch r := res.(type) {
	case songURLResult:
		s.handleSongURL(r)
	case lyricsResult:
		s.handleLyrics(r)
	case gapExpiredResult:
		s.gapExpired(r.seq)
	case loginResult:
		s.handleLogin(r)
	case qrNewResult:
		s.handleQRNew(r)
	case qrTickResult:
		s.handleQRTick(r)
	case qrCheckResult:
		s.handleQRCheck(r)
	case playlistsResult:
		errStr := ""
		if r.err != nil {
			errStr = r.err.Error()
		}
		s.reply(r.id, ipc.TPlaylists, ipc.PlaylistsMsg{Playlists: r.playlists, Err: errStr})
	case songsResult:
		errStr := ""
		if r.err != nil {
			errStr = r.err.Error()
		}
		s.reply(r.id, ipc.TSongs, ipc.SongsMsg{PlaylistID: r.playlistID, Songs: r.songs, Err: errStr})
	}
}
