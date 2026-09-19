package main

import (
	"fmt"
	"os"
	"path/filepath"

	tea "charm.land/bubbletea/v2"

	"molpe/internal/config"
	"molpe/internal/netease"
	"molpe/internal/ui"
)

func main() {
	dirs := config.DefaultDirs()

	cfg, cfgErr := config.LoadConfig(dirs.Config)
	if cfgErr != nil {
		// 配置损坏不阻断启动：界面内会提示，并以默认配置运行。
		fmt.Fprintln(os.Stderr, "警告:", cfgErr)
	}
	cfg.Quality = netease.NormalizeQuality(cfg.Quality)

	client, err := netease.NewClient(filepath.Join(dirs.Data, "cookies"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "初始化失败:", err)
		os.Exit(1)
	}

	// 主题缺失或损坏不阻断启动：回退默认主题，界面内提示。
	if err := ui.EnsureThemes(dirs.Config); err != nil {
		fmt.Fprintln(os.Stderr, "警告:", err)
	}
	theme, themeErr := ui.LoadTheme(dirs.Config, cfg.Theme)

	p := tea.NewProgram(ui.New(client, cfg, dirs, theme, cfg.Theme, cfgErr, themeErr))
	fm, err := p.Run()
	// 兜底清理：SIGTERM 等信号路径不会进入界面的退出分支，
	// 必须在这里确保 mpv 子进程与 D-Bus 资源被释放。
	if m, ok := fm.(ui.Model); ok {
		m.Cleanup()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "运行失败:", err)
		os.Exit(1)
	}
}
