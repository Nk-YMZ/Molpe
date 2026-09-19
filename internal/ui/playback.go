package ui

import (
	tea "charm.land/bubbletea/v2"

	"molpe/internal/mpris"
	"molpe/internal/netease"
)

type songURLFetchedMsg struct {
	song netease.Song
	url  *netease.SongURL
	err  error
	seq  int // 请求序号，用于丢弃过期响应
}

type mprisEventMsg mpris.Event

// playerEndedMsg 表示当前曲目自然播完（mpv end-file/eof），应自动连播。
type playerEndedMsg struct{}

// playCmd 拉取歌曲播放地址。每次发起都会递增请求序号，
// 快速连续切歌时先发出的慢响应会被 handleSongURL 按序号丢弃。
func (m *Model) playCmd(song netease.Song) tea.Cmd {
	m.playSeq++
	seq := m.playSeq
	return func() tea.Msg {
		u, err := m.client.SongURL(song.ID, m.quality)
		return songURLFetchedMsg{song: song, url: u, err: err, seq: seq}
	}
}

// listenMprisCmd 等待一次桌面控制事件；服务仅随进程退出回收，
// 进程退出前该监听 goroutine 由 Bubble Tea 一并结束。
func listenMprisCmd(svc *mpris.Service) tea.Cmd {
	if svc == nil {
		return nil
	}
	return func() tea.Msg {
		ev, ok := <-svc.Events()
		if !ok {
			return nil
		}
		return mprisEventMsg(ev)
	}
}

// listenEndCmd 等待一次 mpv 自然播完事件。
func listenEndCmd(endCh <-chan struct{}) tea.Cmd {
	return func() tea.Msg {
		<-endCh
		return playerEndedMsg{}
	}
}
