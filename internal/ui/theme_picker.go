package ui

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// themePickerMaxRows 主题选择弹窗可见行数上限。
const themePickerMaxRows = 12

// themePicker 主题选择弹窗。打开时重新扫描主题目录，
// 因此修改主题文件后重新打开或再次选中即可生效。
type themePicker struct {
	open  bool
	list  listModel
	names []string // 主题名（与 list.items 一一对应）
}

// themePickerHeight 计算弹窗可见行数。
func themePickerHeight(screenHeight int) int {
	return max(3, min(themePickerMaxRows, screenHeight-6))
}

// openThemePicker 扫描主题目录并打开选择弹窗，光标定位到当前主题。
func (m *Model) openThemePicker() {
	names, err := ListThemes(m.dirs.Config)
	if err != nil {
		m.errNote = err.Error()
		return
	}
	m.picker.names = names
	m.picker.list.SetItems(names)
	m.picker.list.SetHeight(themePickerHeight(m.height))
	for i, n := range names {
		if n == m.themeName {
			m.picker.list.SetCursor(i)
			break
		}
	}
	// 两个弹窗互斥。
	m.popup.open = false
	m.picker.open = true
}

// updatePicker 处理主题选择弹窗打开时的按键。
func (m Model) updatePicker(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Quit):
		m.shutdown()
		return m, tea.Quit
	case key.Matches(msg, m.keys.Theme), key.Matches(msg, m.keys.Back):
		m.picker.open = false
	case key.Matches(msg, m.keys.Up):
		m.picker.list.Move(-1)
	case key.Matches(msg, m.keys.Down):
		m.picker.list.Move(1)
	case key.Matches(msg, m.keys.Top):
		m.picker.list.GoTop()
	case key.Matches(msg, m.keys.Bottom):
		m.picker.list.GoBottom()
	case key.Matches(msg, m.keys.Enter):
		i := m.picker.list.Selected()
		m.picker.open = false
		if i < 0 || i >= len(m.picker.names) {
			return m, nil
		}
		return m, m.applyTheme(m.picker.names[i])
	}
	return m, nil
}

// applyTheme 加载并应用主题：重建样式、同步帮助栏配色，
// 并以内嵌新符号重建歌单列表项；主题名随配置在退出时统一落盘。
func (m *Model) applyTheme(name string) tea.Cmd {
	t, err := LoadTheme(m.dirs.Config, name)
	if err != nil {
		return m.setNote("主题加载失败：" + err.Error())
	}
	m.theme = t
	m.themeName = name
	m.sty = newStyles(t)
	m.help.Styles = helpStyles(t)
	m.cfg.Theme = name
	if m.playlists.data != nil {
		m.playlists.list.SetItems(playlistItems(m.playlists.data, t.Glyphs))
	}
	return m.setNote("已切换主题：" + name)
}
