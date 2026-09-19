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
// 背景色不属于主题：所有主题统一纯黑背景（#000000）。
type Colors struct {
	Foreground string `json:"foreground"`
	Muted      string `json:"muted"`
	Accent     string `json:"accent"`
	Error      string `json:"error"`
}

// fillDefaults 将空字符串配色回退为默认值；配色为空会破坏显示，
// 而符号允许用户显式置空，故 Glyphs 不做同样处理。
func (c *Colors) fillDefaults() {
	d := DefaultTheme().Colors
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
	ProgressFill    string `json:"progress_fill"`     // 进度条已播放部分
	ProgressHead    string `json:"progress_head"`     // 进度条头部（可为空）
	ProgressEmpty   string `json:"progress_empty"`    // 进度条未播放部分
}

// 边框样式的合法取值。
const (
	borderRounded = "rounded"
	borderSquare  = "square"
	borderNone    = "none"
)

// Theme 主题：配色 + 符号 + 边框样式（枚举）。布局与尺寸由代码固定，
// 不属于主题；歌词行数等数值是全局设定，统一在 config.json 中配置。
type Theme struct {
	Colors Colors `json:"colors"`
	Glyphs Glyphs `json:"glyphs"`
	// Border 弹窗边框样式：rounded（默认）/ square / none。
	Border string `json:"border,omitempty"`
}

// DefaultTheme 返回默认主题（所有主题共享纯黑背景）。
func DefaultTheme() Theme {
	return Theme{
		Colors: Colors{
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
			ProgressFill:    "━",
			ProgressHead:    "●",
			ProgressEmpty:   "─",
		},
		Border: borderRounded,
	}
}

// BackgroundColor 返回终端背景色：固定纯黑，不随主题变化。
func (t Theme) BackgroundColor() color.Color { return color.Black }

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
	// 边框为枚举值，非法值回退默认。
	switch t.Border {
	case borderRounded, borderSquare, borderNone:
	default:
		t.Border = borderRounded
	}
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

	borderColor color.Color     // 弹窗边框颜色
	border      lipgloss.Border // 弹窗边框样式
	noBorder    bool            // 弹窗不使用边框
	glyphs      Glyphs
	cursorBlank string // 与光标符号等宽的空白，用于未选中行对齐
	popupRule   string // 弹窗分区标题两侧的装饰线（双写）
}

func newStyles(t Theme) styles {
	base := lipgloss.NewStyle().Foreground(lipgloss.Color(t.Colors.Foreground))
	return styles{
		Title:    base.Bold(true),
		Item:     base,
		Selected: base.Foreground(lipgloss.Color(t.Colors.Accent)).Bold(true),
		Muted:    base.Foreground(lipgloss.Color(t.Colors.Muted)),
		Error:    base.Foreground(lipgloss.Color(t.Colors.Error)),
		Status:   base.Foreground(lipgloss.Color(t.Colors.Accent)),

		borderColor: lipgloss.Color(t.Colors.Muted),
		border:      popupBorder(t.Border),
		noBorder:    t.Border == borderNone,
		glyphs:      t.Glyphs,
		cursorBlank: strings.Repeat(" ", ansi.StringWidth(t.Glyphs.Cursor)),
		popupRule:   t.Glyphs.PopupHeaderRule + t.Glyphs.PopupHeaderRule,
	}
}

// popupBorder 将边框枚举映射为 lipgloss 边框。
func popupBorder(name string) lipgloss.Border {
	if name == borderSquare {
		return lipgloss.NormalBorder()
	}
	return lipgloss.RoundedBorder()
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
