package ui

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/help"
	"github.com/charmbracelet/x/ansi"
)

// strip 剥离 ANSI 转义序列，便于断言纯文本。
func strip(s string) string { return ansi.Strip(s) }

func newHelpForTest() help.Model { return help.New() }

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
	playing := strip(renderStatus(statusView{playing: true, track: "歌 - 手"}, sty))
	if !strings.Contains(playing, "▶ 歌 - 手") {
		t.Errorf("播放中应使用播放符号: %q", playing)
	}
	paused := strip(renderStatus(statusView{playing: true, paused: true, track: "歌 - 手"}, sty))
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

func TestRenderProgress(t *testing.T) {
	sty := testStyles()
	if got := renderProgress(progressView{}, 80, sty); got != "" {
		t.Errorf("无播放时应留空: %q", got)
	}
	out := strip(renderProgress(progressView{pos: 30, dur: 100, ok: true}, 80, sty))
	if !strings.Contains(out, "00:30") || !strings.Contains(out, "01:40") {
		t.Errorf("时间标签缺失: %q", out)
	}
	filled := strings.Count(out, "━") + strings.Count(out, "●")
	empty := strings.Count(out, "─")
	if filled+empty != 66 {
		t.Errorf("进度条总宽应为 66: fill=%d empty=%d", filled, empty)
	}
	// 30% 进度，已播放部分约占三成。
	if filled < 18 || filled > 22 {
		t.Errorf("进度比例不符: filled=%d/66", filled)
	}
}

func TestRenderProgressNarrow(t *testing.T) {
	sty := testStyles()
	out := strip(renderProgress(progressView{pos: 30, dur: 100, ok: true}, 20, sty))
	if out != "00:30 / 01:40" {
		t.Errorf("窄终端应退化为时间文本: %q", out)
	}
}

func TestRenderContentAnchorsBottom(t *testing.T) {
	sty := testStyles()
	h := newHelpForTest()
	s := viewState{
		width: 80, height: 20, lyricLines: 3,
		body:   bodyView{kind: bodyList, list: listView{items: []string{"甲", "乙"}, cursor: 0}},
		status: statusView{playing: true, track: "歌 - 手"},
	}
	lines := strings.Split(strip(renderContent(s, sty, h)), "\n")
	if len(lines) != 20 {
		t.Fatalf("总行数应恒等于窗口高度: %d", len(lines))
	}
	// 底部锚定：倒数第 3 行是进度条（无播放进度时留空），倒数第 2 行是状态栏。
	if strings.TrimSpace(lines[17]) != "" {
		t.Errorf("无进度信息时进度条行应留空: %q", lines[17])
	}
	if !strings.Contains(lines[18], "▶ 歌 - 手") {
		t.Errorf("状态栏应锚定在倒数第 2 行: %q", lines[18])
	}
	if !strings.Contains(lines[0], "木末 Molpe") {
		t.Errorf("头部应在第 1 行: %q", lines[0])
	}
}
