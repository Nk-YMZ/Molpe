package server

import (
	"testing"
	"time"

	"molpe/internal/config"
	"molpe/internal/ipc"
	"molpe/internal/netease"
	"molpe/internal/queue"
)

// setupServer 在临时 XDG 目录下启动一个真实的服务端，返回其运行结束通知。
func setupServer(t *testing.T) (ipc.State, <-chan error) {
	t.Helper()
	for _, env := range []string{"XDG_RUNTIME_DIR", "XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_CACHE_HOME"} {
		t.Setenv(env, t.TempDir())
	}
	dirs := config.DefaultDirs()
	srv, err := New(dirs)
	if err != nil {
		t.Fatalf("New 失败: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- srv.Run() }()
	return ipc.State{}, done
}

// dial 带重试地连接服务端（等待 socket 就绪）。
func dial(t *testing.T, dirs config.Dirs) (*ipc.Client, ipc.State) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		c, st, err := ipc.Dial(ipc.SocketPath(dirs.Cache))
		if err == nil {
			return c, st
		}
		if time.Now().After(deadline) {
			t.Fatalf("连接服务端失败: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// nextEvent 读取下一条指定类型的事件，超时或收到其他事件则继续等待。
func nextEvent(t *testing.T, c *ipc.Client, want ipc.Type) ipc.Message {
	t.Helper()
	timeout := time.After(5 * time.Second)
	for {
		select {
		case msg := <-c.Events():
			if msg.Type == want {
				return msg
			}
		case <-c.Gone():
			t.Fatalf("连接意外断开")
		case <-timeout:
			t.Fatalf("等待事件 %s 超时", want)
		}
	}
}

func TestServerLifecycle(t *testing.T) {
	_, done := setupServer(t)
	dirs := config.DefaultDirs()

	c, st := dial(t, dirs)
	if st.Volume != 100 || st.Quality != "lossless" || st.Queue.Mode != queue.ModeSequential {
		t.Fatalf("初始状态不符: %+v", st)
	}

	// 第二个连接应被拒绝。
	if _, _, err := ipc.Dial(ipc.SocketPath(dirs.Cache)); err != ipc.ErrBusy {
		t.Fatalf("重复连接应返回 ErrBusy，实际: %v", err)
	}

	// 加入下一首播放：应推送更新后的状态快照。
	song := netease.Song{ID: 42, Name: "测试曲", Artists: "某人"}
	if err := c.Send(ipc.TPlayNext, ipc.PlayNextCmd{Song: song}); err != nil {
		t.Fatalf("发送命令失败: %v", err)
	}
	msg := nextEvent(t, c, ipc.TState)
	var st2 ipc.State
	if err := msg.DecodeData(&st2); err != nil {
		t.Fatalf("解析状态失败: %v", err)
	}
	if len(st2.Queue.NextUp) != 1 || st2.Queue.NextUp[0].ID != 42 {
		t.Fatalf("下一首播放队列不符: %+v", st2.Queue.NextUp)
	}

	// 数据请求应按 ID 配对应答（未登录时报错）。
	resp, err := c.Request(ipc.TGetPlaylists, nil)
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	var pm ipc.PlaylistsMsg
	if err := resp.DecodeData(&pm); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}

	// 完全退出：服务端关闭连接并退出。
	if err := c.Send(ipc.TShutdown, nil); err != nil {
		t.Fatalf("发送 shutdown 失败: %v", err)
	}
	select {
	case <-c.Gone():
	case <-time.After(5 * time.Second):
		t.Fatal("服务端未在 shutdown 后断开连接")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("服务端退出异常: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("服务端未在 shutdown 后退出")
	}
}
