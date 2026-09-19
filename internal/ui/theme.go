package ui

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// Theme 定义界面配色。当前仅有默认的纯黑主题，
// 结构预留了后续扩展多套主题的可能。
type Theme struct {
	Background color.Color
	Foreground color.Color
	Muted      color.Color
	Accent     color.Color
	Error      color.Color
}

// DefaultTheme 返回纯黑背景的默认主题。
func DefaultTheme() Theme {
	return Theme{
		Background: color.Black,
		Foreground: lipgloss.Color("15"),
		Muted:      lipgloss.Color("8"),
		Accent:     lipgloss.Color("10"),
		Error:      lipgloss.Color("9"),
	}
}

// styles 由 Theme 派生的组件样式集合。
type styles struct {
	Title    lipgloss.Style
	Item     lipgloss.Style
	Selected lipgloss.Style
	Muted    lipgloss.Style
	Error    lipgloss.Style
	Status   lipgloss.Style
}

func newStyles(t Theme) styles {
	base := lipgloss.NewStyle().Background(t.Background).Foreground(t.Foreground)
	return styles{
		Title:    base.Bold(true),
		Item:     base,
		Selected: base.Foreground(t.Accent).Bold(true),
		Muted:    base.Foreground(t.Muted),
		Error:    base.Foreground(t.Error),
		Status:   base.Foreground(t.Accent),
	}
}
