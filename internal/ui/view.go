package ui

import (
	"fmt"
	"image"
	"image/color"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/kitty"
)

// 渲染层：唯一允许使用 lipgloss 的地方（theme.go 除外）。
// 交互层（ui.go 等）在 View 前先把 Model 收敛为 viewState 纯数据快照，
// 渲染函数只消费快照与 styles，不接触 netease/queue 等内部类型。
//
// 新增功能的渲染扩展点：viewState 加字段 + buildViewState 填充 +
// 一个 renderXxx 函数 + 需要时在 Theme 增加配色/符号 token。
//
// 视觉语言：纯字符栅格。头部品牌行 + 整宽细分隔线确立秩序；列表为
// 对齐的等宽列（标记 / 主文本 / 次文本 / 右列）；明暗三级（faint <
// muted < foreground）承担层次，双强调色小剂量点缀；弹窗以字符
// 阴影浮于纯黑背景之上。

// chromeFixed 是正文区之外固定占用的行数：
// 头部(1) + 分隔线(1) + 空行(1) + 进度(1) + 状态(1) + 帮助(1)。
// 歌词区行数与各区块间距（来自配置）另计。
// 交互层按同一常量计算列表高度。
const chromeFixed = 6

// listItemView 列表项快照：按等宽列网格渲染
// （光标 / 序号 / 标记 / 主文本列 / 次文本列 / 右列），
// 切片直接引用交互层的数据，不做拷贝。
type listItemView struct {
	index     string // 序号（右对齐，微光），可为空
	marker    string // 前置标记（如歌单的创建/收藏符号），可为空
	current   bool   // 当前播放曲目的标记独立高亮
	primary   string // 主文本（歌名、歌单名）
	secondary string // 次文本（艺术家），弱化显示，可为空
	right     string // 右列（时长、曲目数），微光显示，可为空
}

// listView 列表快照。
type listView struct {
	items  []listItemView
	cursor int
	offset int
	height int // 可见行数，<=0 表示不限制
}

type bodyKind int

const (
	bodyList    bodyKind = iota // 列表正文
	bodyNotice                  // 加载/错误等提示
	bodyLogin                   // 登录页（二维码）
	bodyPlaying                 // 独立播放页
)

// bodyView 正文区快照：title 非空时先渲染标题行（歌曲页）。
type bodyView struct {
	kind      bodyKind
	title     string
	titleNote string // 标题右侧的弱化附注（如曲目数）
	notice    string
	isErr     bool
	list      listView
	login     loginView
}

type loginView struct {
	art    string // 二维码字符画
	status string
	notice string
	frame  int // 状态行动画帧序号
}

// statusView 状态栏快照：只保留正在播放的核心信息，
// 音质/模式/音量等次要信息移到头部右侧弱化显示。
type statusView struct {
	err      string // 非空时整行替换
	playing  bool
	paused   bool
	track    string // "歌名 - 艺术家"（可能处于逐字出现中，仅含已出现部分）
	pos      int    // 歌单内位置（从 1 计），0 表示不在歌单
	total    int
	nextUp   int    // 待播数量
	netRetry bool   // 播放失败等待网络自动重试中
	loading  bool   // 播放地址拉取中（无音频出声，位置冻结）
	note     string // 一次性提示
	noteErr  bool   // note 按错误样式展示
}

// progressView 播放进度快照。
type progressView struct {
	pos float64 // 已播放秒数
	dur float64 // 总时长秒数
	ok  bool    // 是否可显示（播放中且时长已知）
}

// popupRowView 队列弹窗行快照。
type popupRowView struct {
	header  bool   // 分区标题行
	title   string // header 行文本
	text    string // 曲目行文本
	current bool   // 当前播放曲目（加播放符号）
	dim     bool   // 历史/后续（弱化显示）
}

// popupView 队列弹窗快照。
type popupView struct {
	rows   []popupRowView
	cursor int
	offset int
	height int
}

type qualityPickerView struct {
	list    listView
	actual  string
	details string
}

type playerView struct {
	art                  string
	coverStatus          string
	title, artist, album string
	list                 listView
}

// viewState 一帧界面的纯数据快照。
type viewState struct {
	width  int
	height int

	headerRight string // 头部右侧的次要信息（模式 · 音质 · 音量）

	body   bodyView
	player *playerView

	lyrics           []string // 歌词原文（未截断）
	lyricCur         int      // 当前歌词行下标，-1 表示尚未到第一句
	lyricLines       int      // 歌词显示总行数
	gapAbove         int      // 歌词区与正文区之间的空行数
	gapBelow         int      // 歌词区与进度条之间的空行数
	playbackGapBelow int      // 播放信息块与操作帮助行之间的空行数

	progress progressView
	status   statusView

	popup   *popupView         // 队列弹窗，nil 表示关闭
	picker  *listView          // 主题选择弹窗，nil 表示关闭
	quality *qualityPickerView // 音质选择弹窗，nil 表示关闭

	helpBindings []key.Binding
}

// renderRoot 渲染整屏内容；弹窗打开时正文被弹窗替换。
func renderRoot(s viewState, sty styles) string {
	content := renderContent(s, sty)
	switch {
	case s.quality != nil:
		content = renderQualityPicker(*s.quality, sty, s.width, s.height)
	case s.popup != nil:
		content = renderQueuePopup(*s.popup, sty, s.width, s.height)
	case s.picker != nil:
		content = renderThemePicker(*s.picker, sty, s.width, s.height)
	}
	return content
}

func renderContent(s viewState, sty styles) string {
	// 栅格固定：正文区高度 = 总高 - chromeFixed - 歌词行数 - 各区块间距，
	// 底部区块（歌词/进度/状态/帮助）锚定在屏幕底部，不随正文内容多少浮动。
	lyricHeight, gapAbove := s.lyricLines, s.gapAbove
	if s.body.kind == bodyPlaying {
		lyricHeight, gapAbove = 0, 0
	}
	bodyH := max(1, s.height-chromeFixed-lyricHeight-gapAbove-s.gapBelow-s.playbackGapBelow)
	body := renderBody(s.body, sty, s.width, bodyH)
	if s.body.kind == bodyPlaying {
		body = renderPlayer(s, sty, bodyH)
	}
	blocks := []string{
		renderHeader(s.width, s.headerRight, sty),
		renderRule(s.width, sty),
		"",
		body,
	}
	for range gapAbove {
		blocks = append(blocks, "")
	}
	if s.body.kind != bodyPlaying {
		blocks = append(blocks, renderLyrics(s.lyrics, s.lyricCur, s.lyricLines, s.width, sty))
	}
	for range s.gapBelow {
		blocks = append(blocks, "")
	}
	status := renderStatus(s.status, sty)
	if s.body.kind == bodyPlaying {
		status = ansi.Truncate(status, max(1, s.width), "")
	}
	blocks = append(blocks,
		renderProgress(s.progress, s.width, sty),
		status,
	)
	for range s.playbackGapBelow {
		blocks = append(blocks, "")
	}
	blocks = append(blocks, renderHint(s.helpBindings, s.width, sty))
	return lipgloss.JoinVertical(lipgloss.Left, blocks...)
}

// fixHeight 将内容钳制为固定行数：超出截断，不足补空行。
func fixHeight(content string, lines int) string {
	ls := strings.Split(content, "\n")
	if len(ls) > lines {
		ls = ls[:lines]
	}
	for len(ls) < lines {
		ls = append(ls, "")
	}
	return strings.Join(ls, "\n")
}

// padHeight 只将内容补足到固定行数，不截断。
func padHeight(content string, lines int) string {
	ls := strings.Split(content, "\n")
	for len(ls) < lines {
		ls = append(ls, "")
	}
	return strings.Join(ls, "\n")
}

// centerLine 将单行内容水平居中于 width 宽度内。
func centerLine(s string, width int) string {
	pad := (width - ansi.StringWidth(s)) / 2
	if pad <= 0 {
		return s
	}
	return strings.Repeat(" ", pad) + s
}

// renderHeader 渲染头部：左侧品牌标记 + 名称，右侧次要信息（微光）；
// 宽度不足时舍弃右侧信息，保证品牌完整。
func renderHeader(width int, right string, sty styles) string {
	mark := sty.glyphs.HeaderMark
	left := sty.Accent2.Render(mark) + " " + sty.Title.Render("木末") + sty.Faint.Render(" molpe")
	lw := ansi.StringWidth(mark) + 1 + ansi.StringWidth("木末 molpe")
	if right == "" || width <= 0 {
		return left
	}
	gap := width - lw - ansi.StringWidth(right)
	if gap < 2 {
		return left
	}
	return left + strings.Repeat(" ", gap) + sty.Faint.Render(right)
}

// renderRule 渲染头部下方的整宽细分隔线（微光）。
func renderRule(width int, sty styles) string {
	if width <= 0 || sty.glyphs.Rule == "" {
		return ""
	}
	return sty.Faint.Render(strings.Repeat(sty.glyphs.Rule, width))
}

// renderBody 渲染正文区并钳制到固定行数；提示类内容在区域内居中，
// 登录页二维码只补不截（截断将无法扫描）。
func renderBody(b bodyView, sty styles, width, bodyH int) string {
	switch b.kind {
	case bodyLogin:
		return padHeight(renderLogin(b.login, sty, width), bodyH)
	case bodyNotice:
		notice := sty.Faint.Render(b.notice)
		if b.isErr {
			notice = sty.Error.Render(b.notice)
		}
		return fixHeight(centerLine(notice, width), bodyH)
	default:
		content := renderList(b.list, sty, width)
		if b.title != "" {
			head := sty.Accent2.Render("▍ ") + sty.Title.Render(b.title)
			if b.titleNote != "" {
				head += sty.Faint.Render("  " + b.titleNote)
			}
			// 标题与列表块左缘对齐（列表块居中时一并缩进）。
			if ind := listIndent(b.list, sty, width); ind > 0 {
				head = strings.Repeat(" ", ind) + head
			}
			content = lipgloss.JoinVertical(lipgloss.Left, head, "", content)
		}
		return fixHeight(content, bodyH)
	}
}

// loginFrames 登录页状态行的块状旋转帧。
var loginFrames = []string{"▖", "▘", "▝", "▗"}

// renderLogin 渲染登录页：二维码装框居中，下方为带动画帧的状态行。
func renderLogin(l loginView, sty styles, width int) string {
	var blocks []string
	if l.notice != "" {
		blocks = append(blocks, centerLine(sty.Muted.Render(l.notice), width), "")
	}
	if l.art != "" {
		box := lipgloss.NewStyle().Padding(0, 1)
		if !sty.noBorder {
			box = box.Border(sty.border).BorderForeground(sty.borderColor)
		}
		for _, line := range strings.Split(box.Render(l.art), "\n") {
			blocks = append(blocks, centerLine(line, width))
		}
		blocks = append(blocks, "")
	}
	status := l.status
	if l.art != "" && status != "" {
		frame := loginFrames[l.frame%len(loginFrames)]
		status = sty.Accent2.Render(frame) + " " + sty.Muted.Render(status)
	} else if status != "" {
		status = sty.Muted.Render(status)
	}
	blocks = append(blocks, centerLine(status, width))
	return strings.Join(blocks, "\n")
}

// listMaxWidth 列表块最大宽度；宽屏时列表块居中、两侧留白，避免铺满整行。
const listMaxWidth = 72

// listGeom 列式列表的几何参数：各列宽度、块总宽与水平居中缩进。
type listGeom struct {
	cursorW, indexW, markerW int
	nameW, artistW, rightW   int
	blockW, indent           int
}

// padLeft / padRight 按显示宽度补齐空格。
func padLeft(s string, w int) string {
	if d := w - ansi.StringWidth(s); d > 0 {
		return strings.Repeat(" ", d) + s
	}
	return s
}

func padRight(s string, w int) string {
	if d := w - ansi.StringWidth(s); d > 0 {
		return s + strings.Repeat(" ", d)
	}
	return s
}

// measureList 计算可见项的列宽与块宽：列宽取可见项内容的最大值
// （受剩余空间约束，主文本优先、次文本过窄则整列舍弃）；
// 块宽不超过 listMaxWidth，居中缩进由块宽与窗口宽度决定。
func measureList(vis []listItemView, sty styles, width int) listGeom {
	g := listGeom{cursorW: ansi.StringWidth(sty.glyphs.Cursor)}
	nameMax, artistMax := 0, 0
	for _, it := range vis {
		g.indexW = max(g.indexW, ansi.StringWidth(it.index))
		g.markerW = max(g.markerW, ansi.StringWidth(it.marker))
		nameMax = max(nameMax, ansi.StringWidth(it.primary))
		artistMax = max(artistMax, ansi.StringWidth(it.secondary))
		g.rightW = max(g.rightW, ansi.StringWidth(it.right))
	}
	avail := width
	if avail <= 0 || avail > listMaxWidth {
		avail = listMaxWidth
	}
	avail -= g.cursorW
	if g.indexW > 0 {
		avail -= g.indexW + 1
	}
	if g.markerW > 0 {
		avail -= g.markerW + 1
	}
	if g.rightW > 0 {
		avail -= g.rightW + 3
	}
	nameCap := avail
	if artistMax > 0 {
		nameCap = max(12, avail*3/5)
	}
	g.nameW = max(0, min(nameMax, nameCap, avail))
	if artistMax > 0 {
		g.artistW = min(artistMax, avail-g.nameW-3)
		if g.artistW < 6 {
			// 次文本列过窄不如不留，把空间还给主文本。
			g.artistW = 0
			g.nameW = max(0, min(nameMax, avail))
		}
	}
	g.blockW = g.cursorW
	if g.indexW > 0 {
		g.blockW += g.indexW + 1
	}
	if g.markerW > 0 {
		g.blockW += g.markerW + 1
	}
	g.blockW += g.nameW
	if g.artistW > 0 {
		g.blockW += 3 + g.artistW
	}
	if g.rightW > 0 {
		g.blockW += 3 + g.rightW
	}
	if width > g.blockW {
		g.indent = (width - g.blockW) / 2
	}
	return g
}

// listIndent 返回列表块水平居中的缩进量（供正文标题与列表块左缘对齐）。
func listIndent(l listView, sty styles, width int) int {
	if len(l.items) == 0 {
		return 0
	}
	end := len(l.items)
	if l.height > 0 && l.offset+l.height < end {
		end = l.offset + l.height
	}
	return measureList(l.items[l.offset:end], sty, width).indent
}

// renderList 渲染等宽列网格列表：光标 / 序号 / 标记 / 主文本列 / 次文本列 / 右列，
// 整块限宽并在宽屏下水平居中；序号、标记、右列为微光，次文本弱化，
// 选中行光标与主文本用主强调色加粗。窗口尺寸未知（width<=0）时按最大宽度排版。
func renderList(l listView, sty styles, width int) string {
	if len(l.items) == 0 {
		return sty.Faint.Render("（空）")
	}
	end := len(l.items)
	if l.height > 0 && l.offset+l.height < end {
		end = l.offset + l.height
	}
	vis := l.items[l.offset:end]
	g := measureList(vis, sty, width)
	indent := strings.Repeat(" ", g.indent)
	var b strings.Builder
	for i, it := range vis {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(indent)
		idx := l.offset + i
		mainStyle := sty.Item
		cursor := sty.cursorBlank
		if idx == l.cursor {
			mainStyle = sty.Selected
			cursor = sty.glyphs.Cursor
		}
		b.WriteString(mainStyle.Render(cursor))
		if g.indexW > 0 {
			b.WriteString(sty.Faint.Render(padLeft(it.index, g.indexW) + " "))
		}
		if g.markerW > 0 {
			markerStyle := sty.Faint
			if it.current {
				markerStyle = sty.Accent2
			}
			b.WriteString(markerStyle.Render(padRight(it.marker, g.markerW) + " "))
		}
		b.WriteString(mainStyle.Render(padRight(ansi.Truncate(it.primary, g.nameW, "…"), g.nameW)))
		if g.artistW > 0 {
			b.WriteString("   " + sty.Muted.Render(padRight(ansi.Truncate(it.secondary, g.artistW, "…"), g.artistW)))
		}
		if g.rightW > 0 {
			b.WriteString("   " + sty.Faint.Render(padLeft(it.right, g.rightW)))
		}
	}
	return b.String()
}

// renderStatus 渲染状态栏：常驻正在播放的核心信息，操作提示追加其后；
// 仅错误提示会整行替换。
func renderStatus(st statusView, sty styles) string {
	if st.err != "" {
		return sty.Error.Render("✗ " + st.err)
	}
	if !st.playing {
		return sty.Faint.Render("♪ 未在播放")
	}
	icon := sty.glyphs.Playing
	if st.paused || st.loading {
		icon = sty.glyphs.Paused
	}
	trackStyle := sty.Title
	if st.paused || st.loading {
		trackStyle = sty.Muted
	}
	status := sty.Accent2.Render(icon) + trackStyle.Render(st.track)
	if st.pos > 0 {
		status += sty.Faint.Render(fmt.Sprintf("  %d/%d", st.pos, st.total))
	}
	if st.netRetry {
		status += sty.Accent2.Render("  ·  等待网络")
	} else if st.loading {
		status += sty.Muted.Render("  ·  加载中")
	}
	if st.nextUp > 0 {
		status += sty.Faint.Render(fmt.Sprintf("  待播 %d", st.nextUp))
	}
	if st.note != "" {
		noteStyle := sty.Faint
		if st.noteErr {
			noteStyle = sty.Error
		}
		status += noteStyle.Render("  ·  " + st.note)
	}
	return status
}

// renderProgress 渲染播放进度条：像素块状填充 + 热端 + 微光空余部分，
// 两端为时间标签；无播放或时长未知时整行留空（保持栅格稳定）。
func renderProgress(p progressView, width int, sty styles) string {
	if !p.ok || p.dur <= 0 {
		return ""
	}
	pos := min(p.pos, p.dur)
	left, right := formatTime(pos), formatTime(p.dur)
	barW := width - 14 // 两侧时间标签与空格
	if barW < 8 {
		return sty.Faint.Render(left + " / " + right)
	}
	filled := min(int(pos/p.dur*float64(barW)), barW)
	var bar string
	if filled > 0 {
		fill := strings.Repeat(sty.glyphs.ProgressFill, filled)
		// 热端：已播放部分的最后一格用副强调色（或主题头部符号），
		// 形成色彩过渡；进度为 0 时无热端。
		head := sty.glyphs.ProgressHead
		if head == "" {
			head = sty.glyphs.ProgressFill
		}
		if ansi.StringWidth(head) == 1 && filled >= 1 {
			bar = sty.Accent.Render(strings.Repeat(sty.glyphs.ProgressFill, filled-1)) + sty.Accent2.Render(head)
		} else {
			bar = sty.Accent.Render(fill)
		}
	}
	bar += sty.Faint.Render(strings.Repeat(sty.glyphs.ProgressEmpty, barW-filled))
	return sty.Faint.Render(left) + " " + bar + " " + sty.Faint.Render(right)
}

func formatTime(sec float64) string {
	s := max(0, int(sec))
	return fmt.Sprintf("%02d:%02d", s/60, s%60)
}

// renderLyrics 渲染滚动歌词区：固定 n 行，当前句居中并以主强调色高亮，
// 其余句按与当前句的距离从前景色平滑渐变到微光色（逐行取档，档位随
// 歌词行数自适应）；开头结尾不足时留空；无歌词时整区留空。第一句尚未
// 开始时按第一句居中排版但不高亮，避免整区位置跳变。歌词文本水平居中于窗口。
func renderLyrics(lines []string, cur, n, width int, sty styles) string {
	center := n / 2
	anchor := max(0, cur) // 排版锚点：-1 时视为第一句
	maxD := max(center, n-1-center)
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		idx := i - center + anchor
		if idx < 0 || idx >= len(lines) {
			out = append(out, "")
			continue
		}
		text := lines[idx]
		if width > 0 {
			text = ansi.Truncate(text, width, "")
		}
		// 水平居中：先补左侧留白，再上样式，避免样式重置留白。
		if pad := (width - ansi.StringWidth(text)) / 2; pad > 0 {
			text = strings.Repeat(" ", pad) + text
		}
		if idx == cur {
			out = append(out, sty.Selected.Render(text))
			continue
		}
		d := idx - anchor
		if d < 0 {
			d = -d
		}
		out = append(out, lyricStyle(sty, d, maxD).Render(text))
	}
	return strings.Join(out, "\n")
}

type playerGeometry struct {
	coverWidth, coverHeight int
	leftWidth, listWidth    int
	lyricHeight, listHeight int
}

func playingLayout(width, height, lyricLines int) playerGeometry {
	w := max(1, min(200, width-4))
	if width < 58 || height < 16 {
		lyrics := max(1, min(lyricLines, height-5))
		return playerGeometry{
			leftWidth: max(1, width), listWidth: max(1, width),
			lyricHeight: lyrics, listHeight: max(1, height-lyrics-4),
		}
	}
	// 为封面至少保留四行，歌词配置较大时按可用高度收拢。
	lyrics := max(1, min(lyricLines, height-10))
	leftWidth := min(96, (w-6)/2)
	coverWidth := min(leftWidth, (height-lyrics-6)*2)
	coverWidth -= coverWidth % 2
	return playerGeometry{
		coverWidth: coverWidth, coverHeight: coverWidth / 2,
		leftWidth: leftWidth, listWidth: w - leftWidth - 6,
		lyricHeight: lyrics, listHeight: max(1, height-2),
	}
}

// 每格从 2×2 采样中选择误差最小的双颜色象限组合。
// 位顺序为左上、右上、左下、右下；互补的掩码交换前景和背景即可表示。
var coverQuadrants = [16]string{" ", "▘", "▝", "▀", "▖", "▌", "▞", "▛", "▗", "▚", "▐", "▜", "▄", "▙", "▟", "█"}

func fitCoverCell(pixels [4]color.RGBA) (foreground, background color.RGBA, glyph string) {
	bestError := int(^uint(0) >> 1)
	// 水平分割优先，保证纯色和上下分区的输出稳定。
	for _, mask := range [...]int{3, 1, 2, 4, 5, 6, 7} {
		var sums [2][3]int
		var counts [2]int
		for i, pixel := range pixels {
			group := (mask >> i) & 1
			sums[group][0] += int(pixel.R)
			sums[group][1] += int(pixel.G)
			sums[group][2] += int(pixel.B)
			counts[group]++
		}
		var colors [2]color.RGBA
		for group := range colors {
			colors[group] = color.RGBA{
				R: uint8((sums[group][0] + counts[group]/2) / counts[group]),
				G: uint8((sums[group][1] + counts[group]/2) / counts[group]),
				B: uint8((sums[group][2] + counts[group]/2) / counts[group]),
				A: 255,
			}
		}
		errorSum := 0
		for i, pixel := range pixels {
			candidate := colors[(mask>>i)&1]
			red, green, blue := int(pixel.R)-int(candidate.R), int(pixel.G)-int(candidate.G), int(pixel.B)-int(candidate.B)
			errorSum += red*red + green*green + blue*blue
		}
		if errorSum < bestError {
			bestError = errorSum
			foreground, background, glyph = colors[1], colors[0], coverQuadrants[mask]
		}
	}
	return
}

// renderCoverImage 用象限字符在每格内表达 2×2 图像结构。
// 结果在交互层缓存，歌词或进度刷新不会重新转换封面。
func renderCoverImage(img image.Image, width, height int, background color.Color) string {
	if img == nil || img.Bounds().Empty() || width < 1 || height < 1 {
		return ""
	}
	pixels := resizeCover(img, img.Bounds(), width*2, height*2)
	br, bg, bb, _ := background.RGBA()
	blend := func(p color.RGBA) color.RGBA {
		alpha := uint32(255 - p.A)
		return color.RGBA{uint8(uint32(p.R) + (br>>8)*alpha/255), uint8(uint32(p.G) + (bg>>8)*alpha/255), uint8(uint32(p.B) + (bb>>8)*alpha/255), 255}
	}
	var out strings.Builder
	for y := range height {
		for x := range width {
			cell := [4]color.RGBA{
				blend(pixels.RGBAAt(x*2, y*2)),
				blend(pixels.RGBAAt(x*2+1, y*2)),
				blend(pixels.RGBAAt(x*2, y*2+1)),
				blend(pixels.RGBAAt(x*2+1, y*2+1)),
			}
			foreground, background, glyph := fitCoverCell(cell)
			fmt.Fprintf(&out, "\x1b[38;2;%d;%d;%dm\x1b[48;2;%d;%d;%dm%s", foreground.R, foreground.G, foreground.B, background.R, background.G, background.B, glyph)
		}
		out.WriteString("\x1b[0m")
		if y < height-1 {
			out.WriteByte('\n')
		}
	}
	return out.String()
}

func renderPlayer(s viewState, sty styles, height int) string {
	if s.player == nil {
		return fixHeight(centerLine(sty.Muted.Render("正在加载歌曲…"), s.width), height)
	}
	p := s.player
	layout := playingLayout(s.width, height, s.lyricLines)
	lyrics := renderLyrics(s.lyrics, s.lyricCur, layout.lyricHeight, layout.leftWidth, sty)
	if len(s.lyrics) == 0 {
		lyrics = centerLine(sty.Muted.Render("暂无歌词"), layout.leftWidth)
	}
	lyrics = fixHeight(lyrics, layout.lyricHeight)
	text := func(value string, style lipgloss.Style, width int) string {
		return centerLine(style.Render(ansi.Truncate(value, max(1, width), "")), width)
	}
	list := p.list
	list.height = layout.listHeight
	playlist := renderBody(bodyView{kind: bodyList, title: "播放列表", titleNote: fmt.Sprintf("%d 首", len(list.items)), list: list}, sty, layout.listWidth, layout.listHeight+2)
	if layout.coverWidth == 0 {
		return fixHeight(strings.Join([]string{text(p.title+" - "+p.artist, sty.Title, s.width), lyrics, "", playlist}, "\n"), height)
	}
	art := p.art
	if art == "" {
		art = lipgloss.Place(layout.coverWidth, layout.coverHeight, lipgloss.Center, lipgloss.Center, sty.Faint.Render(ansi.Hardwrap(p.coverStatus, layout.coverWidth, true)))
	}
	art = lipgloss.PlaceHorizontal(layout.leftWidth, lipgloss.Center, art)
	left := strings.Join([]string{art, "", text(p.title, sty.Title, layout.leftWidth), text(p.artist, sty.Muted, layout.leftWidth), text(p.album, sty.Faint, layout.leftWidth), "", lyrics}, "\n")
	row := lipgloss.JoinHorizontal(lipgloss.Top, padHeight(left, height), "      ", playlist)
	return fixHeight(lipgloss.Place(s.width, height, lipgloss.Center, lipgloss.Center, row), height)
}

// lyricStyle 按与当前句的距离 d（>=0）返回渐变样式：相邻句接近前景色，
// 最远端（d == maxD）落在微光色上；渐变不可用（主题色无法解析）时
// 回退为前景/微光两档。
func lyricStyle(sty styles, d, maxD int) lipgloss.Style {
	if d <= 0 || maxD <= 1 || len(sty.lyricRamp) == 0 {
		if d <= 1 {
			return sty.Item
		}
		return sty.Faint
	}
	return sty.lyricRamp[(d-1)*(len(sty.lyricRamp)-1)/(maxD-1)]
}

// renderHint 渲染底部帮助行：按键名用副强调色、说明用微光色，
// 宽度不足时从尾部舍弃条目，保证不换行。
func renderHint(bindings []key.Binding, width int, sty styles) string {
	var parts []string
	used := 0
	for _, b := range bindings {
		h := b.Help()
		if h.Key == "" || h.Desc == "" {
			continue
		}
		w := ansi.StringWidth(h.Key) + 1 + ansi.StringWidth(h.Desc)
		if used > 0 {
			w += 2
		}
		if width > 0 && used+w > width {
			break
		}
		parts = append(parts, sty.Accent2.Render(h.Key)+" "+sty.Faint.Render(h.Desc))
		used += w
	}
	return strings.Join(parts, "  ")
}

// renderQueuePopup 渲染播放队列弹窗，带字符阴影居中放置在整个屏幕上。
func renderQueuePopup(p popupView, sty styles, width, height int) string {
	var b strings.Builder
	b.WriteString(sty.Accent2.Bold(true).Render("播放队列") + "\n")
	b.WriteString(sty.Faint.Render(strings.Repeat(sty.glyphs.Rule, queuePopupWidth-4)) + "\n")
	if len(p.rows) == 0 {
		b.WriteString(sty.Faint.Render("（空）") + "\n")
	}
	end := min(p.offset+p.height, len(p.rows))
	for i := p.offset; i < end; i++ {
		row := p.rows[i]
		if row.header {
			// 区域标题之间空一行（弹窗顶部的第一个标题除外）。
			if i > p.offset {
				b.WriteString("\n")
			}
			b.WriteString(sty.Faint.Render(sty.popupRule+" ") + sty.Muted.Render(row.title) + sty.Faint.Render(" "+sty.popupRule) + "\n")
			continue
		}
		text := ansi.Truncate(row.text, queuePopupWidth-4, "")
		marker := sty.cursorBlank
		style := sty.Item
		switch {
		case row.current:
			marker = sty.glyphs.Playing
			style = sty.Selected
		case row.dim:
			style = sty.Faint
		}
		if i == p.cursor {
			if !row.current {
				marker = sty.glyphs.Cursor
			}
			style = sty.Selected
		}
		b.WriteString(style.Render(marker+text) + "\n")
	}
	b.WriteString(sty.Accent2.Render("j/k") + sty.Faint.Render(" 移动  ") + sty.Accent2.Render("del") + sty.Faint.Render(" 删除  ") + sty.Accent2.Render("esc") + sty.Faint.Render(" 关闭"))
	return placeCentered(b.String(), sty, width, height)
}

// renderThemePicker 渲染主题选择弹窗，带字符阴影居中放置在整个屏幕上。
func renderQualityPicker(p qualityPickerView, sty styles, width, height int) string {
	w := max(1, min(44, width-8))
	hint := sty.Accent2.Render("enter") + sty.Faint.Render(" 应用  ") + sty.Accent2.Render("esc") + sty.Faint.Render(" 关闭")
	content := lipgloss.JoinVertical(lipgloss.Left,
		sty.Accent2.Bold(true).Render(ansi.Truncate("选择音质", w, "")),
		sty.Faint.Render(strings.Repeat(sty.glyphs.Rule, w)),
		renderList(p.list, sty, w),
		"",
		sty.Muted.Render(ansi.Truncate(p.actual, w, "")),
		sty.Muted.Render(ansi.Truncate(p.details, w, "")),
		sty.Faint.Render(ansi.Truncate("可用音质取决于歌曲资源与账号权限", w, "")),
		ansi.Truncate(hint, w, ""),
	)
	return placeCentered(content, sty, width, height)
}

func renderThemePicker(l listView, sty styles, width, height int) string {
	content := lipgloss.JoinVertical(lipgloss.Left,
		sty.Accent2.Bold(true).Render("选择主题"),
		sty.Faint.Render(strings.Repeat(sty.glyphs.Rule, themePickerWidth-4)),
		renderList(l, sty, themePickerWidth-4),
		"",
		sty.Accent2.Render("enter")+sty.Faint.Render(" 应用  ")+sty.Accent2.Render("esc")+sty.Faint.Render(" 关闭"),
	)
	return placeCentered(content, sty, width, height)
}

// placeCentered 将内容装入主题边框，附字符阴影后居中放置在屏幕上。
func placeCentered(content string, sty styles, width, height int) string {
	box := lipgloss.NewStyle().Padding(0, 2)
	if !sty.noBorder {
		box = box.Border(sty.border).BorderForeground(sty.borderColor)
	}
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, addShadow(box.Render(content), sty, width, height))
}

// addShadow 在内容块的右侧与底部各加一格微光字符阴影（░），
// 在纯黑背景上形成浮起的空间感；空间不足时放弃阴影。
func addShadow(s string, sty styles, width, height int) string {
	lines := strings.Split(s, "\n")
	w := 0
	for _, l := range lines {
		w = max(w, ansi.StringWidth(l))
	}
	if w == 0 || w+2 > width || len(lines)+1 > height {
		return s
	}
	shadow := sty.Faint.Render("░")
	for i, l := range lines {
		lines[i] = l + strings.Repeat(" ", w-ansi.StringWidth(l)) + shadow
	}
	bottom := strings.Repeat(" ", 2) + sty.Faint.Render(strings.Repeat("░", max(0, w-1)))
	return strings.Join(append(lines, bottom), "\n")
}

// renderGraphicsPlaceholder 由正常字符布局定位图片，重绘、弹窗和滚动均沿用同一屏幕模型。
func renderGraphicsPlaceholder(id, width, height int) string {
	var out strings.Builder
	for row := range height {
		fmt.Fprintf(&out, "\x1b[38;2;%d;%d;%dm", (id>>16)&255, (id>>8)&255, id&255)
		for column := range width {
			out.WriteRune(kitty.Placeholder)
			out.WriteRune(kitty.Diacritic(row))
			out.WriteRune(kitty.Diacritic(column))
			out.WriteRune(kitty.Diacritic(id >> 24))
		}
		out.WriteString("\x1b[39m")
		if row < height-1 {
			out.WriteByte('\n')
		}
	}
	return out.String()
}
