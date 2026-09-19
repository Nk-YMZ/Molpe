package main

import (
	"fmt"
	"os"
	"path/filepath"

	tea "charm.land/bubbletea/v2"

	"mountain-air/internal/config"
	"mountain-air/internal/netease"
	"mountain-air/internal/ui"
)

func main() {
	dirs := config.DefaultDirs()

	cfg, err := config.LoadConfig(dirs.Config)
	if err != nil {
		fmt.Fprintln(os.Stderr, "警告:", err)
	}
	cfg.Quality = netease.NormalizeQuality(cfg.Quality)

	client, err := netease.NewClient(filepath.Join(dirs.Data, "cookies"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "初始化失败:", err)
		os.Exit(1)
	}

	p := tea.NewProgram(ui.New(client, cfg.Quality))
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "运行失败:", err)
		os.Exit(1)
	}
}
