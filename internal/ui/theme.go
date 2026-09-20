package ui

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"image/color"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// themesDir 是配置目录下存放主题文件的子目录。
const themesDir = "themes"

// defaultThemeName 是默认主题名（对应 default.json）。
const defaultThemeName = "default"

const emberThemeName = "ember"

// 内置主题通过 go:embed 编译进二进制，发布时无需在配置目录携带主题文件。
// $XDG_CONFIG_HOME/molpe/themes/ 仅用于追加用户自定义主题。
//
//go:embed themes/default.json
var builtinDefaultThemeJSON []byte

//go:embed themes/ember.json
var builtinEmberThemeJSON []byte

var builtinThemeData = map[string][]byte{
	defaultThemeName: builtinDefaultThemeJSON,
	emberThemeName:   builtinEmberThemeJSON,
}

// Colors 主题配色；值为 ANSI 色号（如 "15"）或十六进制颜色（如 "#ffffff"）。
//
// 界面层次按明暗分级：Faint（装饰线、空进度、远端歌词等最弱层）<
// Muted（次要文本）< Foreground（正文）；Accent 与 Accent2 是两个强调色，
// 前者承载选中/播放/进度等主高亮，后者小剂量点缀（品牌标记、进度热端、
// 弹窗标题、按键名），为界面提供温度。
// Background 为整屏背景色（仅支持 "#rrggbb"），缺省或非法时为纯黑；
// 随附主题统一使用纯黑（#000000）。
type Colors struct {
	Background string `json:"background,omitempty"`
	Foreground string `json:"foreground"`
	Muted      string `json:"muted"`
	Faint      string `json:"faint"`
	Accent     string `json:"accent"`
	Accent2    string `json:"accent2"`
	Error      string `json:"error"`
}

// fillDefaults 将空字符串配色回退为默认值；配色为空会破坏显示，
// 而符号允许用户显式置空，故 Glyphs 不做同样处理。
// Background 例外：允许留空，由 BackgroundColor 回退纯黑。
func (c *Colors) fillDefaults() {
	d := DefaultTheme().Colors
	if c.Foreground == "" {
		c.Foreground = d.Foreground
	}
	if c.Muted == "" {
		c.Muted = d.Muted
	}
	if c.Faint == "" {
		c.Faint = d.Faint
	}
	if c.Accent == "" {
		c.Accent = d.Accent
	}
	if c.Accent2 == "" {
		c.Accent2 = d.Accent2
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
	ProgressHead    string `json:"progress_head"`     // 进度条头部（可为空，空时用填充符加副强调色）
	ProgressEmpty   string `json:"progress_empty"`    // 进度条未播放部分
	Rule            string `json:"rule"`              // 整宽分隔线（头部下方）
	HeaderMark      string `json:"header_mark"`       // 头部品牌标记
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

// fallbackTheme 是内置 default.json 意外损坏时的编译期兜底。
// 默认配色选用终端基础色（16 色内），Faint 使用 256 色深灰，
// 在未支持 256 色的终端上会由终端自行降级，不破坏可读性。
func fallbackTheme() Theme {
	return Theme{
		Colors: Colors{
			Background: "#000000",
			Foreground: "15",
			Muted:      "8",
			Faint:      "238",
			Accent:     "10",
			Accent2:    "11",
			Error:      "9",
		},
		Glyphs: Glyphs{
			Cursor:          "▌ ",
			Playing:         "▶ ",
			Paused:          "⏸ ",
			PlaylistCreated: "✎",
			PlaylistStarred: "♥",
			PopupHeaderRule: "─",
			ProgressFill:    "█",
			ProgressHead:    "",
			ProgressEmpty:   "░",
			Rule:            "─",
			HeaderMark:      "▞",
		},
		Border: borderRounded,
	}
}

// DefaultTheme 返回编译进程序的默认主题。
func DefaultTheme() Theme {
	t := fallbackTheme()
	if err := json.Unmarshal(builtinDefaultThemeJSON, &t); err != nil {
		return fallbackTheme()
	}
	return t
}

// BackgroundColor 返回主题的背景色；仅解析 "#rrggbb"，缺省或非法时为纯黑。
func (t Theme) BackgroundColor() color.Color {
	s := t.Colors.Background
	if len(s) == 7 && s[0] == '#' {
		if v, err := strconv.ParseUint(s[1:], 16, 32); err == nil {
			return color.RGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 0xff}
		}
	}
	return color.Black
}

// EnsureThemes 确保外部扩展主题目录存在；内置主题不会写入该目录。
func EnsureThemes(dir string) error {
	if err := os.MkdirAll(filepath.Join(dir, themesDir), 0o755); err != nil {
		return fmt.Errorf("创建扩展主题目录失败: %w", err)
	}
	return nil
}

// LoadTheme 优先按名称加载内置主题；其他名称从 dir/themes/ 扩展目录加载。
// 空名称使用内置默认主题。外部主题与默认值逐字段合并：未指定的字段
// 保留默认，故主题文件只需写出想覆盖的字段。同名外部文件不覆盖内置主题。
// 文件缺失或损坏时返回默认主题与错误，由调用方提示用户。
func LoadTheme(dir, name string) (Theme, error) {
	if name == "" {
		name = defaultThemeName
	}
	if strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
		return DefaultTheme(), fmt.Errorf("非法主题名 %q", name)
	}
	if data, ok := builtinThemeData[name]; ok {
		t, err := decodeTheme(data)
		if err != nil {
			return DefaultTheme(), fmt.Errorf("解析内置主题 %s 失败: %w", name, err)
		}
		return t, nil
	}
	data, err := os.ReadFile(filepath.Join(dir, themesDir, name+".json"))
	if errors.Is(err, fs.ErrNotExist) {
		return DefaultTheme(), fmt.Errorf("主题 %s 不存在", name)
	}
	if err != nil {
		return DefaultTheme(), fmt.Errorf("读取主题 %s 失败: %w", name, err)
	}
	t, err := decodeTheme(data)
	if err != nil {
		return DefaultTheme(), fmt.Errorf("解析主题 %s 失败: %w", name, err)
	}
	return t, nil
}

func decodeTheme(data []byte) (Theme, error) {
	t := DefaultTheme()
	if err := json.Unmarshal(data, &t); err != nil {
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

// ListThemes 合并内置主题与 dir/themes 下的扩展主题名，去重后排序。
func ListThemes(dir string) ([]string, error) {
	seen := make(map[string]struct{}, len(builtinThemeData))
	for name := range builtinThemeData {
		seen[name] = struct{}{}
	}
	entries, err := os.ReadDir(filepath.Join(dir, themesDir))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("读取主题目录失败: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		seen[strings.TrimSuffix(e.Name(), ".json")] = struct{}{}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

// styles 由 Theme 派生的组件样式集合，含预构建的符号串，
// 渲染时直接取用，不做重复计算。
type styles struct {
	Title    lipgloss.Style // 正文粗体（标题、当前曲目名）
	Item     lipgloss.Style // 正文
	Selected lipgloss.Style // 选中项/当前歌词（主强调色粗体）
	Muted    lipgloss.Style // 次要文本
	Faint    lipgloss.Style // 最弱层（装饰线、远端歌词、空进度）
	Accent   lipgloss.Style // 主强调色（播放符号、进度填充）
	Accent2  lipgloss.Style // 副强调色（品牌标记、进度热端、弹窗标题、按键名）
	Error    lipgloss.Style

	borderColor color.Color     // 弹窗边框颜色
	border      lipgloss.Border // 弹窗边框样式
	noBorder    bool            // 弹窗不使用边框
	glyphs      Glyphs
	cursorBlank string // 与光标符号等宽的空白，用于未选中行对齐
	popupRule   string // 弹窗分区标题两侧的装饰线（双写）

	// lyricRamp 歌词渐变样式表：由主题的前景色平滑过渡到微光色，
	// 按与当前句的距离取档，距离越远越虚；主题色无法解析时为 nil，
	// 渲染层回退为前景/微光两档。
	lyricRamp []lipgloss.Style
}

func newStyles(t Theme) styles {
	base := lipgloss.NewStyle().Foreground(lipgloss.Color(t.Colors.Foreground))
	return styles{
		Title:    base.Bold(true),
		Item:     base,
		Selected: base.Foreground(lipgloss.Color(t.Colors.Accent)).Bold(true),
		Muted:    base.Foreground(lipgloss.Color(t.Colors.Muted)),
		Faint:    base.Foreground(lipgloss.Color(t.Colors.Faint)),
		Accent:   base.Foreground(lipgloss.Color(t.Colors.Accent)),
		Accent2:  base.Foreground(lipgloss.Color(t.Colors.Accent2)),
		Error:    base.Foreground(lipgloss.Color(t.Colors.Error)),

		borderColor: lipgloss.Color(t.Colors.Faint),
		border:      popupBorder(t.Border),
		noBorder:    t.Border == borderNone,
		glyphs:      t.Glyphs,
		cursorBlank: strings.Repeat(" ", ansi.StringWidth(t.Glyphs.Cursor)),
		popupRule:   t.Glyphs.PopupHeaderRule + t.Glyphs.PopupHeaderRule,

		lyricRamp: buildLyricRamp(t.Colors.Foreground, t.Colors.Faint),
	}
}

// lyricRampSteps 歌词渐变的预计算档数；渲染按距离取档，不逐帧计算颜色。
const lyricRampSteps = 16

// buildLyricRamp 预计算从前景色到微光色的渐变样式表（首档为前景色，
// 末档为微光色）；任一端点无法解析时返回 nil，由渲染层回退处理。
func buildLyricRamp(foreground, faint string) []lipgloss.Style {
	fr, fg, fb, ok1 := parseThemeColor(foreground)
	tr, tg, tb, ok2 := parseThemeColor(faint)
	if !ok1 || !ok2 {
		return nil
	}
	ramp := make([]lipgloss.Style, lyricRampSteps)
	last := lyricRampSteps - 1
	for i := range ramp {
		// 整数插值：首末档精确落在两个主题色上。
		r := fr + (tr-fr)*i/last
		g := fg + (tg-fg)*i/last
		b := fb + (tb-fb)*i/last
		ramp[i] = lipgloss.NewStyle().Foreground(lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", r, g, b)))
	}
	return ramp
}

// parseThemeColor 将主题配色解析为 8 位 RGB：支持 "#rrggbb" 与
// ANSI 色号（0-255，按 xterm 标准调色板换算，与终端渲染结果一致）。
func parseThemeColor(s string) (r, g, b int, ok bool) {
	if len(s) == 7 && s[0] == '#' {
		v, err := strconv.ParseUint(s[1:], 16, 32)
		if err != nil {
			return 0, 0, 0, false
		}
		return int(v >> 16 & 0xff), int(v >> 8 & 0xff), int(v & 0xff), true
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 || n > 255 {
		return 0, 0, 0, false
	}
	return ansiPaletteRGB(n)
}

// ansiPaletteRGB 返回 xterm 256 色调色板中色号 n 的 RGB 值。
func ansiPaletteRGB(n int) (r, g, b int, ok bool) {
	// 0-15：标准/高亮基础色。
	basic := [16][3]int{
		{0x00, 0x00, 0x00}, {0x80, 0x00, 0x00}, {0x00, 0x80, 0x00}, {0x80, 0x80, 0x00},
		{0x00, 0x00, 0x80}, {0x80, 0x00, 0x80}, {0x00, 0x80, 0x80}, {0xc0, 0xc0, 0xc0},
		{0x80, 0x80, 0x80}, {0xff, 0x00, 0x00}, {0x00, 0xff, 0x00}, {0xff, 0xff, 0x00},
		{0x00, 0x00, 0xff}, {0xff, 0x00, 0xff}, {0x00, 0xff, 0xff}, {0xff, 0xff, 0xff},
	}
	switch {
	case n < 16:
		c := basic[n]
		return c[0], c[1], c[2], true
	case n < 232:
		// 16-231：6×6×6 颜色立方体，通道取值为 0,95,135,175,215,255。
		n -= 16
		level := func(i int) int {
			if i == 0 {
				return 0
			}
			return 55 + 40*i
		}
		return level(n / 36), level(n / 6 % 6), level(n % 6), true
	default:
		// 232-255：灰度阶梯。
		v := 8 + 10*(n-232)
		return v, v, v, true
	}
}

// popupBorder 将边框枚举映射为 lipgloss 边框。
func popupBorder(name string) lipgloss.Border {
	if name == borderSquare {
		return lipgloss.NormalBorder()
	}
	return lipgloss.RoundedBorder()
}
