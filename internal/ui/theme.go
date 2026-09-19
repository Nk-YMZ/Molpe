package ui

import (
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"molpe/internal/config"
)

// themesDir 是配置目录下存放主题文件的子目录。
const themesDir = "themes"

// defaultThemeName 是默认主题名（对应 default.json）。
const defaultThemeName = "default"

// Colors 主题配色；值为 ANSI 色号（如 "15"）或十六进制颜色（如 "#ffffff"）。
type Colors struct {
	Background string `json:"background"`
	Foreground string `json:"foreground"`
	Muted      string `json:"muted"`
	Accent     string `json:"accent"`
	Error      string `json:"error"`
}

// fillDefaults 将空字符串配色回退为默认值；配色为空会破坏显示，
// 而符号允许用户显式置空，故 Glyphs 不做同样处理。
func (c *Colors) fillDefaults() {
	d := DefaultTheme().Colors
	if c.Background == "" {
		c.Background = d.Background
	}
	if c.Foreground == "" {
		c.Foreground = d.Foreground
	}
	if c.Muted == "" {
		c.Muted = d.Muted
	}
	if c.Accent == "" {
		c.Accent = d.Accent
	}
	if c.Error == "" {
		c.Error = d.Error
	}
}

// Glyphs 主题符号表；仅替换已有元素的显示字符，
// 不影响布局、尺寸与交互行为。
type Glyphs struct {
	Cursor          string `json:"cursor"`            // 列表光标前缀
	Playing         string `json:"playing"`           // 播放中
	Paused          string `json:"paused"`            // 已暂停
	PlaylistCreated string `json:"playlist_created"`  // 创建的歌单标记
	PlaylistStarred string `json:"playlist_starred"`  // 收藏的歌单标记
	PopupHeaderRule string `json:"popup_header_rule"` // 弹窗分区标题两侧的装饰线
}

// Theme 主题：配色 + 符号。布局与尺寸由代码固定，不属于主题；
// 歌词行数等数值是全局设定，统一在 config.json 中配置。
type Theme struct {
	Colors Colors `json:"colors"`
	Glyphs Glyphs `json:"glyphs"`
}

// DefaultTheme 返回纯黑背景的默认主题。
func DefaultTheme() Theme {
	return Theme{
		Colors: Colors{
			Background: "#000000",
			Foreground: "15",
			Muted:      "8",
			Accent:     "10",
			Error:      "9",
		},
		Glyphs: Glyphs{
			Cursor:          "▸ ",
			Playing:         "▶ ",
			Paused:          "⏸ ",
			PlaylistCreated: "✎",
			PlaylistStarred: "♥",
			PopupHeaderRule: "─",
		},
	}
}

// BackgroundColor 返回终端背景色。
func (t Theme) BackgroundColor() color.Color { return lipgloss.Color(t.Colors.Background) }

// EnsureThemes 确保主题目录存在且默认主题文件已写入（便于用户复制修改）。
func EnsureThemes(dir string) error {
	path := filepath.Join(dir, themesDir, defaultThemeName+".json")
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	if err := config.SaveJSON(filepath.Join(dir, themesDir), defaultThemeName+".json", DefaultTheme()); err != nil {
		return fmt.Errorf("写入默认主题失败: %w", err)
	}
	return nil
}

// LoadTheme 从 dir/themes/ 下加载名为 name 的主题（不含扩展名）；
// 空名称使用默认主题。主题文件与默认值逐字段合并：未指定的字段
// 保留默认，故主题文件只需写出想覆盖的字段。
// 文件缺失或损坏时返回默认主题与错误，由调用方提示用户。
func LoadTheme(dir, name string) (Theme, error) {
	if name == "" {
		name = defaultThemeName
	}
	if strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
		return DefaultTheme(), fmt.Errorf("非法主题名 %q", name)
	}
	if _, err := os.Stat(filepath.Join(dir, themesDir, name+".json")); err != nil {
		return DefaultTheme(), fmt.Errorf("主题 %s 不存在", name)
	}
	t := DefaultTheme()
	if err := config.LoadJSON(filepath.Join(dir, themesDir), name+".json", &t); err != nil {
		return DefaultTheme(), err
	}
	t.Colors.fillDefaults()
	return t, nil
}

// ListThemes 扫描 dir/themes 下的主题名（不含扩展名，按名称排序）。
func ListThemes(dir string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(dir, themesDir))
	if err != nil {
		return nil, fmt.Errorf("读取主题目录失败: %w", err)
	}
	names := []string{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		names = append(names, strings.TrimSuffix(e.Name(), ".json"))
	}
	sort.Strings(names)
	return names, nil
}

// styles 由 Theme 派生的组件样式集合，含预构建的符号串，
// 渲染时直接取用，不做重复计算。
type styles struct {
	Title    lipgloss.Style
	Item     lipgloss.Style
	Selected lipgloss.Style
	Muted    lipgloss.Style
	Error    lipgloss.Style
	Status   lipgloss.Style

	borderColor color.Color // 弹窗边框颜色
	glyphs      Glyphs
	cursorBlank string // 与光标符号等宽的空白，用于未选中行对齐
	popupRule   string // 弹窗分区标题两侧的装饰线（双写）
}

func newStyles(t Theme) styles {
	base := lipgloss.NewStyle().Background(t.BackgroundColor()).Foreground(lipgloss.Color(t.Colors.Foreground))
	return styles{
		Title:    base.Bold(true),
		Item:     base,
		Selected: base.Foreground(lipgloss.Color(t.Colors.Accent)).Bold(true),
		Muted:    base.Foreground(lipgloss.Color(t.Colors.Muted)),
		Error:    base.Foreground(lipgloss.Color(t.Colors.Error)),
		Status:   base.Foreground(lipgloss.Color(t.Colors.Accent)),

		borderColor: lipgloss.Color(t.Colors.Muted),
		glyphs:      t.Glyphs,
		cursorBlank: strings.Repeat(" ", ansi.StringWidth(t.Glyphs.Cursor)),
		popupRule:   t.Glyphs.PopupHeaderRule + t.Glyphs.PopupHeaderRule,
	}
}

// helpStyles 由 Theme 派生的帮助栏样式。
func helpStyles(t Theme) help.Styles {
	s := help.DefaultStyles(true)
	key := lipgloss.NewStyle().Foreground(lipgloss.Color(t.Colors.Accent))
	desc := lipgloss.NewStyle().Foreground(lipgloss.Color(t.Colors.Muted))
	s.ShortKey, s.FullKey = key, key
	s.ShortDesc, s.FullDesc = desc, desc
	s.ShortSeparator, s.FullSeparator, s.Ellipsis = desc, desc, desc
	return s
}
