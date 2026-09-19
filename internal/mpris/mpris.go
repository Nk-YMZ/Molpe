// Package mpris 实现 MPRIS2 D-Bus 接口，向桌面环境暴露播放状态并接收媒体控制。
package mpris

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
)

// 总线名称与对象路径（MPRIS2 规范）。
const (
	busName     = "org.mpris.MediaPlayer2.mountain-air"
	objectPath  = dbus.ObjectPath("/org/mpris/MediaPlayer2")
	ifaceRoot   = "org.mpris.MediaPlayer2"
	ifacePlayer = "org.mpris.MediaPlayer2.Player"
	ifaceProps  = "org.freedesktop.DBus.Properties"
)

// Event 是桌面环境（媒体键、KDE 媒体组件等）发来的控制事件。
type Event int

const (
	EventPlayPause Event = iota
	EventPlay
	EventPause
	EventStop
)

// Track 是传递给桌面环境的曲目元数据。
type Track struct {
	ID       int64
	Title    string
	Artists  []string
	Album    string
	Duration time.Duration
	ArtURL   string // 封面地址，对应 mpris:artUrl
}

// Service 是 MPRIS2 服务。桌面端的控制动作通过 Events 通道传出，
// 播放状态与元数据由调用方通过 SetStatus / SetTrack 更新。
type Service struct {
	conn   *dbus.Conn
	events chan Event

	positionFn func() (float64, error) // 返回播放位置（秒）

	mu       sync.Mutex
	status   string
	metadata map[string]dbus.Variant
}

// New 连接会话总线并注册 MPRIS 接口。positionFn 用于应答 Position 属性查询。
func New(positionFn func() (float64, error)) (*Service, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, fmt.Errorf("连接会话总线失败: %w", err)
	}
	s := &Service{
		conn:       conn,
		events:     make(chan Event, 8),
		positionFn: positionFn,
		status:     "Stopped",
		metadata:   emptyMetadata(),
	}

	if err := conn.Export(s, objectPath, ifaceRoot); err != nil {
		conn.Close()
		return nil, err
	}
	if err := conn.Export(s, objectPath, ifacePlayer); err != nil {
		conn.Close()
		return nil, err
	}
	// Seek 的 MPRIS 签名与 io.Seeker 冲突（go vet 会警告），
	// 结构体方法命名为 SeekOffset，在 D-Bus 层映射回 Seek。
	if err := conn.ExportWithMap(s, map[string]string{"Seek": "SeekOffset"},
		objectPath, ifacePlayer); err != nil {
		conn.Close()
		return nil, err
	}
	if err := conn.Export(s, objectPath, ifaceProps); err != nil {
		conn.Close()
		return nil, err
	}
	if err := conn.Export(introspect.Introspectable(introspectionXML), objectPath,
		"org.freedesktop.DBus.Introspectable"); err != nil {
		conn.Close()
		return nil, err
	}

	reply, err := conn.RequestName(busName, dbus.NameFlagDoNotQueue)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("注册总线名称失败: %w", err)
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		conn.Close()
		return nil, errors.New("总线名称已被占用（是否已有实例在运行）")
	}
	return s, nil
}

// Events 返回桌面控制事件通道；服务关闭后通道关闭。
func (s *Service) Events() <-chan Event { return s.events }

// Close 释放总线名称并断开连接。
func (s *Service) Close() {
	if _, err := s.conn.ReleaseName(busName); err != nil {
		_ = err
	}
	s.conn.Close()
	close(s.events)
}

func emptyMetadata() map[string]dbus.Variant {
	return map[string]dbus.Variant{
		"mpris:trackid": dbus.MakeVariant(dbus.ObjectPath("/org/mpris/MediaPlayer2/track/0")),
	}
}

// SetTrack 更新当前曲目元数据并通知桌面环境。
func (s *Service) SetTrack(t Track) {
	s.mu.Lock()
	s.metadata = map[string]dbus.Variant{
		"mpris:trackid": dbus.MakeVariant(dbus.ObjectPath(fmt.Sprintf("/org/mpris/MediaPlayer2/track/%d", t.ID))),
		"mpris:length":  dbus.MakeVariant(t.Duration.Microseconds()),
		"xesam:title":   dbus.MakeVariant(t.Title),
		"xesam:artist":  dbus.MakeVariant(t.Artists),
		"xesam:album":   dbus.MakeVariant(t.Album),
		"mpris:artUrl":  dbus.MakeVariant(t.ArtURL),
	}
	s.mu.Unlock()
	s.emitChanged(ifacePlayer, "Metadata")
}

// SetStatus 更新播放状态（Playing / Paused / Stopped）并通知桌面环境。
func (s *Service) SetStatus(status string) {
	s.mu.Lock()
	s.status = status
	s.mu.Unlock()
	s.emitChanged(ifacePlayer, "PlaybackStatus")
}

func (s *Service) emitChanged(iface, name string) {
	value, err := s.get(iface, name)
	if err != nil {
		return
	}
	s.conn.Emit(objectPath, ifaceProps+".PropertiesChanged",
		iface, map[string]dbus.Variant{name: value}, []string{})
}

// ---- org.freedesktop.DBus.Properties ----

func (s *Service) get(iface, name string) (dbus.Variant, *dbus.Error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch iface {
	case ifaceRoot:
		switch name {
		case "CanQuit", "CanRaise", "HasTrackList":
			return dbus.MakeVariant(false), nil
		case "Identity":
			return dbus.MakeVariant("Mountain Air"), nil
		case "DesktopEntry":
			return dbus.MakeVariant("mountain-air"), nil
		case "SupportedUriSchemes", "SupportedMimeTypes":
			return dbus.MakeVariant([]string{}), nil
		}
	case ifacePlayer:
		switch name {
		case "PlaybackStatus":
			return dbus.MakeVariant(s.status), nil
		case "Metadata":
			return dbus.MakeVariant(s.metadata), nil
		case "Position":
			return dbus.MakeVariant(s.positionMicros()), nil
		case "Rate", "Volume":
			return dbus.MakeVariant(1.0), nil
		case "MinimumRate", "MaximumRate":
			return dbus.MakeVariant(1.0), nil
		case "CanControl", "CanPlay", "CanPause":
			return dbus.MakeVariant(true), nil
		case "CanGoNext", "CanGoPrevious", "CanSeek":
			return dbus.MakeVariant(false), nil
		}
	}
	return dbus.Variant{}, dbus.NewError("org.freedesktop.DBus.Error.UnknownProperty", []any{iface, name})
}

func (s *Service) positionMicros() int64 {
	if s.positionFn == nil {
		return 0
	}
	sec, err := s.positionFn()
	if err != nil {
		return 0
	}
	return int64(sec * 1e6)
}

// Get 实现 org.freedesktop.DBus.Properties.Get。
func (s *Service) Get(iface, name string) (dbus.Variant, *dbus.Error) {
	return s.get(iface, name)
}

// GetAll 实现 org.freedesktop.DBus.Properties.GetAll。
func (s *Service) GetAll(iface string) (map[string]dbus.Variant, *dbus.Error) {
	names := map[string][]string{
		ifaceRoot: {"CanQuit", "CanRaise", "HasTrackList", "Identity", "DesktopEntry",
			"SupportedUriSchemes", "SupportedMimeTypes"},
		ifacePlayer: {"PlaybackStatus", "Metadata", "Position", "Rate", "Volume",
			"MinimumRate", "MaximumRate", "CanControl", "CanPlay", "CanPause",
			"CanGoNext", "CanGoPrevious", "CanSeek"},
	}
	result := make(map[string]dbus.Variant)
	for _, name := range names[iface] {
		if v, err := s.get(iface, name); err == nil {
			result[name] = v
		}
	}
	if len(result) == 0 {
		return nil, dbus.NewError("org.freedesktop.DBus.Error.UnknownInterface", []any{iface})
	}
	return result, nil
}

// Set 实现 org.freedesktop.DBus.Properties.Set；所有属性只读。
func (s *Service) Set(iface, name string, value dbus.Variant) *dbus.Error {
	return dbus.NewError("org.freedesktop.DBus.Error.PropertyReadOnly", []any{iface, name})
}

// ---- org.mpris.MediaPlayer2 ----

// Raise 不支持（TUI 无法前台化），空实现。
func (s *Service) Raise() *dbus.Error { return nil }

// Quit 不支持，空实现。
func (s *Service) Quit() *dbus.Error { return nil }

// ---- org.mpris.MediaPlayer2.Player ----

func (s *Service) push(ev Event) {
	select {
	case s.events <- ev:
	default: // 通道满时丢弃，避免阻塞 D-Bus 调用方
	}
}

// PlayPause 切换播放/暂停。
func (s *Service) PlayPause() *dbus.Error { s.push(EventPlayPause); return nil }

// Play 恢复播放。
func (s *Service) Play() *dbus.Error { s.push(EventPlay); return nil }

// Pause 暂停播放。
func (s *Service) Pause() *dbus.Error { s.push(EventPause); return nil }

// Stop 停止播放（按暂停处理）。
func (s *Service) Stop() *dbus.Error { s.push(EventStop); return nil }

// Next 暂不支持（无队列），空实现。
func (s *Service) Next() *dbus.Error { return nil }

// Previous 暂不支持（无队列），空实现。
func (s *Service) Previous() *dbus.Error { return nil }

// SeekOffset 对应 MPRIS 的 Seek 方法，暂不支持，空实现。
func (s *Service) SeekOffset(offset int64) *dbus.Error { return nil }

// SetPosition 暂不支持，空实现。
func (s *Service) SetPosition(trackID dbus.ObjectPath, position int64) *dbus.Error { return nil }

// OpenUri 暂不支持，空实现。
func (s *Service) OpenUri(uri string) *dbus.Error { return nil }
