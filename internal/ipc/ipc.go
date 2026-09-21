// Package ipc 定义 TUI 前端与播放守护进程（molpe daemon）之间的通信协议。
//
// 传输为 unix socket 上的 JSON Lines：每行一个 Message。客户端→服务端为
// 命令与数据请求（请求带 ID，响应以相同 ID 配对）；服务端→客户端为事件推送
// （状态快照、提示、歌词）与请求响应。播放位置等连续变化的量不周期推送，
// 由前端依据快照中的位置锚点本地外推。
package ipc

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"molpe/internal/netease"
	"molpe/internal/queue"
)

// Type 是消息类型。
type Type string

// 服务端→客户端。
const (
	TState     Type = "state"     // 全量状态快照（State），任何变化时推送
	TBusy      Type = "busy"      // 已有前端连接，拒绝新连接
	TNote      Type = "note"      // 操作提示（Note）
	TLyrics    Type = "lyrics"    // 当前曲目歌词（LyricsMsg）
	TPlaylists Type = "playlists" // 歌单列表响应（PlaylistsMsg）
	TSongs     Type = "songs"     // 歌单歌曲响应（SongsMsg）
)

// 客户端→服务端。
const (
	TGetPlaylists Type = "get_playlists" // 拉取歌单列表（无载荷）
	TGetSongs     Type = "get_songs"     // 拉取歌单歌曲（GetSongsCmd）
	TPlaySong     Type = "play_song"     // 点歌（PlaySongCmd）
	TPlayNext     Type = "play_next"     // 加入下一首播放（PlayNextCmd）
	TToggle       Type = "toggle"        // 播放/暂停
	TNext         Type = "next"          // 下一首
	TPrev         Type = "prev"          // 上一首
	TCycleMode    Type = "cycle_mode"    // 循环切换播放模式
	TVolumeDelta  Type = "volume_delta"  // 调整音量（VolumeCmd）
	TRemove       Type = "remove"        // 从队列删除条目（RemoveCmd）
	TQRNew        Type = "qr_new"        // 重新生成登录二维码（无载荷）
	TSetTheme     Type = "set_theme"     // 同步主题选择（ThemeCmd）
	TShutdown     Type = "shutdown"      // 完全退出：停止播放并结束后端
)

// Message 是线上消息信封。ID 仅在请求/响应配对时使用，事件推送为 0。
type Message struct {
	ID   int             `json:"id,omitempty"`
	Type Type            `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
}

// Playing 是当前曲目及其实际音质。
type Playing struct {
	Song  netease.Song `json:"song"`
	Level string       `json:"level"` // 实际音质（EAPI 返回值）
}

// QRStatus 是进行中的二维码登录会话状态；Code 为网易云状态码
// （800 过期 / 801 等待 / 802 待确认 / 803 成功）。
type QRStatus struct {
	URL  string `json:"url"`
	Code int    `json:"code"`
}

// State 是服务端播放状态的全量快照，hello 握手与每次变化时推送。
type State struct {
	Checked bool             `json:"checked"`           // 登录态是否已确认（未确认时前端显示检查中）
	Account *netease.Account `json:"account,omitempty"` // 当前登录账号，未登录为 nil
	QR      *QRStatus        `json:"qr,omitempty"`      // 进行中的二维码会话，无为 nil
	Playing *Playing         `json:"playing,omitempty"`
	Paused  bool             `json:"paused"`
	Pos     float64          `json:"pos"`                // 推送时刻的播放位置锚点（秒），前端据此本地外推
	GapLeft float64          `json:"gap_left,omitempty"` // 歌曲间隔剩余秒数，>0 表示间隔中（下一首待播）
	Volume  int              `json:"volume"`             // 音量百分比（0-100）
	Quality string           `json:"quality"`            // 配置的音质偏好
	Queue   queue.State      `json:"queue"`              // 队列快照（含模式）
}

// Note 是服务端发给前端的一次性提示；Err 为 true 时按错误展示。
type Note struct {
	Text string `json:"text"`
	Err  bool   `json:"err,omitempty"`
}

// LyricsMsg 是歌词推送；SongID 用于前端丢弃切歌后的过期结果。
type LyricsMsg struct {
	SongID int64               `json:"song_id"`
	Lines  []netease.LyricLine `json:"lines"`
	Err    string              `json:"err,omitempty"`
}

// PlaylistsMsg 是歌单列表响应。
type PlaylistsMsg struct {
	Playlists []netease.Playlist `json:"playlists"`
	Err       string             `json:"err,omitempty"`
}

// SongsMsg 是歌单歌曲响应；PlaylistID 用于前端丢弃过期响应。
type SongsMsg struct {
	PlaylistID int64          `json:"playlist_id"`
	Songs      []netease.Song `json:"songs"`
	Err        string         `json:"err,omitempty"`
}

// PlaySongCmd 点歌：Playlist 为该曲所在的完整歌单（服务端据以重置队列歌单）。
type PlaySongCmd struct {
	Playlist []netease.Song `json:"playlist"`
	Song     netease.Song   `json:"song"`
}

// PlayNextCmd 将歌曲加入下一首播放队列。
type PlayNextCmd struct {
	Song netease.Song `json:"song"`
}

// VolumeCmd 按步进调整音量。
type VolumeCmd struct {
	Delta int `json:"delta"`
}

// Remove 的删除区域。
const (
	SectionHistory  = "history"  // 历史记录
	SectionNextUp   = "nextup"   // 下一首播放队列
	SectionPlaylist = "playlist" // 歌单后续
	SectionCurrent  = "current"  // 当前播放曲目
)

// RemoveCmd 删除队列条目；Index 为条目在对应区域内的下标（current 忽略）。
type RemoveCmd struct {
	Section string `json:"section"`
	Index   int    `json:"index"`
}

// GetSongsCmd 拉取指定歌单的歌曲。
type GetSongsCmd struct {
	PlaylistID int64 `json:"playlist_id"`
}

// ThemeCmd 同步前端选择的主题名（由后端随配置统一落盘）。
type ThemeCmd struct {
	Name string `json:"name"`
}

// Encode 将消息以 JSON Lines 写入 w。
func Encode(w io.Writer, m Message) error {
	data, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("序列化消息失败: %w", err)
	}
	if _, err := w.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("写入消息失败: %w", err)
	}
	return nil
}

// Decode 从 r 读取一条 JSON Lines 消息。
func Decode(r *bufio.Reader) (Message, error) {
	var m Message
	line, err := r.ReadBytes('\n')
	if err != nil {
		return m, err
	}
	if err := json.Unmarshal(line, &m); err != nil {
		return m, fmt.Errorf("解析消息失败: %w", err)
	}
	return m, nil
}

// NewMessage 构造带载荷的消息；载荷为 nil 时省略 Data。
func NewMessage(id int, t Type, payload any) Message {
	m := Message{ID: id, Type: t}
	if payload != nil {
		// 协议类型均为已知可序列化结构，失败属于编程错误，静默省略即可。
		m.Data, _ = json.Marshal(payload)
	}
	return m
}

// DecodeData 将消息载荷解析到 v；无载荷时返回 nil。
func (m Message) DecodeData(v any) error {
	if len(m.Data) == 0 {
		return nil
	}
	return json.Unmarshal(m.Data, v)
}

// SocketPath 返回前后端通信的 unix socket 路径；
// 优先使用 XDG_RUNTIME_DIR（随登录会话清理），缺失时回退到 cacheDir。
func SocketPath(cacheDir string) string {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = cacheDir
	}
	return filepath.Join(dir, "molpe", "ipc.sock")
}
