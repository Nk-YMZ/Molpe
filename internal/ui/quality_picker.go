package ui

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"molpe/internal/ipc"
	"molpe/internal/netease"
)

type qualityPicker struct {
	open   bool
	list   listModel
	levels []string
}

func qualityPickerHeight(height int) int {
	return max(1, min(len(netease.QualityLevels()), height-10))
}

func (m *Model) openQualityPicker() {
	m.quality.levels = netease.QualityLevels()
	m.quality.list.SetItems(m.quality.levels)
	m.quality.list.SetHeight(qualityPickerHeight(m.height))
	for i, level := range m.quality.levels {
		if level == m.st.Quality {
			m.quality.list.SetCursor(i)
			break
		}
	}
	m.popup.open, m.picker.open = false, false
	m.quality.open = true
}

func (m Model) updateQualityPicker(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if nm, cmd, ok := m.quitKey(msg); ok {
		return nm, cmd
	}
	switch {
	case key.Matches(msg, m.keys.Quality), key.Matches(msg, m.keys.Back):
		m.quality.open = false
	case key.Matches(msg, m.keys.Up):
		m.quality.list.Move(-1)
	case key.Matches(msg, m.keys.Down):
		m.quality.list.Move(1)
	case key.Matches(msg, m.keys.Top):
		m.quality.list.GoTop()
	case key.Matches(msg, m.keys.Bottom):
		m.quality.list.GoBottom()
	case key.Matches(msg, m.keys.Enter):
		i := m.quality.list.Selected()
		if i < 0 || i >= len(m.quality.levels) {
			return m, nil
		}
		if err := m.srv.Send(ipc.TSetQuality, ipc.QualityCmd{Quality: m.quality.levels[i]}); err != nil {
			return m, m.setNote("切换音质失败："+err.Error(), true)
		}
		m.quality.open = false
	}
	return m, nil
}
