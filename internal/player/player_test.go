package player

import (
	"os/exec"
	"testing"
	"time"
)

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

	if err := p.Play("av://lavfi:sine=frequency=440:duration=5"); err != nil {
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
