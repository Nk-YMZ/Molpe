package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestEffectiveVolumeDefaults(t *testing.T) {
	c := DefaultConfig()
	if got := c.EffectiveVolume(); got != 100 {
		t.Errorf("EffectiveVolume() = %d，预期 100", got)
	}
	if got := c.EffectiveVolumeStep(); got != 5 {
		t.Errorf("EffectiveVolumeStep() = %d，预期 5", got)
	}

	bad := -3
	c.Volume = &bad
	if got := c.EffectiveVolume(); got != 100 {
		t.Errorf("非法音量应回退默认 100，得到 %d", got)
	}
	ok := 40
	c.Volume = &ok
	if got := c.EffectiveVolume(); got != 40 {
		t.Errorf("EffectiveVolume() = %d，预期 40", got)
	}
	c.VolumeStep = 0
	if got := c.EffectiveVolumeStep(); got != 5 {
		t.Errorf("非法步进应回退默认 5，得到 %d", got)
	}
}

func TestDefaultDirsRespectsXDGEnv(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg-config")
	t.Setenv("XDG_DATA_HOME", "/tmp/xdg-data")
	t.Setenv("XDG_CACHE_HOME", "/tmp/xdg-cache")

	dirs := DefaultDirs()

	if dirs.Config != filepath.Join("/tmp/xdg-config", appName) {
		t.Errorf("Config = %q", dirs.Config)
	}
	if dirs.Data != filepath.Join("/tmp/xdg-data", appName) {
		t.Errorf("Data = %q", dirs.Data)
	}
	if dirs.Cache != filepath.Join("/tmp/xdg-cache", appName) {
		t.Errorf("Cache = %q", dirs.Cache)
	}
}

func TestDefaultDirsFallback(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")

	dirs := DefaultDirs()
	for name, dir := range map[string]string{
		"Config": dirs.Config,
		"Data":   dirs.Data,
		"Cache":  dirs.Cache,
	} {
		if !filepath.IsAbs(dir) {
			t.Errorf("%s = %q, 期望绝对路径", name, dir)
		}
	}
}

func TestEffectiveLyricGapDefaults(t *testing.T) {
	c := DefaultConfig()
	if got := c.EffectiveLyricGapAbove(); got != 1 {
		t.Errorf("EffectiveLyricGapAbove() = %d，预期 1", got)
	}
	if got := c.EffectiveLyricGapBelow(); got != 1 {
		t.Errorf("EffectiveLyricGapBelow() = %d，预期 1", got)
	}
	zero, ten := 0, 10
	c.LyricGapAbove, c.LyricGapBelow = &zero, &ten
	if got := c.EffectiveLyricGapAbove(); got != 0 {
		t.Errorf("0 是合法的紧凑间距，得到 %d", got)
	}
	if got := c.EffectiveLyricGapBelow(); got != 10 {
		t.Errorf("EffectiveLyricGapBelow() = %d，预期 10", got)
	}
	neg, big := -1, 11
	c.LyricGapAbove, c.LyricGapBelow = &neg, &big
	if got := c.EffectiveLyricGapAbove(); got != 1 {
		t.Errorf("非法值应回退默认 1，得到 %d", got)
	}
	if got := c.EffectiveLyricGapBelow(); got != 1 {
		t.Errorf("非法值应回退默认 1，得到 %d", got)
	}
	if got := c.EffectivePlaybackGapBelow(); got != 1 {
		t.Errorf("EffectivePlaybackGapBelow() = %d，预期 1", got)
	}
	c.PlaybackGapBelow = &ten
	if got := c.EffectivePlaybackGapBelow(); got != 10 {
		t.Errorf("EffectivePlaybackGapBelow() = %d，预期 10", got)
	}
	c.PlaybackGapBelow = &big
	if got := c.EffectivePlaybackGapBelow(); got != 1 {
		t.Errorf("非法播放信息块间距应回退默认 1，得到 %d", got)
	}
}

func TestLoadConfigCreatesCompleteTemplate(t *testing.T) {
	dir := t.TempDir()
	cfg, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("首次加载配置失败: %v", err)
	}
	if cfg.Quality != "lossless" || cfg.EffectiveVolume() != 100 || cfg.Theme != defaultTheme {
		t.Fatalf("返回的默认配置不完整: %+v", cfg)
	}

	data, err := os.ReadFile(filepath.Join(dir, configFile))
	if err != nil {
		t.Fatalf("读取生成的配置模板失败: %v", err)
	}
	var file configFileData
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatalf("配置模板不是合法 JSON: %v", err)
	}
	if file.Quality != "lossless" || file.AutoPlay || file.Volume != 100 || file.VolumeStep != 5 {
		t.Errorf("基础配置默认值不符: %+v", file)
	}
	if !file.LyricTranslation || file.LyricLines != 5 || file.LyricGapAbove != 1 || file.LyricGapBelow != 1 || file.PlaybackGapBelow != 1 {
		t.Errorf("歌词配置默认值不符: %+v", file)
	}
	if file.Theme != defaultTheme {
		t.Errorf("默认主题 = %q，预期 %q", file.Theme, defaultTheme)
	}
	for name, help := range map[string]string{
		"quality":            file.Help.Quality,
		"auto_play":          file.Help.AutoPlay,
		"volume":             file.Help.Volume,
		"volume_step":        file.Help.VolumeStep,
		"lyric_translation":  file.Help.LyricTranslation,
		"lyric_lines":        file.Help.LyricLines,
		"lyric_gap_above":    file.Help.LyricGapAbove,
		"lyric_gap_below":    file.Help.LyricGapBelow,
		"playback_gap_below": file.Help.PlaybackGapBelow,
		"theme":              file.Help.Theme,
	} {
		if help == "" {
			t.Errorf("%s 缺少说明", name)
		}
	}

	// 后续运行保存配置时仍保留说明，而不是把模板覆盖成无注释的精简结构。
	volume := 42
	cfg.Volume = &volume
	if err := SaveConfig(dir, cfg); err != nil {
		t.Fatalf("保存配置失败: %v", err)
	}
	data, err = os.ReadFile(filepath.Join(dir, configFile))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatal(err)
	}
	if file.Volume != 42 || file.Help.Volume == "" {
		t.Errorf("保存后数值或说明丢失: %+v", file)
	}

	// _说明 是未知字段，正常读取配置时应被安全忽略。
	reloaded, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("重新加载自说明配置失败: %v", err)
	}
	if reloaded.EffectiveVolume() != 42 {
		t.Errorf("重新加载音量 = %d，预期 42", reloaded.EffectiveVolume())
	}
}
