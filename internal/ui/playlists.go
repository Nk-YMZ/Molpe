package ui

import (
	tea "charm.land/bubbletea/v2"

	"mountain-air/internal/netease"
)

// playlistsPage 是歌单列表页。
type playlistsPage struct {
	list    listModel
	data    []netease.Playlist
	loading bool
	err     error
}

// songsPage 是歌单内歌曲页。
type songsPage struct {
	list     listModel
	data     []netease.Song
	playlist netease.Playlist
	loading  bool
	err      error
}

type playlistsFetchedMsg struct {
	playlists []netease.Playlist
	err       error
}

type songsFetchedMsg struct {
	songs []netease.Song
	err   error
}

func fetchPlaylistsCmd(c *netease.Client, uid int64) tea.Cmd {
	return func() tea.Msg {
		playlists, err := c.UserPlaylists(uid)
		return playlistsFetchedMsg{playlists: playlists, err: err}
	}
}

func fetchSongsCmd(c *netease.Client, playlistID int64) tea.Cmd {
	return func() tea.Msg {
		songs, err := c.PlaylistSongs(playlistID)
		return songsFetchedMsg{songs: songs, err: err}
	}
}
