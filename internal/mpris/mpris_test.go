package mpris

import (
	"os"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

// 集成测试：需要可用的会话 D-Bus（桌面环境），否则跳过。
func TestService(t *testing.T) {
	if os.Getenv("DBUS_SESSION_BUS_ADDRESS") == "" {
		t.Skip("无会话 D-Bus，跳过集成测试")
	}

	svc, err := New(func() (float64, error) { return 12.5, nil })
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()

	// 模拟桌面端：另建一个连接调用本服务。
	client, err := dbus.ConnectSessionBus()
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	obj := client.Object(busName, objectPath)

	// 根接口属性。
	var identity dbus.Variant
	if err := obj.Call(ifaceProps+".Get", 0, ifaceRoot, "Identity").Store(&identity); err != nil {
		t.Fatal(err)
	}
	if identity.Value() != "Mountain Air" {
		t.Errorf("Identity = %v", identity.Value())
	}

	// 元数据，含封面地址。
	svc.SetTrack(Track{
		ID:       42,
		Title:    "测试歌曲",
		Artists:  []string{"甲", "乙"},
		Album:    "测试专辑",
		Duration: 3*time.Minute + 4*time.Second,
		ArtURL:   "http://example.com/cover.jpg",
	})
	svc.SetStatus("Playing")

	var metadata dbus.Variant
	if err := obj.Call(ifaceProps+".Get", 0, ifacePlayer, "Metadata").Store(&metadata); err != nil {
		t.Fatal(err)
	}
	md := metadata.Value().(map[string]dbus.Variant)
	if got := md["xesam:title"].Value(); got != "测试歌曲" {
		t.Errorf("title = %v", got)
	}
	if got := md["mpris:artUrl"].Value(); got != "http://example.com/cover.jpg" {
		t.Errorf("artUrl = %v", got)
	}
	if got := md["mpris:length"].Value(); got != int64(184_000_000) {
		t.Errorf("length = %v", got)
	}
	if got := md["xesam:artist"].Value().([]string); len(got) != 2 {
		t.Errorf("artist = %v", got)
	}

	// Position 由 positionFn 动态计算。
	var pos dbus.Variant
	if err := obj.Call(ifaceProps+".Get", 0, ifacePlayer, "Position").Store(&pos); err != nil {
		t.Fatal(err)
	}
	if pos.Value() != int64(12_500_000) {
		t.Errorf("Position = %v", pos.Value())
	}

	// 媒体控制方法应产生事件。
	if call := obj.Call(ifacePlayer+".PlayPause", 0); call.Err != nil {
		t.Fatal(call.Err)
	}
	select {
	case ev := <-svc.Events():
		if ev != EventPlayPause {
			t.Errorf("事件 = %v", ev)
		}
	case <-time.After(2 * time.Second):
		t.Error("未收到 PlayPause 事件")
	}

	// 总线名称冲突：第二个实例应失败。
	svc2, err := New(nil)
	if err == nil {
		svc2.Close()
		t.Error("重复实例应失败")
	}
}
