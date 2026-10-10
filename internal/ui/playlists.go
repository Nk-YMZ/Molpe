package ui

import (
	"errors"
	"time"

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

// fetchRetryInterval 歌单/歌曲拉取失败后的自动重试间隔。
// 重试本身即网络探测：网络恢复后最多一个间隔内自动加载。
const fetchRetryInterval = 5 * time.Second

// playlistsRetryMsg 歌单列表自动重试定时器到期。
type playlistsRetryMsg struct{}

// songsRetryMsg 歌单歌曲自动重试定时器到期；playlistID 用于丢弃切换歌单后的残留。
type songsRetryMsg struct{ playlistID int64 }

// schedulePlaylistsRetry 歌单列表拉取失败后安排自动重试；
// 重复失败重复安排，成功后由守卫条件（err==nil）自然终止。
func (m *Model) schedulePlaylistsRetry() tea.Cmd {
	return tea.Tick(fetchRetryInterval, func(time.Time) tea.Msg { return playlistsRetryMsg{} })
}

// scheduleSongsRetry 歌单歌曲拉取失败后安排自动重试（同上）。
func (m *Model) scheduleSongsRetry(playlistID int64) tea.Cmd {
	return tea.Tick(fetchRetryInterval, func(time.Time) tea.Msg { return songsRetryMsg{playlistID} })
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
