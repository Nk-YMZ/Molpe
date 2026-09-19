package ui

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"molpe/internal/netease"
	"molpe/internal/queue"
)

// queuePopupMaxRows 弹窗可见曲目行数上限，超出时滚动显示。
const queuePopupMaxRows = 16

// queuePopupWidth 弹窗内容宽度（字符数）。
const queuePopupWidth = 56

type queueRowKind int

const (
	rowHeader   queueRowKind = iota // 区域标题行（不可选中）
	rowHistory                      // 历史记录（含“上一首”回退出的前进位置）
	rowCurrent                      // 当前播放曲目
	rowNextUp                       // 手动添加的“下一首播放”队列
	rowUpcoming                     // 歌单后续（仅顺序/列表循环模式展示）
)

// queueRow 弹窗中的一行：kind 标明来源队列，index 为该行在来源队列中的下标；
// rowHeader 行仅使用 title。
type queueRow struct {
	kind  queueRowKind
	index int
	title string
	song  netease.Song
}

// queuePopup 播放队列弹窗。布局自上而下为：历史记录（越早越靠上）、
// 当前曲目（打开时锚定在窗口中央）、“下一首播放”队列、歌单后续。
type queuePopup struct {
	open   bool
	rows   []queueRow
	cursor int // 光标所在行（rows 下标）
	offset int // 可见窗口在 rows 中的起始下标
	height int // 可见行数
}

// currentRow 返回当前曲目行在 rows 中的下标，不存在时为 -1。
func (p *queuePopup) currentRow() int {
	for i, r := range p.rows {
		if r.kind == rowCurrent {
			return i
		}
	}
	return -1
}

// moveCursor 上下移动光标，自动跳过区域标题行。
func (p *queuePopup) moveCursor(delta int) {
	c := p.cursor + delta
	for c >= 0 && c < len(p.rows) && p.rows[c].kind == rowHeader {
		c += delta
	}
	if c >= 0 && c < len(p.rows) {
		p.cursor = c
		p.ensureVisible()
	}
}

// normalizeCursor 删除重建后校正光标位置：落在标题行上时按 dir 方向
// （+1 向下、-1 向上）寻找最近的曲目行，该方向没有则反向寻找。
func (p *queuePopup) normalizeCursor(dir int) {
	if len(p.rows) == 0 {
		p.cursor = 0
		return
	}
	p.cursor = min(p.cursor, len(p.rows)-1)
	if p.rows[p.cursor].kind != rowHeader {
		return
	}
	for i := p.cursor + dir; i >= 0 && i < len(p.rows); i += dir {
		if p.rows[i].kind != rowHeader {
			p.cursor = i
			return
		}
	}
	for i := p.cursor - dir; i >= 0 && i < len(p.rows); i -= dir {
		if p.rows[i].kind != rowHeader {
			p.cursor = i
			return
		}
	}
}

// ensureVisible 调整滚动窗口使光标可见。
func (p *queuePopup) ensureVisible() {
	if p.cursor < p.offset {
		p.offset = p.cursor
	}
	if p.cursor >= p.offset+p.height {
		p.offset = p.cursor - p.height + 1
	}
	if maxOff := len(p.rows) - p.height; maxOff >= 0 && p.offset > maxOff {
		p.offset = maxOff
	}
}

// openQueuePopup 打开播放队列弹窗，光标定位到当前曲目并尽量居中显示。
func (m *Model) openQueuePopup() {
	p := &m.popup
	p.height = max(5, min(queuePopupMaxRows, m.height-6))
	m.buildQueueRows()
	p.cursor = max(0, p.currentRow())
	maxOff := max(0, len(p.rows)-p.height)
	p.offset = min(max(0, p.cursor-p.height/2), maxOff)
	p.open = true
}

// buildQueueRows 从队列快照重建弹窗行，按区域分节：历史记录 / 正在播放 /
// 下一首播放 / 歌单后续。空区域不显示标题。
func (m *Model) buildQueueRows() {
	p := &m.popup
	p.rows = p.rows[:0]
	header := func(title string) {
		p.rows = append(p.rows, queueRow{kind: rowHeader, title: title})
	}
	hist := m.queue.History()
	hpos := m.queue.HistoryPos()
	// 历史记录：按时间正序，越早越靠上。
	if hpos > 0 {
		header("历史记录")
		for i := 0; i < hpos && i < len(hist); i++ {
			p.rows = append(p.rows, queueRow{kind: rowHistory, index: i, song: hist[i]})
		}
	}
	// 正在播放。
	if cur, ok := m.queue.Current(); ok {
		header("正在播放")
		p.rows = append(p.rows, queueRow{kind: rowCurrent, song: cur})
	}
	// 下一首播放：“上一首”回退出的前进位置播放优先级最高，排在该区域最前；
	// 其后为手动添加的下一首队列，播放顺序越远越靠下。均为空则不显示该区域。
	future := hist[min(hpos+1, len(hist)):]
	nextUp := m.queue.NextUp()
	if len(future)+len(nextUp) > 0 {
		header("下一首播放")
		for i, s := range future {
			p.rows = append(p.rows, queueRow{kind: rowHistory, index: hpos + 1 + i, song: s})
		}
		for i, s := range nextUp {
			p.rows = append(p.rows, queueRow{kind: rowNextUp, index: i, song: s})
		}
	}
	// 歌单后续：仅顺序/列表循环模式展示，从当前曲的下一首到歌单末尾，不考虑循环。
	if m.queue.Mode() != queue.ModeRandom {
		if base, up := m.queue.Upcoming(); len(up) > 0 {
			header("歌单后续")
			for i, s := range up {
				p.rows = append(p.rows, queueRow{kind: rowUpcoming, index: base + i, song: s})
			}
		}
	}
}

// refreshQueuePopup 在曲目切换后刷新弹窗内容，光标回到当前曲目行。
func (m *Model) refreshQueuePopup() {
	p := &m.popup
	if !p.open {
		return
	}
	if _, ok := m.queue.Current(); !ok {
		p.open = false
		return
	}
	m.buildQueueRows()
	p.cursor = max(0, p.currentRow())
	p.ensureVisible()
}

// updatePopup 处理弹窗打开时的按键。
func (m Model) updatePopup(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	p := &m.popup
	switch {
	case key.Matches(msg, m.keys.Quit):
		m.shutdown()
		return m, tea.Quit
	case key.Matches(msg, m.keys.Queue), key.Matches(msg, m.keys.Back):
		p.open = false
	case key.Matches(msg, m.keys.Up):
		p.moveCursor(-1)
	case key.Matches(msg, m.keys.Down):
		p.moveCursor(1)
	case key.Matches(msg, m.keys.Delete):
		return m.deletePopupRow()
	}
	return m, nil
}

// deletePopupRow 删除光标选中的曲目：历史/下一首队列/歌单后续仅从对应
// 队列移除；删除当前曲目则自动跳转下一首。
func (m Model) deletePopupRow() (tea.Model, tea.Cmd) {
	p := &m.popup
	if len(p.rows) == 0 {
		return m, nil
	}
	row := p.rows[p.cursor]
	curRow := p.currentRow()
	d := p.cursor

	if row.kind == rowCurrent {
		m.queue.RemoveCurrent()
		song, ok := m.queue.Next()
		m.saveQueue()
		if !ok {
			// 没有可播放的下一首：停止播放并关闭弹窗。
			if pl, err := m.player.get(); err == nil {
				pl.Close()
			}
			m.playing = nil
			m.paused = false
			m.publishState()
			p.open = false
			return m, m.setNote("队列已播完")
		}
		m.buildQueueRows()
		// 光标留在原位置，新的当前曲补位到此处。
		p.cursor = d
		p.normalizeCursor(1)
		p.ensureVisible()
		m.paused = false // 删除当前曲即切歌，暂停状态随之解除
		return m, m.playCmd(song)
	}

	switch row.kind {
	case rowHistory:
		m.queue.RemoveHistory(row.index)
	case rowNextUp:
		m.queue.RemoveNextUp(row.index)
	case rowUpcoming:
		m.queue.RemovePlaylist(row.index)
	default: // 标题行，不可删除
		return m, nil
	}
	m.saveQueue()
	m.buildQueueRows()
	if d < curRow {
		// 删除的是当前曲上方的条目：上方条目下移补位，
		// 光标落在补位过来的更早条目上；已是最上方条目时保持原位。
		p.cursor = max(0, d-1)
		if p.offset > 0 {
			p.offset--
		}
		p.normalizeCursor(-1)
	} else {
		// 删除的是当前曲下方的条目：光标位置不动，下方条目上移补位；
		// 下方没有别的曲目时才上移光标。
		p.cursor = d
		p.normalizeCursor(1)
	}
	p.ensureVisible()
	return m, nil
}

// renderQueuePopup 渲染弹窗并居中放置在整个屏幕上。
func (m Model) renderQueuePopup() string {
	p := m.popup
	var b strings.Builder
	b.WriteString(m.sty.Title.Render("播放队列") + "\n")
	if len(p.rows) == 0 {
		b.WriteString(m.sty.Muted.Render("（空）") + "\n")
	}
	end := min(p.offset+p.height, len(p.rows))
	for i := p.offset; i < end; i++ {
		row := p.rows[i]
		if row.kind == rowHeader {
			// 区域标题之间空一行（弹窗顶部的第一个标题除外）。
			if i > p.offset {
				b.WriteString("\n")
			}
			b.WriteString(m.sty.Muted.Render("── "+row.title+" ──") + "\n")
			continue
		}
		text := ansi.Truncate(row.song.Name+" - "+row.song.Artists, queuePopupWidth-2, "")
		marker := "  "
		style := m.sty.Item
		switch row.kind {
		case rowCurrent:
			marker = "▶ "
		case rowHistory, rowUpcoming:
			style = m.sty.Muted
		}
		if i == p.cursor {
			style = m.sty.Selected
		}
		b.WriteString(style.Render(marker+text) + "\n")
	}
	b.WriteString(m.sty.Muted.Render("↑/↓ 移动 · del 删除 · l/b/esc 关闭"))
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(m.theme.Muted).
		Padding(0, 1).
		Render(b.String())
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}
