package server

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"molpe/internal/config"
)

// EnsureRunning 按需启动与当前前端同版本、同环境的后端。
// 独立会话让 Q 转后台后，关闭终端也不向后端发送 SIGHUP。
// Run 持有的文件锁负责防止并发启动覆盖现有 socket。
func EnsureRunning() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("解析可执行文件路径失败: %w", err)
	}
	dirs := config.DefaultDirs()
	if err := os.MkdirAll(dirs.Cache, 0o700); err != nil {
		return fmt.Errorf("创建日志目录失败: %w", err)
	}
	log, err := os.OpenFile(filepath.Join(dirs.Cache, "daemon.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("打开后端日志失败: %w", err)
	}
	defer log.Close()
	cmd := exec.Command(exe, "daemon")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Stdout, cmd.Stderr = log, log
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动后端失败: %w", err)
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
