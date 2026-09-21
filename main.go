package main

import (
	"errors"
	"fmt"
	"os"
	"time"

	tea "charm.land/bubbletea/v2"

	"molpe/internal/config"
	"molpe/internal/ipc"
	"molpe/internal/server"
	"molpe/internal/ui"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "daemon" {
		runDaemon()
		return
	}
	runTUI()
}

// runDaemon 以前台方式运行播放守护进程（由 systemd --user 托管，
// 也可手动运行）。
func runDaemon() {
	dirs := config.DefaultDirs()
	srv, err := server.New(dirs)
	if err != nil {
		fmt.Fprintln(os.Stderr, "初始化失败:", err)
		os.Exit(1)
	}
	if err := srv.ConfigError(); err != nil {
		// 配置损坏不阻断启动：以默认配置运行，且该次运行退出不写回。
		fmt.Fprintln(os.Stderr, "警告:", err)
	}
	if err := srv.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "运行失败:", err)
		os.Exit(1)
	}
}

// runTUI 连接后端并启动终端界面；后端未运行时先以 systemd --user
// 服务形式拉起。
func runTUI() {
	dirs := config.DefaultDirs()

	client, st, err := connect(dirs)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer client.Close()

	// 界面只消费配置中的显示相关字段（主题、歌词行数、区块间距）；
	// 播放相关字段由后端读取。主题/配置的写回统一由后端完成。
	cfg, cfgErr := config.LoadConfig(dirs.Config)
	if cfgErr != nil {
		fmt.Fprintln(os.Stderr, "警告:", cfgErr)
	}

	// 主题缺失或损坏不阻断启动：回退默认主题，界面内提示。
	if err := ui.EnsureThemes(dirs.Config); err != nil {
		fmt.Fprintln(os.Stderr, "警告:", err)
	}
	theme, themeErr := ui.LoadTheme(dirs.Config, cfg.Theme)

	p := tea.NewProgram(ui.New(client, st, cfg, dirs, theme, cfg.Theme, cfgErr, themeErr))
	fm, err := p.Run()
	if err != nil {
		fmt.Fprintln(os.Stderr, "运行失败:", err)
		os.Exit(1)
	}
	if m, ok := fm.(ui.Model); ok && m.ServerGone() {
		fmt.Fprintln(os.Stderr, "与播放服务的连接已断开")
	}
}

// connect 连接后端；未运行时通过 systemd --user 拉起并等待 socket 就绪。
func connect(dirs config.Dirs) (*ipc.Client, ipc.State, error) {
	sock := ipc.SocketPath(dirs.Cache)
	client, st, err := ipc.Dial(sock)
	if err == nil {
		return client, st, nil
	}
	if errors.Is(err, ipc.ErrBusy) {
		return nil, st, fmt.Errorf("%w，请先关闭该界面", ipc.ErrBusy)
	}
	if err := server.EnsureRunning(); err != nil {
		return nil, st, fmt.Errorf("启动播放服务失败: %w（也可手动运行 molpe daemon 后再启动界面）", err)
	}
	// 等待服务完成初始化（含 Cookie 加载与队列恢复）。
	deadline := time.Now().Add(10 * time.Second)
	for {
		client, st, err = ipc.Dial(sock)
		if err == nil {
			return client, st, nil
		}
		if errors.Is(err, ipc.ErrBusy) {
			return nil, st, fmt.Errorf("%w，请先关闭该界面", ipc.ErrBusy)
		}
		if time.Now().After(deadline) {
			return nil, st, fmt.Errorf("等待播放服务就绪超时: %w", err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
