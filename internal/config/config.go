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

const appName = "molpe"

const configFile = "config.json"

// Config 是面向用户的配置文件内容。
type Config struct {
	// Quality 音质偏好：standard / higher / exhigh / lossless / hires。
	// 修改后重新启动程序生效。
	Quality string `json:"quality"`
	// AutoPlay 启动后自动继续播放上次退出时的歌曲；默认 false（恢复为待播状态）。
	AutoPlay bool `json:"auto_play,omitempty"`
	// Volume 播放音量百分比（0-100）；null 或缺失时使用默认值 100。
	Volume *int `json:"volume,omitempty"`
	// VolumeStep 音量调节步进百分比；<=0 或 >100 时使用默认值 5。
	VolumeStep int `json:"volume_step,omitempty"`
}

const (
	defaultVolume     = 100
	defaultVolumeStep = 5
)

// DefaultConfig 返回默认配置。
func DefaultConfig() Config {
	return Config{Quality: "lossless"}
}

// EffectiveVolume 返回生效的音量百分比（0-100）。
func (c Config) EffectiveVolume() int {
	if c.Volume == nil || *c.Volume < 0 || *c.Volume > 100 {
		return defaultVolume
	}
	return *c.Volume
}

// EffectiveVolumeStep 返回生效的音量调节步进百分比。
func (c Config) EffectiveVolumeStep() int {
	if c.VolumeStep <= 0 || c.VolumeStep > 100 {
		return defaultVolumeStep
	}
	return c.VolumeStep
}

// LoadConfig 从 dir 下的 config.json 读取配置；文件不存在时写入并返回默认配置。
func LoadConfig(dir string) (Config, error) {
	cfg := DefaultConfig()
	data, err := os.ReadFile(filepath.Join(dir, configFile))
	if errors.Is(err, fs.ErrNotExist) {
		if werr := SaveConfig(dir, cfg); werr != nil {
			return cfg, fmt.Errorf("写入默认配置失败: %w", werr)
		}
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
	return SaveJSON(dir, configFile, cfg)
}

// LoadJSON 从 dir 下的 name 文件读取 JSON 到 v；文件不存在时返回 nil（v 保持不变）。
func LoadJSON(dir, name string, v any) error {
	data, err := os.ReadFile(filepath.Join(dir, name))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("读取 %s 失败: %w", name, err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("解析 %s 失败: %w", name, err)
	}
	return nil
}

// SaveJSON 将 v 以 JSON 写入 dir 下的 name 文件（目录不存在时自动创建）。
func SaveJSON(dir, name string, v any) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("创建目录 %s 失败: %w", dir, err)
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化 %s 失败: %w", name, err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
		return fmt.Errorf("写入 %s 失败: %w", name, err)
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
