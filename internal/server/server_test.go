package server

import (
	"errors"
	"path/filepath"
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

// TestPlayFailureConvergence 播放启动失败后的状态收敛：
// 当前曲置为目标曲并进入待播（暂停）重试状态，快照对外暴露 NetRetry；
// 取消重试后状态清除。
func TestPlayFailureConvergence(t *testing.T) {
	s := &Server{
		queue:   queue.New(nil, queue.ModeLoop, queue.Options{}),
		quality: "lossless",
		results: make(chan any, 1),
	}
	song := netease.Song{ID: 1, Name: "歌", Artists: "人"}
	s.handlePlayFailure(song, errors.New("网络不可达"))

	if s.playing == nil || s.playing.Song.ID != 1 {
		t.Fatalf("失败后当前曲应为目标曲: %+v", s.playing)
	}
	if !s.paused {
		t.Fatal("失败后应收敛为待播（暂停）状态")
	}
	if s.retrySong == nil || s.retrySong.ID != 1 {
		t.Fatalf("应记录待重试曲目: %+v", s.retrySong)
	}
	if !s.state().NetRetry {
		t.Fatal("快照应暴露 NetRetry")
	}
	if s.retryAttempt != 1 || s.retryTimer == nil {
		t.Fatalf("应安排首次重试: attempt=%d timer=%v", s.retryAttempt, s.retryTimer)
	}

	// 切歌/点歌等操作取消重试后：状态清除，到期定时器被丢弃。
	s.cancelRetry()
	if s.retrySong != nil || s.retryAttempt != 0 || s.state().NetRetry {
		t.Fatal("取消重试后状态未清除")
	}
	s.handleRetry(retryResult{seq: 0}) // 残留定时器（序号已过期）
	if s.retrySong != nil {
		t.Fatal("过期重试定时器不应生效")
	}
}

// TestToggleDuringLoading 加载中按播放键只翻转播放意图：
// 不触碰播放器与位置锚点，loading 状态保持不变。
func TestToggleDuringLoading(t *testing.T) {
	s := &Server{
		queue:   queue.New(nil, queue.ModeLoop, queue.Options{}),
		quality: "lossless",
		loading: true,
		playing: &ipc.Playing{Song: netease.Song{ID: 1, Name: "歌"}},
	}
	s.toggle()
	if !s.paused || !s.loading {
		t.Fatalf("加载中暂停应仅翻转意图位: paused=%v loading=%v", s.paused, s.loading)
	}
	if !s.state().Loading || !s.state().Paused {
		t.Fatal("快照应反映加载中与暂停意图")
	}
	s.toggle()
	if s.paused || !s.loading {
		t.Fatalf("再次按下应恢复播放意图: paused=%v loading=%v", s.paused, s.loading)
	}
}

// TestPlayerErrorConvergence mpv 流播放中途失败（end-file reason=error）：
// 收敛为待播重试状态；加载中、已暂停、已在重试时的事件不处理。
func TestPlayerErrorConvergence(t *testing.T) {
	newServer := func() *Server {
		return &Server{
			queue:   queue.New(nil, queue.ModeLoop, queue.Options{}),
			quality: "lossless",
			results: make(chan any, 1),
			playing: &ipc.Playing{Song: netease.Song{ID: 1, Name: "歌"}},
		}
	}

	// 播放中流失败 → 收敛为待播重试。
	s := newServer()
	s.handlePlayerError()
	if s.retrySong == nil || !s.paused || !s.state().NetRetry {
		t.Fatalf("流失败应收敛为待播重试: retry=%v paused=%v", s.retrySong, s.paused)
	}
	s.cancelRetry()

	// 加载中（旧流事件）不处理。
	s = newServer()
	s.loading = true
	s.handlePlayerError()
	if s.retrySong != nil {
		t.Fatal("加载中的旧流事件不应触发重试")
	}

	// 用户暂停期间不处理（恢复播放时再按结果收敛）。
	s = newServer()
	s.paused = true
	s.handlePlayerError()
	if s.retrySong != nil {
		t.Fatal("暂停期间的流事件不应触发重试")
	}

	// 已在重试管道中不重复收敛。
	s = newServer()
	s.retrySong = &netease.Song{ID: 1}
	s.handlePlayerError()
	if s.retryAttempt != 0 {
		t.Fatal("重试中的流事件不应重复收敛")
	}
}

// TestPlayerExitedRecovery mpv 进程异常退出：播放中接入待播重试管道，
// 已暂停时仅清理（下次播放重建）。
func TestPlayerExitedRecovery(t *testing.T) {
	s := &Server{
		queue:   queue.New(nil, queue.ModeLoop, queue.Options{}),
		quality: "lossless",
		results: make(chan any, 1),
		playing: &ipc.Playing{Song: netease.Song{ID: 1, Name: "歌"}},
	}
	s.handlePlayerExited()
	if s.retrySong == nil || !s.paused || !s.state().NetRetry {
		t.Fatal("播放中进程退出应接入待播重试")
	}
}

// TestHandleLoginKeepsAccountOnError 登录态查询失败（网络错误）时保留现有
// 登录态，不误判为未登录。
func TestHandleLoginKeepsAccountOnError(t *testing.T) {
	s := &Server{
		queue:   queue.New(nil, queue.ModeLoop, queue.Options{}),
		quality: "lossless",
		results: make(chan any, 1),
		account: &netease.Account{ID: 7, Nickname: "某人"},
	}
	s.handleLogin(loginResult{err: errors.New("网络错误")})
	if s.account == nil || s.account.ID != 7 {
		t.Fatal("查询失败不应清除登录态")
	}
	if !s.checked {
		t.Fatal("查询结束应标记已检查")
	}
}

// TestRefresh 全局刷新：催动等待中的网络重试（转入加载中），不中断当前状态。
func TestRefresh(t *testing.T) {
	client, err := netease.NewClient(filepath.Join(t.TempDir(), "cookies"))
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}
	s := &Server{
		client:    client,
		queue:     queue.New(nil, queue.ModeLoop, queue.Options{}),
		quality:   "lossless",
		results:   make(chan any, 8),
		playing:   &ipc.Playing{Song: netease.Song{ID: 1, Name: "歌"}},
		retrySong: &netease.Song{ID: 1, Name: "歌"},
	}
	s.refresh()
	// 等待中的重试被立即催动：清空待重试并转入加载中（异步拉地址）。
	if s.retrySong != nil || !s.loading {
		t.Fatalf("refresh 应立即重试待播曲目: retry=%v loading=%v", s.retrySong, s.loading)
	}
	if !s.state().Loading {
		t.Fatal("快照应反映加载中")
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
