package ui

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"molpe/internal/ipc"
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

// buildQueueRows 从服务端状态快照重建弹窗行，按区域分节：历史记录 /
// 正在播放 / 下一首播放 / 歌单后续。空区域不显示标题。
func (m *Model) buildQueueRows() {
	p := &m.popup
	p.rows = p.rows[:0]
	header := func(title string) {
		p.rows = append(p.rows, queueRow{kind: rowHeader, title: title})
	}
	q := m.st.Queue
	hist := q.History
	hpos := q.Hpos
	// 历史记录：按时间正序，越早越靠上。
	if hpos > 0 {
		header("历史记录")
		for i := 0; i < hpos && i < len(hist); i++ {
			p.rows = append(p.rows, queueRow{kind: rowHistory, index: i, song: hist[i]})
		}
	}
	// 正在播放。
	if m.st.Playing != nil {
		header("正在播放")
		p.rows = append(p.rows, queueRow{kind: rowCurrent, song: m.st.Playing.Song})
	}
	// 下一首播放：“上一首”回退出的前进位置播放优先级最高，排在该区域最前；
	// 其后为手动添加的下一首队列，播放顺序越远越靠下。均为空则不显示该区域。
	future := hist[min(hpos+1, len(hist)):]
	nextUp := q.NextUp
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
	if q.Mode != queue.ModeRandom && m.st.Playing != nil {
		if base, up := upcomingSongs(m.st); len(up) > 0 {
			header("歌单后续")
			for i, s := range up {
				p.rows = append(p.rows, queueRow{kind: rowUpcoming, index: base + i, song: s})
			}
		}
	}
}

// upcomingSongs 返回歌单中当前曲之后的歌曲及其在歌单中的起始下标；
// 当前曲不在歌单中或已是歌单末尾时 songs 为空。
func upcomingSongs(st ipc.State) (base int, songs []netease.Song) {
	if st.Playing == nil {
		return 0, nil
	}
	for i, s := range st.Queue.Songs {
		if s.ID == st.Playing.Song.ID {
			if i+1 >= len(st.Queue.Songs) {
				return 0, nil
			}
			return i + 1, st.Queue.Songs[i+1:]
		}
	}
	return 0, nil
}

// updatePopup 处理弹窗打开时的按键。
func (m Model) updatePopup(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	p := &m.popup
	if nm, cmd, ok := m.quitKey(msg); ok {
		return nm, cmd
	}
	switch {
	case key.Matches(msg, m.keys.Queue), key.Matches(msg, m.keys.Back):
		p.open = false
	case key.Matches(msg, m.keys.Up):
		p.moveCursor(-1)
	case key.Matches(msg, m.keys.Down):
		p.moveCursor(1)
	case key.Matches(msg, m.keys.Delete):
		m.deletePopupRow()
	}
	return m, nil
}

// deletePopupRow 删除光标选中的曲目（通知后端执行）：历史/下一首队列/
// 歌单后续仅从对应队列移除；删除当前曲目则自动跳转下一首。
// 删除结果由后端推送的状态快照驱动刷新，光标由 applyState 就近校正。
func (m *Model) deletePopupRow() {
	p := &m.popup
	if len(p.rows) == 0 {
		return
	}
	row := p.rows[p.cursor]
	var section string
	switch row.kind {
	case rowHistory:
		section = ipc.SectionHistory
	case rowNextUp:
		section = ipc.SectionNextUp
	case rowUpcoming:
		section = ipc.SectionPlaylist
	case rowCurrent:
		section = ipc.SectionCurrent
	default: // 标题行，不可删除
		return
	}
	_ = m.srv.Send(ipc.TRemove, ipc.RemoveCmd{Section: section, Index: row.index})
}
