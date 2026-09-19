package ui

import (
	tea "charm.land/bubbletea/v2"

	"mountain-air/internal/netease"
)

type songURLFetchedMsg struct {
	song netease.Song
	url  *netease.SongURL
	err  error
}

func fetchSongURLCmd(c *netease.Client, song netease.Song, quality string) tea.Cmd {
	return func() tea.Msg {
		u, err := c.SongURL(song.ID, quality)
		return songURLFetchedMsg{song: song, url: u, err: err}
	}
}
