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
}

type mprisEventMsg mpris.Event

// playerEndedMsg 表示当前曲目自然播完（mpv end-file/eof），应自动连播。
type playerEndedMsg struct{}

// listenMprisCmd 等待一次桌面控制事件；服务关闭后返回 nil 消息并停止监听。
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

func fetchSongURLCmd(c *netease.Client, song netease.Song, quality string) tea.Cmd {
	return func() tea.Msg {
		u, err := c.SongURL(song.ID, quality)
		return songURLFetchedMsg{song: song, url: u, err: err}
	}
}

// listenEndCmd 等待一次 mpv 自然播完事件。
func listenEndCmd(endCh <-chan struct{}) tea.Cmd {
	return func() tea.Msg {
		<-endCh
		return playerEndedMsg{}
	}
}
