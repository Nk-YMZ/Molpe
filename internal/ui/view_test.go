package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// strip 剥离 ANSI 转义序列，便于断言纯文本。
func strip(s string) string { return ansi.Strip(s) }

func testStyles() styles {
	// 渲染测试禁用 ANSI 转义，便于断言纯文本。
	return newStyles(DefaultTheme())
}

func TestRenderListCursorGlyph(t *testing.T) {
	sty := testStyles()
	l := listView{items: []string{"甲", "乙", "丙"}, cursor: 1}
	out := strip(renderList(l, sty))
	lines := strings.Split(out, "\n")
	if len(lines) != 3 {
		t.Fatalf("行数不符: %d", len(lines))
	}
	if !strings.Contains(lines[1], "▸ 乙") {
		t.Errorf("选中行缺少光标符号: %q", lines[1])
	}
	if !strings.Contains(lines[0], "  甲") {
		t.Errorf("未选中行应为等宽空白: %q", lines[0])
	}
}

func TestRenderStatusPausedIcon(t *testing.T) {
	sty := testStyles()
	playing := strip(renderStatus(statusView{playing: true, track: "歌 - 手", quality: "无损", mode: "循环", volume: 80}, sty))
	if !strings.Contains(playing, "▶ 歌 - 手") {
		t.Errorf("播放中应使用播放符号: %q", playing)
	}
	paused := strip(renderStatus(statusView{playing: true, paused: true, track: "歌 - 手", quality: "无损", mode: "循环", volume: 80}, sty))
	if !strings.Contains(paused, "⏸ 歌 - 手") {
		t.Errorf("暂停应使用暂停符号: %q", paused)
	}
	idle := strip(renderStatus(statusView{}, sty))
	if !strings.Contains(idle, "未在播放") {
		t.Errorf("空闲状态不符: %q", idle)
	}
	if got := strip(renderStatus(statusView{err: "出错了"}, sty)); !strings.Contains(got, "出错了") {
		t.Errorf("错误应整行替换: %q", got)
	}
}

func TestRenderLyricsWindow(t *testing.T) {
	sty := testStyles()
	lines := []string{"一", "二", "三", "四", "五", "六", "七"}
	out := strip(renderLyrics(lines, 3, 5, 0, sty))
	got := strings.Split(out, "\n")
	if len(got) != 5 {
		t.Fatalf("歌词区应为 5 行: %d", len(got))
	}
	if got[2] != "四" || got[0] != "二" || got[4] != "六" {
		t.Errorf("当前句应居中: %v", got)
	}
	// 第一句未开始：按第一句居中排版但不高亮，上方留空。
	out = strip(renderLyrics(lines, -1, 5, 0, sty))
	got = strings.Split(out, "\n")
	if got[0] != "" || got[2] != "一" {
		t.Errorf("前奏排版不符: %v", got)
	}
}
