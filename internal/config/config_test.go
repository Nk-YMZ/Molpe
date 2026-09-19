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
