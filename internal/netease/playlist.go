package netease

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/go-musicfox/netease-music/service"
)

// Playlist 是用户歌单的基本信息。
type Playlist struct {
	ID         int64
	Name       string
	TrackCount int
	Created    bool // true 为用户创建，false 为用户收藏
}

// UserPlaylists 获取用户创建与收藏的歌单。
func (c *Client) UserPlaylists(uid int64) ([]Playlist, error) {
	s := &service.UserPlaylistService{
		Uid:   strconv.FormatInt(uid, 10),
		Limit: "1000",
	}
	code, body := s.UserPlaylist()
	if code != 200 || len(body) == 0 {
		return nil, fmt.Errorf("获取歌单列表失败: code=%v", code)
	}
	var resp struct {
		Playlist []struct {
			ID         int64  `json:"id"`
			Name       string `json:"name"`
			TrackCount int    `json:"trackCount"`
			Creator    struct {
				UserID int64 `json:"userId"`
			} `json:"creator"`
		} `json:"playlist"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("解析歌单列表失败: %w", err)
	}

	playlists := make([]Playlist, 0, len(resp.Playlist))
	for _, p := range resp.Playlist {
		playlists = append(playlists, Playlist{
			ID:         p.ID,
			Name:       p.Name,
			TrackCount: p.TrackCount,
			Created:    p.Creator.UserID == uid,
		})
	}
	return playlists, nil
}

// Song 是歌曲的基本信息。
type Song struct {
	ID       int64
	Name     string
	Artists  string // 多个艺术家以 / 分隔
	Album    string
	Duration time.Duration
}

// PlaylistSongs 获取歌单内的全部歌曲。
func (c *Client) PlaylistSongs(id int64) ([]Song, error) {
	s := &service.PlaylistTrackAllService{Id: strconv.FormatInt(id, 10)}
	code, body := s.AllTracks()
	if code != 200 || len(body) == 0 {
		return nil, fmt.Errorf("获取歌单歌曲失败: code=%v", code)
	}
	var resp struct {
		Playlist struct {
			Tracks []struct {
				ID   int64  `json:"id"`
				Name string `json:"name"`
				DT   int64  `json:"dt"`
				Ar   []struct {
					Name string `json:"name"`
				} `json:"ar"`
				Al struct {
					Name string `json:"name"`
				} `json:"al"`
			} `json:"tracks"`
		} `json:"playlist"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("解析歌单歌曲失败: %w", err)
	}

	songs := make([]Song, 0, len(resp.Playlist.Tracks))
	for _, t := range resp.Playlist.Tracks {
		artists := make([]string, 0, len(t.Ar))
		for _, a := range t.Ar {
			artists = append(artists, a.Name)
		}
		songs = append(songs, Song{
			ID:       t.ID,
			Name:     t.Name,
			Artists:  strings.Join(artists, "/"),
			Album:    t.Al.Name,
			Duration: time.Duration(t.DT) * time.Millisecond,
		})
	}
	return songs, nil
}
