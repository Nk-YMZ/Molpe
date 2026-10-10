package netease

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
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
	code, body, err := c.doWeapi("user/playlist", map[string]string{
		"uid":    strconv.FormatInt(uid, 10),
		"limit":  "1000",
		"offset": "0",
	})
	if err != nil {
		return nil, fmt.Errorf("获取歌单列表失败（网络传输错误）: %w", err)
	}
	if code != 200 {
		return nil, requestError("获取歌单列表", code, body)
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
	CoverURL string // 专辑封面地址，供 MPRIS 等系统组件使用
	Duration time.Duration
}

// songDetailJSON 是歌曲详情接口（v3/song/detail）返回的单曲结构。
type songDetailJSON struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	DT   int64  `json:"dt"`
	Ar   []struct {
		Name string `json:"name"`
	} `json:"ar"`
	Al struct {
		Name   string `json:"name"`
		PicURL string `json:"picUrl"`
	} `json:"al"`
}

func (t songDetailJSON) toSong() Song {
	artists := make([]string, 0, len(t.Ar))
	for _, a := range t.Ar {
		artists = append(artists, a.Name)
	}
	return Song{
		ID:       t.ID,
		Name:     t.Name,
		Artists:  strings.Join(artists, "/"),
		Album:    t.Al.Name,
		CoverURL: t.Al.PicURL,
		Duration: time.Duration(t.DT) * time.Millisecond,
	}
}

// PlaylistSongs 获取歌单内的全部歌曲：先拉歌单详情取得完整曲目 ID 列表，
// 再分批（每批 500 首）拉取歌曲详情。
func (c *Client) PlaylistSongs(id int64) ([]Song, error) {
	pid := strconv.FormatInt(id, 10)
	code, body, err := c.doLinuxapi("https://music.163.com/api/v3/playlist/detail", map[string]string{
		"id": pid, "n": "100000", "s": "8",
	})
	if err != nil {
		return nil, fmt.Errorf("获取歌单歌曲失败（网络传输错误）: %w", err)
	}
	if code != 200 {
		return nil, requestError("获取歌单歌曲", code, body)
	}
	var detail struct {
		Playlist struct {
			TrackIDs []struct {
				ID int64 `json:"id"`
			} `json:"trackIds"`
		} `json:"playlist"`
	}
	if err := json.Unmarshal(body, &detail); err != nil {
		return nil, fmt.Errorf("解析歌单歌曲失败: %w", err)
	}

	var songs []Song
	ids := detail.Playlist.TrackIDs
	for start := 0; start < len(ids); start += 500 {
		batch := ids[start:min(start+500, len(ids))]
		var idList strings.Builder
		var cParam strings.Builder
		cParam.WriteByte('[')
		for i, t := range batch {
			if i > 0 {
				idList.WriteByte(',')
				cParam.WriteByte(',')
			}
			s := strconv.FormatInt(t.ID, 10)
			idList.WriteString(s)
			cParam.WriteString(`{"id":` + s + `}`)
		}
		cParam.WriteByte(']')
		code, body, err = c.doWeapi("v3/song/detail", map[string]string{
			"c":   cParam.String(),
			"ids": "[" + idList.String() + "]",
		})
		if err != nil {
			return nil, fmt.Errorf("获取歌单歌曲失败（网络传输错误）: %w", err)
		}
		if code != 200 {
			return nil, requestError("获取歌单歌曲", code, body)
		}
		var resp struct {
			Songs []songDetailJSON `json:"songs"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("解析歌单歌曲失败: %w", err)
		}
		for _, t := range resp.Songs {
			songs = append(songs, t.toSong())
		}
	}
	return songs, nil
}
