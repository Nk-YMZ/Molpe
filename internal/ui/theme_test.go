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

func TestParseThemeColor(t *testing.T) {
	cases := []struct {
		in      string
		r, g, b int
	}{
		{"#102030", 0x10, 0x20, 0x30},
		{"15", 0xff, 0xff, 0xff},  // ANSI 15 亮白
		{"238", 0x44, 0x44, 0x44}, // 灰度阶梯：8+10*6
		{"196", 0xff, 0x00, 0x00}, // 颜色立方体：level(5,0,0)
		{"21", 0x00, 0x00, 0xff},  // 颜色立方体：16+5，level(0,0,5)
	}
	for _, c := range cases {
		r, g, b, ok := parseThemeColor(c.in)
		if !ok || r != c.r || g != c.g || b != c.b {
			t.Errorf("parseThemeColor(%q) = (%d,%d,%d,%v)，预期 (%d,%d,%d,true)", c.in, r, g, b, ok, c.r, c.g, c.b)
		}
	}
	for _, bad := range []string{"", "oops", "#12345", "#1234567", "256", "-1"} {
		if _, _, _, ok := parseThemeColor(bad); ok {
			t.Errorf("parseThemeColor(%q) 应失败", bad)
		}
	}
}

func TestBuildLyricRamp(t *testing.T) {
	// 端点无法解析时回退 nil，由渲染层用前景/微光两档。
	if got := buildLyricRamp("oops", "#000000"); got != nil {
		t.Errorf("非法端点应返回 nil: %v", got)
	}
	ramp := buildLyricRamp("#ffffff", "#000000")
	if len(ramp) != lyricRampSteps {
		t.Fatalf("渐变档数 = %d，预期 %d", len(ramp), lyricRampSteps)
	}
	// 首末档精确落在两个主题色上（渐变档为 hex 色，底层类型是 color.RGBA）。
	if got, ok := ramp[0].GetForeground().(color.RGBA); !ok || got != (color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}) {
		t.Errorf("首档应为前景色: %v", ramp[0].GetForeground())
	}
	if got, ok := ramp[len(ramp)-1].GetForeground().(color.RGBA); !ok || got != (color.RGBA{A: 0xff}) {
		t.Errorf("末档应为微光色: %v", ramp[len(ramp)-1].GetForeground())
	}
	// 中间档严格单调变暗。
	prev := 0xff
	for i := 1; i < len(ramp)-1; i++ {
		c, ok := ramp[i].GetForeground().(color.RGBA)
		if !ok {
			t.Fatalf("第 %d 档颜色类型不符: %T", i, ramp[i].GetForeground())
		}
		if int(c.R) >= prev {
			t.Errorf("第 %d 档应严格单调变暗: %v", i, c)
		}
		prev = int(c.R)
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
