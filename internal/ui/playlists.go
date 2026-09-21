package ui

import (
	"errors"

	tea "charm.land/bubbletea/v2"

	"molpe/internal/ipc"
	"molpe/internal/netease"
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
	playlistID int64 // 请求的目标歌单，用于丢弃过期响应
	songs      []netease.Song
	err        error
}

// fetchPlaylistsCmd 向后端请求歌单列表。
func fetchPlaylistsCmd(c *ipc.Client) tea.Cmd {
	return func() tea.Msg {
		resp, err := c.Request(ipc.TGetPlaylists, nil)
		if err != nil {
			return playlistsFetchedMsg{err: err}
		}
		var pm ipc.PlaylistsMsg
		if err := resp.DecodeData(&pm); err != nil {
			return playlistsFetchedMsg{err: err}
		}
		if pm.Err != "" {
			return playlistsFetchedMsg{err: errors.New(pm.Err)}
		}
		return playlistsFetchedMsg{playlists: pm.Playlists}
	}
}

// fetchSongsCmd 向后端请求歌单歌曲。
func fetchSongsCmd(c *ipc.Client, playlistID int64) tea.Cmd {
	return func() tea.Msg {
		resp, err := c.Request(ipc.TGetSongs, ipc.GetSongsCmd{PlaylistID: playlistID})
		if err != nil {
			return songsFetchedMsg{playlistID: playlistID, err: err}
		}
		var sm ipc.SongsMsg
		if err := resp.DecodeData(&sm); err != nil {
			return songsFetchedMsg{playlistID: playlistID, err: err}
		}
		if sm.Err != "" {
			return songsFetchedMsg{playlistID: playlistID, err: errors.New(sm.Err)}
		}
		return songsFetchedMsg{playlistID: playlistID, songs: sm.Songs}
	}
}
