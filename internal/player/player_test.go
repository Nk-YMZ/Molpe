package player

import (
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestPlayerEnd 验证自然播完事件和关闭后的进程、socket 清理。
func TestPlayerEnd(t *testing.T) {
	if _, err := exec.LookPath("mpv"); err != nil {
		t.Skip("未安装 mpv，跳过集成测试")
	}
	p, err := Start()
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	ended := make(chan string, 1)
	p.SetEndCallback(func(reason string) {
		select {
		case ended <- reason:
		default:
		}
	})
	if err := p.SetVolume(5); err != nil {
		t.Fatal(err)
	}
	if err := p.Load("av://lavfi:sine=frequency=440:duration=1", 0, false); err != nil {
		t.Fatal(err)
	}
	select {
	case reason := <-ended:
		if reason != "eof" {
			t.Fatalf("自然播完应为 eof，实际为 %q", reason)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("未收到自然播完事件")
	}
	p.Close()
	if _, err := os.Stat(p.socket); !os.IsNotExist(err) {
		t.Fatalf("关闭后 socket 未清理: %v", err)
	}
	select {
	case <-p.Exited():
	default:
		t.Fatal("关闭后 mpv 进程仍在运行")
	}
}

// TestPlayerLifecycle 需要系统安装 mpv，属于集成测试。
func TestPlayerLifecycle(t *testing.T) {
	if _, err := exec.LookPath("mpv"); err != nil {
		t.Skip("未安装 mpv，跳过集成测试")
	}

	p, err := Start()
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	if err := p.Load("av://lavfi:sine=frequency=440:duration=5", 0, false); err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Second)
	if err := p.SetPause(true); err != nil {
		t.Fatal(err)
	}
	if err := p.SetPause(false); err != nil {
		t.Fatal(err)
	}
}

// TestLoadPauseAndStart 验证 Load 的 pause/start 文件级选项：
// 与加载原子生效（不存在 loadfile 后单独 set_property pause 的竞争窗口），
// 且不受上一曲遗留的全局暂停状态影响。需要系统安装 mpv。
func TestLoadPauseAndStart(t *testing.T) {
	if _, err := exec.LookPath("mpv"); err != nil {
		t.Skip("未安装 mpv，跳过集成测试")
	}
	const url = "av://lavfi:sine=frequency=440:duration=60"

	p, err := Start()
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	// 暂停开播：位置应保持不动。
	if err := p.Load(url, 0, true); err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)
	pos1, err := p.Position()
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)
	pos2, _ := p.Position()
	if pos2-pos1 > 0.1 {
		t.Fatalf("暂停开播不应前进: %.2f -> %.2f", pos1, pos2)
	}

	// 遗留全局暂停（用户中途按过暂停）后，带 pause=no 的下一曲应正常开播，
	// 并从 start 指定位置开始前进。
	if err := p.SetPause(true); err != nil {
		t.Fatal(err)
	}
	if err := p.Load(url, 5*time.Second, false); err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)
	pos3, err := p.Position()
	if err != nil {
		t.Fatal(err)
	}
	if pos3 < 4.5 || pos3 > 6.5 {
		t.Fatalf("开播定位应在 5s 附近: %.2f", pos3)
	}
	time.Sleep(500 * time.Millisecond)
	pos4, _ := p.Position()
	if pos4-pos3 < 0.2 {
		t.Fatalf("pause=no 应覆盖遗留的全局暂停并前进: %.2f -> %.2f", pos3, pos4)
	}
}

func TestCommandSkipsEvents(t *testing.T) {
	root, err := os.MkdirTemp("/tmp", "molpe-command-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	socket := filepath.Join(root, "s")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		var command map[string]any
		if json.NewDecoder(conn).Decode(&command) != nil {
			return
		}
		encoder := json.NewEncoder(conn)
		_ = encoder.Encode(map[string]any{"event": "playback-restart"})
		_ = encoder.Encode(map[string]any{"error": "success", "data": 5.0})
	}()
	p := &Player{socket: socket}
	pos, err := p.Position()
	if err != nil || pos != 5 {
		t.Fatalf("事件不能截断命令响应: %.2f %v", pos, err)
	}
}
