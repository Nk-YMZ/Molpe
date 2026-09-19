package ui

import "testing"

func newTestList(n, height int) listModel {
	items := make([]string, n)
	for i := range items {
		items[i] = "item"
	}
	l := listModel{height: height}
	l.SetItems(items)
	return l
}

func TestListMoveClamp(t *testing.T) {
	l := newTestList(5, 0)

	l.Move(-1)
	if l.Selected() != 0 {
		t.Errorf("向上越界: cursor=%d", l.Selected())
	}
	l.Move(3)
	if l.Selected() != 3 {
		t.Errorf("向下移动: cursor=%d, 期望 3", l.Selected())
	}
	l.Move(10)
	if l.Selected() != 4 {
		t.Errorf("向下越界: cursor=%d, 期望 4", l.Selected())
	}
}

func TestListScrollKeepsCursorVisible(t *testing.T) {
	l := newTestList(10, 3)

	l.GoBottom()
	if l.Selected() != 9 || l.offset != 7 {
		t.Errorf("GoBottom: cursor=%d offset=%d, 期望 9/7", l.Selected(), l.offset)
	}
	l.GoTop()
	if l.Selected() != 0 || l.offset != 0 {
		t.Errorf("GoTop: cursor=%d offset=%d, 期望 0/0", l.Selected(), l.offset)
	}
	l.Move(5)
	if l.offset != 3 {
		t.Errorf("移动后 offset=%d, 期望 3", l.offset)
	}
	l.Move(-2)
	if l.offset != 3 {
		t.Errorf("光标仍可见时 offset 不应变化: offset=%d", l.offset)
	}
}

func TestListEmpty(t *testing.T) {
	l := newTestList(0, 5)
	if l.Selected() != -1 {
		t.Errorf("空列表 Selected()=%d, 期望 -1", l.Selected())
	}
	l.Move(1) // 不应 panic
	l.GoBottom()
	if l.Selected() != -1 {
		t.Errorf("空列表操作后 Selected()=%d, 期望 -1", l.Selected())
	}
}
