// Package config 负责应用目录解析与配置管理。
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

const appName = "mountain-air"

const configFile = "config.json"

// Config 是面向用户的配置文件内容。
type Config struct {
	// Quality 音质偏好：standard / higher / exhigh / lossless / hires。
	// 修改后重新启动程序生效。
	Quality string `json:"quality"`
}

// DefaultConfig 返回默认配置。
func DefaultConfig() Config {
	return Config{Quality: "lossless"}
}

// LoadConfig 从 dir 下的 config.json 读取配置；文件不存在时返回默认配置。
func LoadConfig(dir string) (Config, error) {
	cfg := DefaultConfig()
	data, err := os.ReadFile(filepath.Join(dir, configFile))
	if errors.Is(err, fs.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, fmt.Errorf("读取配置文件失败: %w", err)
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return DefaultConfig(), fmt.Errorf("解析配置文件失败: %w", err)
	}
	return cfg, nil
}

// SaveConfig 将配置写入 dir 下的 config.json。
func SaveConfig(dir string, cfg Config) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("创建配置目录失败: %w", err)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化配置失败: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, configFile), data, 0o644); err != nil {
		return fmt.Errorf("写入配置文件失败: %w", err)
	}
	return nil
}

// Dirs 描述应用使用的 XDG 目录。
type Dirs struct {
	Config string
	Data   string
	Cache  string
}

// DefaultDirs 按 XDG 目录规范解析应用目录。
func DefaultDirs() Dirs {
	return Dirs{
		Config: filepath.Join(xdgDir("XDG_CONFIG_HOME", ".config"), appName),
		Data:   filepath.Join(xdgDir("XDG_DATA_HOME", ".local/share"), appName),
		Cache:  filepath.Join(xdgDir("XDG_CACHE_HOME", ".cache"), appName),
	}
}

func xdgDir(env, fallback string) string {
	if dir := os.Getenv(env); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return fallback
	}
	return filepath.Join(home, fallback)
}
