package ui

import (
	"image/color"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadThemeMergesDefaults(t *testing.T) {
	dir := t.TempDir()
	content := `{"colors":{"accent":"12"},"glyphs":{"cursor":"> "}}`
	if err := os.MkdirAll(filepath.Join(dir, themesDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, themesDir, "custom.json"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	th, err := LoadTheme(dir, "custom")
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	if th.Colors.Accent != "12" || th.Glyphs.Cursor != "> " {
		t.Errorf("覆盖字段未生效: %+v", th)
	}
	d := DefaultTheme()
	if th.Colors.Foreground != d.Colors.Foreground || th.Glyphs.Playing != d.Glyphs.Playing {
		t.Errorf("未指定字段未保留默认值: %+v", th)
	}
}

func TestLoadThemeEmptyColorFallsBack(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, themesDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, themesDir, "empty.json"), []byte(`{"colors":{"accent":""}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	th, err := LoadTheme(dir, "empty")
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	if th.Colors.Accent != DefaultTheme().Colors.Accent {
		t.Errorf("空配色未回退默认值: %q", th.Colors.Accent)
	}
}

func TestLoadThemeMissing(t *testing.T) {
	dir := t.TempDir()
	if _, err := LoadTheme(dir, "nonexistent"); err == nil {
		t.Error("缺失主题应返回错误")
	}
	if _, err := LoadTheme(dir, "../etc/passwd"); err == nil {
		t.Error("非法主题名应返回错误")
	}
}

func TestLoadThemeCorrupt(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, themesDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, themesDir, "bad.json"), []byte(`{`), 0o644); err != nil {
		t.Fatal(err)
	}
	th, err := LoadTheme(dir, "bad")
	if err == nil {
		t.Error("损坏主题应返回错误")
	}
	if th != DefaultTheme() {
		t.Error("损坏主题应回退默认主题")
	}
}

func TestEnsureThemesAndList(t *testing.T) {
	dir := t.TempDir()
	if err := EnsureThemes(dir); err != nil {
		t.Fatalf("创建扩展主题目录失败: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, themesDir, defaultThemeName+".json")); !os.IsNotExist(err) {
		t.Fatalf("内置默认主题不应写入配置目录: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, themesDir, emberThemeName+".json")); !os.IsNotExist(err) {
		t.Fatalf("内置 ember 主题不应写入配置目录: %v", err)
	}
	if _, err := LoadTheme(dir, ""); err != nil {
		t.Fatalf("默认主题应可加载: %v", err)
	}
	ember, err := LoadTheme(dir, emberThemeName)
	if err != nil {
		t.Fatalf("ember 内置主题应可加载: %v", err)
	}
	if ember.Colors.Accent != "#e0a458" || ember.Colors.Background != "#000000" {
		t.Errorf("ember 内置主题内容不符: %+v", ember.Colors)
	}
	// 幂等：重复调用不报错
	if err := EnsureThemes(dir); err != nil {
		t.Fatalf("重复调用失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, themesDir, "b.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	names, err := ListThemes(dir)
	if err != nil {
		t.Fatalf("扫描失败: %v", err)
	}
	if len(names) != 3 || names[0] != "b" || names[1] != defaultThemeName || names[2] != emberThemeName {
		t.Errorf("主题列表不符: %v", names)
	}
}

func TestListThemesWithoutExtensionDirectory(t *testing.T) {
	names, err := ListThemes(t.TempDir())
	if err != nil {
		t.Fatalf("无扩展目录时仍应列出内置主题: %v", err)
	}
	if len(names) != 2 || names[0] != defaultThemeName || names[1] != emberThemeName {
		t.Errorf("内置主题列表不符: %v", names)
	}
}

func TestBackgroundColor(t *testing.T) {
	// 缺省与非法值均回退纯黑。
	if got := (Theme{}).BackgroundColor(); got != color.Black {
		t.Errorf("缺省背景应为纯黑: %v", got)
	}
	th := Theme{Colors: Colors{Background: "oops"}}
	if got := th.BackgroundColor(); got != color.Black {
		t.Errorf("非法背景应回退纯黑: %v", got)
	}
	th.Colors.Background = "#102030"
	r, g, b, _ := th.BackgroundColor().RGBA()
	if r>>8 != 0x10 || g>>8 != 0x20 || b>>8 != 0x30 {
		t.Errorf("背景色解析不符: %v", th.BackgroundColor())
	}
}
