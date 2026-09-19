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
	l := listView{items: []listItemView{
		{index: "1", primary: "甲"},
		{index: "2", primary: "乙"},
		{index: "3", primary: "丙"},
	}, cursor: 1}
	out := strip(renderList(l, sty, 80))
	lines := strings.Split(out, "\n")
	if len(lines) != 3 {
		t.Fatalf("行数不符: %d", len(lines))
	}
	if !strings.Contains(lines[1], "▌") || !strings.Contains(lines[1], "乙") {
		t.Errorf("选中行缺少光标符号: %q", lines[1])
	}
	if strings.Contains(lines[0], "▌") {
		t.Errorf("未选中行不应有光标符号: %q", lines[0])
	}
}

func TestRenderListColumns(t *testing.T) {
	sty := testStyles()
	l := listView{items: []listItemView{
		{index: "1", primary: "歌名", secondary: "艺术家", right: "03:45"},
		{index: "2", primary: "另一首歌", secondary: "某人", right: "12:06"},
	}, cursor: 0}
	out := strip(renderList(l, sty, 80))
	lines := strings.Split(out, "\n")
	if len(lines) != 2 {
		t.Fatalf("行数不符: %d", len(lines))
	}
	// 列网格：两行等宽，右列对齐到同一列。
	w0, w1 := ansi.StringWidth(lines[0]), ansi.StringWidth(lines[1])
	if w0 != w1 {
		t.Errorf("各行块宽应一致: %d vs %d", w0, w1)
	}
	col0 := ansi.StringWidth(lines[0][:strings.Index(lines[0], "03:45")])
	col1 := ansi.StringWidth(lines[1][:strings.Index(lines[1], "12:06")])
	if col0 != col1 {
		t.Errorf("右列应对齐: 列 %d vs %d", col0, col1)
	}
	// 宽屏居中：块宽限 72，两侧留白。
	if !strings.HasPrefix(lines[0], "    ") {
		t.Errorf("宽屏时列表块应居中缩进: %q", lines[0])
	}
	// 窄屏：列宽收缩且不超出窗口。
	narrow := strip(renderList(l, sty, 30))
	for _, line := range strings.Split(narrow, "\n") {
		if w := ansi.StringWidth(line); w > 30 {
			t.Errorf("窄屏行宽不应超出窗口: %d: %q", w, line)
		}
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
	filled := strings.Count(out, "█")
	empty := strings.Count(out, "░")
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

func TestRenderHintFits(t *testing.T) {
	sty := testStyles()
	m := Model{keys: defaultKeyMap(), page: pageSongs}
	out := strip(renderHint(m.ShortHelp(), 40, sty))
	if ansi.StringWidth(out) > 40 {
		t.Errorf("帮助行超出宽度: %d: %q", ansi.StringWidth(out), out)
	}
	if !strings.Contains(out, "上") {
		t.Errorf("帮助行应包含靠前的按键: %q", out)
	}
}

func TestRenderContentAnchorsBottom(t *testing.T) {
	sty := testStyles()
	s := viewState{
		width: 80, height: 20, lyricLines: 3, gapAbove: 1, gapBelow: 1, playbackGapBelow: 1,
		body:     bodyView{kind: bodyList, list: listView{items: []listItemView{{primary: "甲"}, {primary: "乙"}}, cursor: 0}},
		progress: progressView{pos: 30, dur: 100, ok: true},
		status:   statusView{playing: true, track: "歌 - 手"},
	}
	lines := strings.Split(strip(renderContent(s, sty)), "\n")
	if len(lines) != 20 {
		t.Fatalf("总行数应恒等于窗口高度: %d", len(lines))
	}
	// 底部锚定：进度条与歌曲信息相邻，播放信息块之后留一行间距。
	if !strings.Contains(lines[16], "00:30") {
		t.Errorf("进度条位置不符: %q", lines[16])
	}
	if !strings.Contains(lines[17], "▶ 歌 - 手") {
		t.Errorf("歌曲信息应紧邻进度条: %q", lines[17])
	}
	if strings.TrimSpace(lines[18]) != "" {
		t.Errorf("播放信息块下方应留一行空白: %q", lines[18])
	}
	if !strings.Contains(lines[0], "木末") {
		t.Errorf("头部应在第 1 行: %q", lines[0])
	}
	// 第 2 行为整宽分隔线。
	if w := ansi.StringWidth(strings.TrimRight(lines[1], " ")); w != 80 {
		t.Errorf("分隔线应为整宽 80: 宽 %d: %q", w, lines[1])
	}
}

func TestAddShadow(t *testing.T) {
	sty := testStyles()
	out := strip(addShadow("abc\nab", sty, 80, 24))
	lines := strings.Split(out, "\n")
	if len(lines) != 3 {
		t.Fatalf("阴影应增加一行: %d", len(lines))
	}
	if !strings.HasSuffix(lines[0], "░") || !strings.HasSuffix(lines[1], "░") {
		t.Errorf("右侧应有阴影列: %q / %q", lines[0], lines[1])
	}
	if !strings.Contains(lines[2], "░") {
		t.Errorf("底部应有阴影行: %q", lines[2])
	}
	// 空间不足时放弃阴影。
	if got := addShadow("abc", sty, 4, 1); got != "abc" {
		t.Errorf("空间不足时不应加阴影: %q", got)
	}
}
