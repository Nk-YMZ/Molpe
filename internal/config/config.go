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

const defaultTheme = "default"

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
	// LyricTranslation 歌词是否包含翻译；缺失时默认 true。
	LyricTranslation *bool `json:"lyric_translation,omitempty"`
	// LyricLines 歌词显示总行数；<1 或 >15 时使用默认值 5。
	LyricLines int `json:"lyric_lines,omitempty"`
	// LyricGapAbove 歌词区与上方正文区之间的空行数；null、<0 或 >10 时使用默认值 1。
	LyricGapAbove *int `json:"lyric_gap_above,omitempty"`
	// LyricGapBelow 歌词区与下方进度条之间的空行数；null、<0 或 >10 时使用默认值 1。
	LyricGapBelow *int `json:"lyric_gap_below,omitempty"`
	// PlaybackGapBelow 播放信息块（进度条 + 当前歌曲信息）与操作帮助行之间的空行数；null、<0 或 >10 时使用默认值 1。
	PlaybackGapBelow *int `json:"playback_gap_below,omitempty"`
	// Theme 主题名；内置 default/ember，其他名称对应扩展目录 themes/<name>.json；空使用默认主题。
	Theme string `json:"theme,omitempty"`
}

const (
	defaultVolume     = 100
	defaultVolumeStep = 5
	defaultLyricLines = 5
	maxLyricLines     = 15
	defaultSectionGap = 1
	maxSectionGap     = 10
)

// DefaultConfig 返回默认配置。
func DefaultConfig() Config {
	volume := defaultVolume
	translation := true
	above, below, playbackBelow := defaultSectionGap, defaultSectionGap, defaultSectionGap
	return Config{
		Quality:          "lossless",
		Volume:           &volume,
		VolumeStep:       defaultVolumeStep,
		LyricTranslation: &translation,
		LyricLines:       defaultLyricLines,
		LyricGapAbove:    &above,
		LyricGapBelow:    &below,
		PlaybackGapBelow: &playbackBelow,
		Theme:            defaultTheme,
	}
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

// EffectiveLyricTranslation 返回歌词是否包含翻译。
func (c Config) EffectiveLyricTranslation() bool {
	if c.LyricTranslation == nil {
		return true
	}
	return *c.LyricTranslation
}

// EffectiveLyricLines 返回生效的歌词显示总行数。
func (c Config) EffectiveLyricLines() int {
	if c.LyricLines < 1 || c.LyricLines > maxLyricLines {
		return defaultLyricLines
	}
	return c.LyricLines
}

// EffectiveLyricGapAbove 返回歌词区与上方正文区之间的生效空行数。
func (c Config) EffectiveLyricGapAbove() int {
	return effectiveGap(c.LyricGapAbove)
}

// EffectiveLyricGapBelow 返回歌词区与下方进度条之间的生效空行数。
func (c Config) EffectiveLyricGapBelow() int {
	return effectiveGap(c.LyricGapBelow)
}

// EffectivePlaybackGapBelow 返回播放信息块与操作帮助行之间的生效空行数。
func (c Config) EffectivePlaybackGapBelow() int {
	return effectiveGap(c.PlaybackGapBelow)
}

// effectiveGap 校验界面区块间距：null 或越界回退默认值，0 是合法的紧凑间距。
func effectiveGap(v *int) int {
	if v == nil || *v < 0 || *v > maxSectionGap {
		return defaultSectionGap
	}
	return *v
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

// configHelp 是 config.json 内的自说明字段。标准 JSON 不支持注释，
// 因此用读取时会被忽略的 _说明 对象记录每项含义与合法取值。
type configHelp struct {
	Quality          string `json:"quality"`
	AutoPlay         string `json:"auto_play"`
	Volume           string `json:"volume"`
	VolumeStep       string `json:"volume_step"`
	LyricTranslation string `json:"lyric_translation"`
	LyricLines       string `json:"lyric_lines"`
	LyricGapAbove    string `json:"lyric_gap_above"`
	LyricGapBelow    string `json:"lyric_gap_below"`
	PlaybackGapBelow string `json:"playback_gap_below"`
	Theme            string `json:"theme"`
}

// configFileData 是写入磁盘的完整配置模板。字段不使用 omitempty，确保新建文件
// 总是列出所有可用配置项；_说明 会在后续保存时继续保留。
type configFileData struct {
	Help             configHelp `json:"_说明"`
	Quality          string     `json:"quality"`
	AutoPlay         bool       `json:"auto_play"`
	Volume           int        `json:"volume"`
	VolumeStep       int        `json:"volume_step"`
	LyricTranslation bool       `json:"lyric_translation"`
	LyricLines       int        `json:"lyric_lines"`
	LyricGapAbove    int        `json:"lyric_gap_above"`
	LyricGapBelow    int        `json:"lyric_gap_below"`
	PlaybackGapBelow int        `json:"playback_gap_below"`
	Theme            string     `json:"theme"`
}

func newConfigFileData(cfg Config) configFileData {
	theme := cfg.Theme
	if theme == "" {
		theme = defaultTheme
	}
	return configFileData{
		Help: configHelp{
			Quality:          "音质偏好；可选值：standard（标准）、higher（较高）、exhigh（极高）、lossless（无损）、hires（Hi-Res）；修改后重启生效",
			AutoPlay:         "启动后是否自动继续播放上次退出时的歌曲；可选值：true、false",
			Volume:           "播放音量百分比；整数，范围：0-100",
			VolumeStep:       "按 +/- 调节音量时的步进百分比；整数，范围：1-100",
			LyricTranslation: "歌词是否显示翻译；可选值：true、false",
			LyricLines:       "歌词区域显示的总行数；整数，范围：1-15",
			LyricGapAbove:    "歌词区域与上方正文区域之间的空行数；整数，范围：0-10",
			LyricGapBelow:    "歌词区域与下方进度条之间的空行数；整数，范围：0-10",
			PlaybackGapBelow: "播放信息块（进度条和当前歌曲信息）与操作帮助行之间的空行数；整数，范围：0-10",
			Theme:            "主题名称；内置值：default、ember；其他字符串对应扩展目录 themes/<名称>.json",
		},
		Quality:          cfg.Quality,
		AutoPlay:         cfg.AutoPlay,
		Volume:           cfg.EffectiveVolume(),
		VolumeStep:       cfg.EffectiveVolumeStep(),
		LyricTranslation: cfg.EffectiveLyricTranslation(),
		LyricLines:       cfg.EffectiveLyricLines(),
		LyricGapAbove:    cfg.EffectiveLyricGapAbove(),
		LyricGapBelow:    cfg.EffectiveLyricGapBelow(),
		PlaybackGapBelow: cfg.EffectivePlaybackGapBelow(),
		Theme:            theme,
	}
}

// SaveConfig 将带完整说明的配置写入 dir 下的 config.json。
func SaveConfig(dir string, cfg Config) error {
	return SaveJSON(dir, configFile, newConfigFileData(cfg))
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
