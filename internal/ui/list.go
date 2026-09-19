package ui

import (
	"strings"
)

// listModel 是纯键盘操作的简单滚动列表。
type listModel struct {
	items  []string
	cursor int
	offset int
	height int // 可见行数，<=0 表示不限制
}

func (l *listModel) SetItems(items []string) {
	l.items = items
	l.cursor = 0
	l.offset = 0
}

func (l *listModel) SetHeight(h int) {
	l.height = h
	l.clamp()
}

// Selected 返回当前选中项下标；列表为空时返回 -1。
func (l listModel) Selected() int {
	if len(l.items) == 0 {
		return -1
	}
	return l.cursor
}

func (l listModel) Len() int { return len(l.items) }

// Move 将光标移动 delta 行（可为负），并保证光标在可见范围内。
func (l *listModel) Move(delta int) {
	l.cursor += delta
	l.clamp()
}

func (l *listModel) GoTop() {
	l.cursor = 0
	l.clamp()
}

func (l *listModel) GoBottom() {
	l.cursor = len(l.items) - 1
	l.clamp()
}

func (l *listModel) clamp() {
	if n := len(l.items); n == 0 {
		l.cursor, l.offset = 0, 0
		return
	} else {
		if l.cursor < 0 {
			l.cursor = 0
		}
		if l.cursor > n-1 {
			l.cursor = n - 1
		}
	}
	if l.height <= 0 {
		l.offset = 0
		return
	}
	if l.cursor < l.offset {
		l.offset = l.cursor
	}
	if l.cursor >= l.offset+l.height {
		l.offset = l.cursor - l.height + 1
	}
}

func (l listModel) View(sty styles) string {
	if len(l.items) == 0 {
		return sty.Muted.Render("（空）")
	}
	end := len(l.items)
	if l.height > 0 && l.offset+l.height < end {
		end = l.offset + l.height
	}

	var b strings.Builder
	for i := l.offset; i < end; i++ {
		line, style := "  "+l.items[i], sty.Item
		if i == l.cursor {
			line, style = "▸ "+l.items[i], sty.Selected
		}
		if i > l.offset {
			b.WriteByte('\n')
		}
		b.WriteString(style.Render(line))
	}
	return b.String()
}
