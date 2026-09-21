package server

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// unitName 是 systemd --user 服务名。
const unitName = "molpe.service"

// unitTemplate 是用户级服务单元模板；%s 为可执行文件路径。
// 不带 [Install] 段：后端按需拉起，不开机自启。
const unitTemplate = `[Unit]
Description=木末 Molpe 播放守护进程

[Service]
Type=simple
ExecStart=%s daemon
Restart=on-failure
`

// EnsureRunning 确保后端以 systemd --user 服务形式运行：
// 系统级单元（打包安装）缺失时，先在用户目录自写一份指向当前可执行文件的
// 单元，再启动服务。systemd 不可用时返回错误，由调用方提示手动运行
// `molpe daemon`。
func EnsureRunning() error {
	if _, err := exec.LookPath("systemctl"); err != nil {
		return errors.New("未找到 systemctl")
	}
	if exec.Command("systemctl", "--user", "cat", unitName).Run() != nil {
		// 单元不存在（未通过软件包安装）：自写用户级单元。
		exe, err := os.Executable()
		if err != nil {
			return fmt.Errorf("解析可执行文件路径失败: %w", err)
		}
		if exe, err = filepath.EvalSymlinks(exe); err != nil {
			return fmt.Errorf("解析可执行文件路径失败: %w", err)
		}
		dir := filepath.Join(xdgConfigHome(), "systemd", "user")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("创建 systemd 用户目录失败: %w", err)
		}
		unit := fmt.Sprintf(unitTemplate, exe)
		if err := os.WriteFile(filepath.Join(dir, unitName), []byte(unit), 0o644); err != nil {
			return fmt.Errorf("写入服务单元失败: %w", err)
		}
		if err := exec.Command("systemctl", "--user", "daemon-reload").Run(); err != nil {
			return fmt.Errorf("systemd daemon-reload 失败: %w", err)
		}
	}
	if err := exec.Command("systemctl", "--user", "start", unitName).Run(); err != nil {
		return fmt.Errorf("启动 %s 失败: %w", unitName, err)
	}
	return nil
}

func xdgConfigHome() string {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".config"
	}
	return filepath.Join(home, ".config")
}
