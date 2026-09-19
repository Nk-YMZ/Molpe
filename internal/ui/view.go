package ui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// 渲染层：唯一允许使用 lipgloss 的地方（theme.go 除外）。
// 交互层（ui.go 等）在 View 前先把 Model 收敛为 viewState 纯数据快照，
// 渲染函数只消费快照与 styles，不接触 netease/queue 等内部类型。
//
// 新增功能的渲染扩展点：viewState 加字段 + buildViewState 填充 +
// 一个 renderXxx 函数 + 需要时在 Theme 增加配色/符号 token。

// listView 列表快照：items 直接引用交互层的切片，不做拷贝。
type listView struct {
	items  []string
	cursor int
	offset int
	height int // 可见行数，<=0 表示不限制
}

type bodyKind int

const (
	bodyList   bodyKind = iota // 列表正文
	bodyNotice                 // 加载/错误等提示
	bodyLogin                  // 登录页（二维码）
)

// bodyView 正文区快照：title 非空时先渲染标题（歌曲页）。
type bodyView struct {
	kind   bodyKind
	title  string
	notice string
	isErr  bool
	list   listView
	login  loginView
}

type loginView struct {
	art    string // 二维码字符画
	status string
	notice string
}

// statusView 状态栏快照。
type statusView struct {
	err     string // 非空时整行替换
	playing bool
	paused  bool
	track   string // "歌名 - 艺术家"
	pos     int    // 歌单内位置（从 1 计），0 表示不在歌单
	total   int
	quality string
	mode    string
	volume  int
	nextUp  int // 待播数量
	note    string
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

// viewState 一帧界面的纯数据快照。
type viewState struct {
	width  int
	height int

	body bodyView

	lyrics     []string // 歌词原文（未截断）
	lyricCur   int      // 当前歌词行下标，-1 表示尚未到第一句
	lyricLines int      // 歌词显示总行数

	status statusView

	popup  *popupView // 队列弹窗，nil 表示关闭
	picker *listView  // 主题选择弹窗，nil 表示关闭

	helpBindings []key.Binding
}

// staticHelpMap 将快照中的按键绑定适配为 help.KeyMap。
type staticHelpMap struct{ bindings []key.Binding }

func (s staticHelpMap) ShortHelp() []key.Binding  { return s.bindings }
func (s staticHelpMap) FullHelp() [][]key.Binding { return [][]key.Binding{s.bindings} }

// renderRoot 渲染整屏内容；弹窗打开时正文被弹窗替换。
func renderRoot(s viewState, sty styles, h help.Model) string {
	content := renderContent(s, sty, h)
	switch {
	case s.popup != nil:
		content = renderQueuePopup(*s.popup, sty, s.width, s.height)
	case s.picker != nil:
		content = renderThemePicker(*s.picker, sty, s.width, s.height)
	}
	return content
}

func renderContent(s viewState, sty styles, h help.Model) string {
	header := sty.Title.Render("木末 Molpe")
	return lipgloss.JoinVertical(lipgloss.Left,
		header, "",
		renderBody(s.body, sty), "",
		renderLyrics(s.lyrics, s.lyricCur, s.lyricLines, s.width, sty),
		renderStatus(s.status, sty),
		h.View(staticHelpMap{s.helpBindings}),
	)
}

func renderBody(b bodyView, sty styles) string {
	var content string
	switch b.kind {
	case bodyLogin:
		content = renderLogin(b.login)
	case bodyNotice:
		if b.isErr {
			content = sty.Error.Render(b.notice)
		} else {
			content = sty.Muted.Render(b.notice)
		}
	default:
		content = renderList(b.list, sty)
	}
	if b.title == "" {
		return content
	}
	return lipgloss.JoinVertical(lipgloss.Left, sty.Title.Render(b.title), "", content)
}

func renderLogin(l loginView) string {
	var s string
	if l.notice != "" {
		s = l.notice + "\n\n"
	}
	if l.art != "" {
		s += l.art + "\n"
	}
	return s + l.status
}

func renderList(l listView, sty styles) string {
	if len(l.items) == 0 {
		return sty.Muted.Render("（空）")
	}
	end := len(l.items)
	if l.height > 0 && l.offset+l.height < end {
		end = l.offset + l.height
	}
	var b strings.Builder
	for i := l.offset; i < end; i++ {
		line, style := sty.cursorBlank+l.items[i], sty.Item
		if i == l.cursor {
			line, style = sty.glyphs.Cursor+l.items[i], sty.Selected
		}
		if i > l.offset {
			b.WriteByte('\n')
		}
		b.WriteString(style.Render(line))
	}
	return b.String()
}

// renderStatus 渲染状态栏：常驻播放信息，操作提示追加其后；
// 仅错误提示会整行替换。
func renderStatus(st statusView, sty styles) string {
	if st.err != "" {
		return sty.Error.Render(st.err)
	}
	var status string
	if !st.playing {
		status = sty.Muted.Render("未在播放")
	} else {
		icon := sty.glyphs.Playing
		if st.paused {
			icon = sty.glyphs.Paused
		}
		status = sty.Status.Render(icon + st.track)
		if st.pos > 0 {
			status += sty.Muted.Render(fmt.Sprintf(" (%d/%d)", st.pos, st.total))
		}
		status += sty.Muted.Render(fmt.Sprintf(" [%s] [%s] [音量 %d%%]", st.quality, st.mode, st.volume))
		if st.nextUp > 0 {
			status += sty.Muted.Render(fmt.Sprintf(" [待播 %d]", st.nextUp))
		}
	}
	if st.note != "" {
		status += sty.Muted.Render(" · " + st.note)
	}
	return status
}

// renderLyrics 渲染滚动歌词区：固定 n 行，当前句居中高亮，上下各占一半，
// 开头结尾不足时留空；无歌词时整区留空。第一句尚未开始时按第一句
// 居中排版但不高亮，避免整区位置跳变。
func renderLyrics(lines []string, cur, n, width int, sty styles) string {
	center := n / 2
	anchor := max(0, cur) // 排版锚点：-1 时视为第一句
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
		if idx == cur {
			out = append(out, sty.Status.Render(text))
		} else {
			out = append(out, sty.Muted.Render(text))
		}
	}
	return strings.Join(out, "\n")
}

// renderQueuePopup 渲染播放队列弹窗并居中放置在整个屏幕上。
func renderQueuePopup(p popupView, sty styles, width, height int) string {
	var b strings.Builder
	b.WriteString(sty.Title.Render("播放队列") + "\n")
	if len(p.rows) == 0 {
		b.WriteString(sty.Muted.Render("（空）") + "\n")
	}
	end := min(p.offset+p.height, len(p.rows))
	for i := p.offset; i < end; i++ {
		row := p.rows[i]
		if row.header {
			// 区域标题之间空一行（弹窗顶部的第一个标题除外）。
			if i > p.offset {
				b.WriteString("\n")
			}
			b.WriteString(sty.Muted.Render(sty.popupRule+" "+row.title+" "+sty.popupRule) + "\n")
			continue
		}
		text := ansi.Truncate(row.text, queuePopupWidth-2, "")
		marker := sty.cursorBlank
		style := sty.Item
		if row.current {
			marker = sty.glyphs.Playing
		}
		if row.dim {
			style = sty.Muted
		}
		if i == p.cursor {
			style = sty.Selected
		}
		b.WriteString(style.Render(marker+text) + "\n")
	}
	b.WriteString(sty.Muted.Render("↑/↓ 移动 · del 删除 · l/b/esc 关闭"))
	return placeCentered(b.String(), sty, width, height)
}

// renderThemePicker 渲染主题选择弹窗并居中放置在整个屏幕上。
func renderThemePicker(l listView, sty styles, width, height int) string {
	body := renderList(l, sty)
	content := lipgloss.JoinVertical(lipgloss.Left,
		sty.Title.Render("选择主题"), "",
		body, "",
		sty.Muted.Render("enter 应用 · t/b/esc 关闭"),
	)
	return placeCentered(content, sty, width, height)
}

// placeCentered 将内容装入圆角边框并居中放置在屏幕上。
func placeCentered(content string, sty styles, width, height int) string {
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(sty.borderColor).
		Padding(0, 1).
		Render(content)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}
