package netease

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// songDetailBatch 是歌曲详情接口单次请求的 ID 数上限。
const songDetailBatch = 500

// Playlist 是用户歌单的基本信息。
type Playlist struct {
	ID         int64
	Name       string
	TrackCount int
	Created    bool // true 为用户创建，false 为用户收藏
}

// UserPlaylists 获取用户创建与收藏的歌单。
func (c *Client) UserPlaylists(uid int64) ([]Playlist, error) {
	code, body, err := c.call("获取歌单列表", "/api/user/playlist", map[string]any{
		"uid": uid, "limit": 1000, "offset": 0, "includeVideo": true,
	})
	if err != nil {
		return nil, err
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
	CoverURL string // 专辑封面地址（统一为 https），供封面显示与 MPRIS 使用
	Duration time.Duration
}

// songDetailJSON 是歌曲对象的公共字段：歌单详情的 tracks[] 与歌曲详情接口的
// songs[] 结构相同（实测逐字段一致，仅封面地址协议不同）。
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
	cover := t.Al.PicURL
	if after, ok := strings.CutPrefix(cover, "http://"); ok {
		cover = "https://" + after // 歌单详情下发 http 地址，同一资源支持 https
	}
	return Song{
		ID:       t.ID,
		Name:     t.Name,
		Artists:  strings.Join(artists, "/"),
		Album:    t.Al.Name,
		CoverURL: cover,
		Duration: time.Duration(t.DT) * time.Millisecond,
	}
}

// PlaylistSongs 获取歌单内的全部歌曲，顺序与歌单一致。
//
// 歌单详情一次返回全部曲目及其完整信息；歌单过大时服务端可能只返回部分
// 曲目详情（曲目 ID 列表仍是全量），缺的部分用歌曲详情接口分批补齐。
func (c *Client) PlaylistSongs(id int64) ([]Song, error) {
	code, body, err := c.call("获取歌单歌曲", "/api/v6/playlist/detail", map[string]any{
		"id": id, "n": 100000, "s": 8,
	})
	if err != nil {
		return nil, err
	}
	if code != 200 {
		return nil, requestError("获取歌单歌曲", code, body)
	}
	var detail struct {
		Playlist struct {
			TrackIDs []struct {
				ID int64 `json:"id"`
			} `json:"trackIds"`
			Tracks []songDetailJSON `json:"tracks"`
		} `json:"playlist"`
	}
	if err := json.Unmarshal(body, &detail); err != nil {
		return nil, fmt.Errorf("解析歌单歌曲失败: %w", err)
	}

	byID := make(map[int64]Song, len(detail.Playlist.Tracks))
	for _, t := range detail.Playlist.Tracks {
		s := t.toSong()
		byID[s.ID] = s
	}
	var missing []int64
	for _, t := range detail.Playlist.TrackIDs {
		if _, ok := byID[t.ID]; !ok {
			missing = append(missing, t.ID)
		}
	}
	for start := 0; start < len(missing); start += songDetailBatch {
		batch := missing[start:min(start+songDetailBatch, len(missing))]
		songs, err := c.songDetails(batch)
		if err != nil {
			return nil, err
		}
		for _, s := range songs {
			byID[s.ID] = s
		}
	}
	// 按曲目 ID 列表的顺序输出；已下架（两处都查不到）的曲目自然被跳过。
	songs := make([]Song, 0, len(detail.Playlist.TrackIDs))
	for _, t := range detail.Playlist.TrackIDs {
		if s, ok := byID[t.ID]; ok {
			songs = append(songs, s)
		}
	}
	return songs, nil
}

// songDetails 按 ID 批量拉取歌曲详情。
func (c *Client) songDetails(ids []int64) ([]Song, error) {
	var cParam strings.Builder
	cParam.WriteByte('[')
	for i, id := range ids {
		if i > 0 {
			cParam.WriteByte(',')
		}
		cParam.WriteString(`{"id":` + strconv.FormatInt(id, 10) + `}`)
	}
	cParam.WriteByte(']')
	code, body, err := c.call("获取歌曲详情", "/api/v3/song/detail", map[string]any{"c": cParam.String()})
	if err != nil {
		return nil, err
	}
	if code != 200 {
		return nil, requestError("获取歌曲详情", code, body)
	}
	var resp struct {
		Songs []songDetailJSON `json:"songs"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("解析歌曲详情失败: %w", err)
	}
	songs := make([]Song, 0, len(resp.Songs))
	for _, t := range resp.Songs {
		songs = append(songs, t.toSong())
	}
	return songs, nil
}
