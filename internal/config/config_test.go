package config

import (
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
	zero, three := 0, 3
	c.LyricGapAbove, c.LyricGapBelow = &zero, &three
	if got := c.EffectiveLyricGapAbove(); got != 0 {
		t.Errorf("0 是合法的紧凑间距，得到 %d", got)
	}
	if got := c.EffectiveLyricGapBelow(); got != 3 {
		t.Errorf("EffectiveLyricGapBelow() = %d，预期 3", got)
	}
	neg, big := -1, 9
	c.LyricGapAbove, c.LyricGapBelow = &neg, &big
	if got := c.EffectiveLyricGapAbove(); got != 1 {
		t.Errorf("非法值应回退默认 1，得到 %d", got)
	}
	if got := c.EffectiveLyricGapBelow(); got != 1 {
		t.Errorf("非法值应回退默认 1，得到 %d", got)
	}
}
