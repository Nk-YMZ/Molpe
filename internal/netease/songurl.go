package netease

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
)

// DefaultQuality 是默认音质偏好。
const DefaultQuality = "lossless"

// qualityOrder 音质等级，从低到高。
var qualityOrder = []string{"standard", "higher", "exhigh", "lossless", "hires"}

// QualityLevels 返回全部可选音质，从低到高。
func QualityLevels() []string {
	return slices.Clone(qualityOrder)
}

// NormalizeQuality 校验音质字符串，无效时返回默认音质。
func NormalizeQuality(q string) string {
	if slices.Contains(qualityOrder, q) {
		return q
	}
	return DefaultQuality
}

// SongURL 是歌曲播放地址及其实际音质信息。
type SongURL struct {
	URL   string
	Level string // 实际获得的音质（可能低于请求音质）
	Size  int64
	Type  string // flac / mp3 等
}

// SongURL 按指定音质获取歌曲播放地址及其实际音质。
//
// 使用 EAPI（官方客户端通道）与完整的 PC 客户端档案：网页端通道对会员歌曲的
// Hi-Res 会错误标注，设备头残缺时同样把 Hi-Res 标成 lossless。未登录时不发
// 请求（匿名只能拿到试听音质），直接返回 ErrNotLoggedIn。
func (c *Client) SongURL(id int64, quality string) (*SongURL, error) {
	if !c.LoggedIn() {
		return nil, ErrNotLoggedIn
	}
	op := fmt.Sprintf("获取歌曲 %d 播放地址", id)
	code, body, err := c.call(op, "/api/song/enhance/player/url/v1", map[string]any{
		"ids":        "[" + strconv.FormatInt(id, 10) + "]",
		"level":      NormalizeQuality(quality),
		"encodeType": "flac",
	})
	if err != nil {
		return nil, err
	}
	if code != 200 {
		return nil, requestError(op, code, body)
	}
	var resp struct {
		Code int `json:"code"`
		Data []struct {
			URL   string `json:"url"`
			Level string `json:"level"`
			Size  int64  `json:"size"`
			Type  string `json:"type"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("解析歌曲 %d 播放地址失败: %w", id, err)
	}
	if resp.Code != 200 || len(resp.Data) == 0 || resp.Data[0].URL == "" {
		return nil, fmt.Errorf("歌曲 %d 没有指定音质的可用播放地址", id)
	}
	d := resp.Data[0]
	return &SongURL{URL: d.URL, Level: d.Level, Size: d.Size, Type: d.Type}, nil
}
