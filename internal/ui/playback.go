package ui

import (
	tea "charm.land/bubbletea/v2"

	"mountain-air/internal/mpris"
	"mountain-air/internal/netease"
)

type songURLFetchedMsg struct {
	song netease.Song
	url  *netease.SongURL
	err  error
}

type mprisEventMsg mpris.Event

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
