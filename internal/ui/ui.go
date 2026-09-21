// Package ui 提供基于 Bubble Tea 的终端界面。
//
// 界面是播放守护进程（molpe daemon）的纯客户端：不直接持有网易云接口、
// 播放器、队列与 MPRIS 资源，一切播放状态来自后端推送的 ipc.State 快照，
// 一切操作通过 IPC 命令发给后端。播放位置等连续变化的量由快照中的位置
// 锚点本地外推，不向后端周期轮询。
package ui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"molpe/internal/config"
	"molpe/internal/ipc"
	"molpe/internal/netease"
	"molpe/internal/queue"
)

type page int

const (
	pageChecking  page = iota // 等待后端确认登录态
	pageLogin                 // 二维码登录
	pagePlaylists             // 歌单列表
	pageSongs                 // 歌单内歌曲
)

type keyMap struct {
	Up         key.Binding
	Down       key.Binding
	Top        key.Binding
	Bottom     key.Binding
	Enter      key.Binding
	Back       key.Binding
	Toggle     key.Binding
	Next       key.Binding
	Prev       key.Binding
	PlayNext   key.Binding
	ModeCycle  key.Binding
	VolumeUp   key.Binding
	VolumeDown key.Binding
	Refresh    key.Binding
	RefreshQR  key.Binding
	Queue      key.Binding // 播放队列弹窗
	Delete     key.Binding // 弹窗内删除选中曲目
	Theme      key.Binding // 主题选择弹窗
	Detach     key.Binding // 转入后台：关闭界面，后端继续播放
	Quit       key.Binding
}

// defaultKeyMap 返回默认按键绑定；后续将支持在配置文件中覆盖。
func defaultKeyMap() keyMap {
	return keyMap{
		Up:         key.NewBinding(key.WithKeys("k", "up"), key.WithHelp("k/↑", "上")),
		Down:       key.NewBinding(key.WithKeys("j", "down"), key.WithHelp("j/↓", "下")),
		Top:        key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "顶部")),
		Bottom:     key.NewBinding(key.WithKeys("G"), key.WithHelp("G", "底部")),
		Enter:      key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "进入")),
		Back:       key.NewBinding(key.WithKeys("b", "esc"), key.WithHelp("b/esc", "返回")),
		Toggle:     key.NewBinding(key.WithKeys("space"), key.WithHelp("space", "播放/暂停")),
		Next:       key.NewBinding(key.WithKeys("]"), key.WithHelp("]", "下一首")),
		Prev:       key.NewBinding(key.WithKeys("["), key.WithHelp("[", "上一首")),
		PlayNext:   key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "下一首播放")),
		ModeCycle:  key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "播放模式")),
		VolumeUp:   key.NewBinding(key.WithKeys("+", "="), key.WithHelp("+/-", "音量")),
		VolumeDown: key.NewBinding(key.WithKeys("-"), key.WithHelp("", "")),
		Refresh:    key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "刷新")),
		RefreshQR:  key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "刷新二维码")),
		Queue:      key.NewBinding(key.WithKeys("l"), key.WithHelp("l", "队列")),
		Delete:     key.NewBinding(key.WithKeys("delete", "ctrl+d"), key.WithHelp("del", "删除")),
		Theme:      key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "主题")),
		Detach:     key.NewBinding(key.WithKeys("Q"), key.WithHelp("Q", "后台")),
		Quit:       key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "退出")),
	}
}

// Model 是 TUI 的根模型。
type Model struct {
	srv  *ipc.Client
	dirs config.Dirs

	theme     Theme
	themeName string // 当前主题名（对应 themes/<name>.json）
	sty       styles
	keys      keyMap

	page      page
	login     loginModel
	playlists playlistsPage
	songs     songsPage
	popup     queuePopup  // 播放队列弹窗
	picker    themePicker // 主题选择弹窗

	st   ipc.State // 最新服务端状态快照
	stAt time.Time // 快照接收时刻（播放位置本地外推的基准）

	lyrics      []netease.LyricLine // 当前曲目歌词（按时间排序）
	lyricSongID int64               // 歌词所属歌曲 ID
	lyricCur    int                 // 当前歌词行下标，-1 表示尚未到第一句

	lyricLines       int // 歌词显示总行数（配置 lyric_lines）
	lyricGapAbove    int // 歌词区与正文区之间的空行数（配置 lyric_gap_above）
	lyricGapBelow    int // 歌词区与进度条之间的空行数（配置 lyric_gap_below）
	playbackGapBelow int // 播放信息块与操作帮助行之间的空行数（配置 playback_gap_below）
	volumeStep       int // 音量调节步进百分比（配置 volume_step）

	lyricTicking    bool // 歌词滚动定时器是否在运行
	lyricSeq        int  // 定时器序号，用于丢弃暂停/切歌后残留的过期定时器
	progressTicking bool // 进度条定时器是否在运行
	progressSeq     int  // 定时器序号，用于丢弃暂停/切歌后残留的过期定时器
	loginTicking    bool // 登录页状态行帧动画定时器是否在运行

	revealTarget  []rune // 逐字出现的目标文本（切歌后的“歌名 - 艺术家”）
	revealN       int    // 已出现的字符数
	revealSeq     int    // 定时器序号，用于丢弃切歌后残留的过期定时器
	revealTicking bool   // 逐字出现定时器是否在运行

	errNote string // 状态栏错误提示
	note    string // 状态栏普通提示（如队列操作反馈），到期自动清除
	noteID  int    // 提示序号，防止过期定时器误清更新的提示

	serverGone bool // 与后端的连接已断开

	width  int
	height int

	initCmd tea.Cmd // 由初始状态快照决定的启动命令（定时器等）
}

// New 创建根模型。st 为握手时后端推送的首份状态快照；
// cfgErr 非空表示配置加载失败，将以默认配置运行并在状态栏提示；
// themeErr 非空表示主题加载失败，已回退默认主题。
// 界面只消费配置中的显示相关字段（主题、歌词行数、区块间距）。
func New(client *ipc.Client, st ipc.State, cfg config.Config, dirs config.Dirs, theme Theme, themeName string, cfgErr, themeErr error) Model {
	if themeName == "" {
		themeName = defaultThemeName
	}
	m := Model{
		srv:              client,
		dirs:             dirs,
		lyricLines:       cfg.EffectiveLyricLines(),
		lyricGapAbove:    cfg.EffectiveLyricGapAbove(),
		lyricGapBelow:    cfg.EffectiveLyricGapBelow(),
		playbackGapBelow: cfg.EffectivePlaybackGapBelow(),
		volumeStep:       cfg.EffectiveVolumeStep(),
		theme:            theme,
		themeName:        themeName,
		sty:              newStyles(theme),
		keys:             defaultKeyMap(),
	}
	if cfgErr != nil {
		m.errNote = "配置文件有误，已按默认配置运行：" + cfgErr.Error()
	}
	if themeErr != nil {
		if m.errNote != "" {
			m.errNote += "；"
		}
		m.errNote += "主题加载失败，已使用默认主题：" + themeErr.Error()
	}
	m.initCmd = m.applyState(st)
	return m
}

// ServerGone 报告与后端的连接是否已断开（供 main 在退出后提示）。
func (m Model) ServerGone() bool { return m.serverGone }

// ShortHelp 返回当前页面的按键提示（供底部帮助行渲染）。
func (m Model) ShortHelp() []key.Binding {
	switch m.page {
	case pageLogin:
		return []key.Binding{m.keys.RefreshQR, m.keys.Detach, m.keys.Quit}
	case pagePlaylists:
		return []key.Binding{m.keys.Up, m.keys.Down, m.keys.Enter, m.keys.Next, m.keys.Prev,
			m.keys.ModeCycle, m.keys.Queue, m.keys.Theme, m.keys.VolumeUp, m.keys.Refresh, m.keys.Detach, m.keys.Quit}
	case pageSongs:
		enter := m.keys.Enter
		enter.SetHelp("enter", "播放")
		return []key.Binding{m.keys.Up, m.keys.Down, enter, m.keys.Toggle, m.keys.PlayNext,
			m.keys.Next, m.keys.Prev, m.keys.ModeCycle, m.keys.Queue, m.keys.Theme, m.keys.VolumeUp, m.keys.Back, m.keys.Detach, m.keys.Quit}
	default:
		return []key.Binding{m.keys.Detach, m.keys.Quit}
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(listenServerCmd(m.srv), m.initCmd)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.playlists.list.SetHeight(m.listHeight(0))
		m.songs.list.SetHeight(m.listHeight(2))
		if m.popup.open {
			m.popup.height = max(5, min(queuePopupMaxRows, m.height-6))
			m.popup.ensureVisible()
		}
		if m.picker.open {
			m.picker.list.SetHeight(themePickerHeight(m.height))
		}
		return m, nil
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	case serverEventMsg:
		return m.handleServerEvent(ipc.Message(msg))
	case serverGoneMsg:
		m.serverGone = true
		return m, tea.Quit
	case playlistsFetchedMsg:
		m.playlists.loading = false
		m.playlists.err = msg.err
		if msg.err == nil {
			m.playlists.data = msg.playlists
			m.playlists.list.SetItems(playlistNames(msg.playlists))
		}
		return m, nil
	case songsFetchedMsg:
		// 过期响应（快速切换歌单时先发出的慢请求）直接丢弃。
		if msg.playlistID != m.songs.playlist.ID {
			return m, nil
		}
		m.songs.loading = false
		m.songs.err = msg.err
		if msg.err == nil {
			m.songs.data = msg.songs
			m.songs.list.SetItems(songNames(msg.songs))
		}
		return m, nil
	case lyricsMsg:
		return m.handleLyrics(msg)
	case lyricTickMsg:
		if msg.seq != m.lyricSeq {
			return m, nil // 过期定时器（暂停/切歌后残留）
		}
		m.lyricTicking = false
		return m, m.scheduleLyricTick()
	case progressTickMsg:
		if msg.seq != m.progressSeq {
			return m, nil // 过期定时器（暂停/切歌后残留）
		}
		m.progressTicking = false
		return m, m.scheduleProgressTick()
	case noteExpiredMsg:
		if msg.id == m.noteID {
			m.note = ""
		}
		return m, nil
	case revealTickMsg:
		if msg.seq != m.revealSeq {
			return m, nil // 过期定时器（切歌后残留）
		}
		m.revealTicking = false
		m.revealN++
		return m, m.scheduleRevealTick()
	case qrArtMsg:
		if msg.url == m.login.renderedURL {
			m.login.art = msg.art
			if msg.err != nil {
				m.login.status = "渲染二维码失败：" + msg.err.Error()
			}
		}
		return m, m.scheduleLoginTick()
	case loginTickMsg:
		m.loginTicking = false
		if m.page != pageLogin {
			return m, nil
		}
		m.login.frame++
		return m, m.scheduleLoginTick()
	}
	return m, nil
}

// handleServerEvent 处理后端推送的事件，并重新挂起事件监听。
func (m Model) handleServerEvent(msg ipc.Message) (tea.Model, tea.Cmd) {
	listen := listenServerCmd(m.srv)
	var cmd tea.Cmd
	switch msg.Type {
	case ipc.TState:
		var st ipc.State
		if msg.DecodeData(&st) == nil {
			cmd = m.applyState(st)
		}
	case ipc.TNote:
		var n ipc.Note
		if msg.DecodeData(&n) == nil {
			if n.Err {
				m.errNote = n.Text
			} else {
				cmd = m.setNote(n.Text)
			}
		}
	case ipc.TLyrics:
		var lm ipc.LyricsMsg
		if msg.DecodeData(&lm) == nil {
			cmd = m.applyLyrics(lm)
		}
	}
	return m, tea.Batch(cmd, listen)
}

// applyState 应用后端推送的状态快照：页面流转、切歌重置、定时器调度。
func (m *Model) applyState(st ipc.State) tea.Cmd {
	prev := m.st
	m.st = st
	m.stAt = time.Now()

	var cmds []tea.Cmd

	// 页面流转：登录态确认后按账号状态进入对应页面。
	if st.Checked {
		switch {
		case st.Account != nil && (m.page == pageChecking || m.page == pageLogin):
			m.page = pagePlaylists
			cmds = append(cmds, m.fetchPlaylists())
		case st.Account == nil && m.page == pageChecking:
			m.page = pageLogin
			if st.QR == nil {
				// 后端尚无二维码会话（如启动后即断开重连）：主动请求一份。
				_ = m.srv.Send(ipc.TQRNew, nil)
			}
		}
	}

	// 二维码会话变化：更新状态文案并按需重新渲染字符画。
	if qrChanged(prev.QR, st.QR) {
		cmds = append(cmds, m.updateQR(st.QR))
	}

	// 播放状态变化。
	prevID, curID := int64(-1), int64(-1)
	if prev.Playing != nil {
		prevID = prev.Playing.Song.ID
	}
	if st.Playing != nil {
		curID = st.Playing.Song.ID
	}
	trackChanged := curID != prevID
	if trackChanged {
		m.errNote = ""
		m.note = ""
		// 切歌后重置歌词，残留滚动定时器由序号作废；新歌词由后端推送。
		m.lyrics = nil
		m.lyricSongID = 0
		m.lyricCur = -1
		m.lyricTicking = false
		m.lyricSeq++
		m.progressTicking = false
		m.progressSeq++
		m.revealTicking = false
		m.revealSeq++
		if st.Playing != nil {
			m.revealTarget = []rune(st.Playing.Song.Name + " - " + st.Playing.Song.Artists)
			// 待播/歌曲间隔中直接完整显示；开播时（含间隔到期）再逐字出现。
			if st.Paused || st.GapLeft > 0 {
				m.revealN = len(m.revealTarget)
			} else {
				m.revealN = 0
				cmds = append(cmds, m.scheduleRevealTick())
			}
		} else {
			m.revealTarget = nil
			m.revealN = 0
		}
	} else if prev.GapLeft > 0 && st.GapLeft == 0 && st.Playing != nil && !st.Paused {
		// 歌曲间隔到期开播：曲名逐字出现。
		m.revealN = 0
		m.revealTicking = false
		m.revealSeq++
		cmds = append(cmds, m.scheduleRevealTick())
	}
	if trackChanged || prev.Paused != st.Paused {
		cmds = append(cmds, m.scheduleProgressTick(), m.scheduleLyricTick())
	}

	// 队列弹窗随状态刷新；播放停止时关闭。
	if m.popup.open {
		if st.Playing == nil {
			m.popup.open = false
		} else {
			m.buildQueueRows()
			m.popup.normalizeCursor(1)
			m.popup.ensureVisible()
		}
	}
	return tea.Batch(cmds...)
}

func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// 弹窗打开时按键全部由弹窗处理（主题选择弹窗优先）。
	if m.picker.open {
		return m.updatePicker(msg)
	}
	if m.popup.open {
		return m.updatePopup(msg)
	}

	if nm, cmd, ok := m.quitKey(msg); ok {
		return nm, cmd
	}

	if key.Matches(msg, m.keys.Toggle) && m.st.Playing != nil {
		_ = m.srv.Send(ipc.TToggle, nil)
		return m, nil
	}

	// 队列与音量控制在非登录页全局可用。
	if m.page != pageLogin {
		switch {
		case key.Matches(msg, m.keys.Queue):
			if m.st.Playing == nil {
				return m, m.setNote("当前没有正在播放的曲目")
			}
			m.openQueuePopup()
			return m, nil
		case key.Matches(msg, m.keys.Next):
			_ = m.srv.Send(ipc.TNext, nil)
			return m, nil
		case key.Matches(msg, m.keys.Prev):
			_ = m.srv.Send(ipc.TPrev, nil)
			return m, nil
		case key.Matches(msg, m.keys.ModeCycle):
			_ = m.srv.Send(ipc.TCycleMode, nil)
			return m, nil
		case key.Matches(msg, m.keys.Theme):
			m.openThemePicker()
			return m, nil
		case key.Matches(msg, m.keys.VolumeUp):
			_ = m.srv.Send(ipc.TVolumeDelta, ipc.VolumeCmd{Delta: m.volumeStep})
			return m, nil
		case key.Matches(msg, m.keys.VolumeDown):
			_ = m.srv.Send(ipc.TVolumeDelta, ipc.VolumeCmd{Delta: -m.volumeStep})
			return m, nil
		}
	}

	switch m.page {
	case pageLogin:
		if key.Matches(msg, m.keys.RefreshQR) {
			_ = m.srv.Send(ipc.TQRNew, nil)
			return m, nil
		}
	case pagePlaylists:
		switch {
		case key.Matches(msg, m.keys.Up):
			m.playlists.list.Move(-1)
		case key.Matches(msg, m.keys.Down):
			m.playlists.list.Move(1)
		case key.Matches(msg, m.keys.Top):
			m.playlists.list.GoTop()
		case key.Matches(msg, m.keys.Bottom):
			m.playlists.list.GoBottom()
		case key.Matches(msg, m.keys.Refresh):
			return m, m.fetchPlaylists()
		case key.Matches(msg, m.keys.Enter):
			i := m.playlists.list.Selected()
			if i < 0 || i >= len(m.playlists.data) {
				return m, nil
			}
			m.songs = songsPage{playlist: m.playlists.data[i], loading: true}
			m.songs.list.SetHeight(m.listHeight(2))
			m.page = pageSongs
			return m, m.fetchSongs(m.songs.playlist.ID)
		}
	case pageSongs:
		switch {
		case key.Matches(msg, m.keys.Up):
			m.songs.list.Move(-1)
		case key.Matches(msg, m.keys.Down):
			m.songs.list.Move(1)
		case key.Matches(msg, m.keys.Top):
			m.songs.list.GoTop()
		case key.Matches(msg, m.keys.Bottom):
			m.songs.list.GoBottom()
		case key.Matches(msg, m.keys.Refresh):
			m.songs.loading = true
			return m, m.fetchSongs(m.songs.playlist.ID)
		case key.Matches(msg, m.keys.Back):
			m.page = pagePlaylists
		case key.Matches(msg, m.keys.Enter):
			i := m.songs.list.Selected()
			if i < 0 || i >= len(m.songs.data) {
				return m, nil
			}
			_ = m.srv.Send(ipc.TPlaySong, ipc.PlaySongCmd{Playlist: m.songs.data, Song: m.songs.data[i]})
			return m, nil
		case key.Matches(msg, m.keys.PlayNext):
			i := m.songs.list.Selected()
			if i < 0 || i >= len(m.songs.data) {
				return m, nil
			}
			_ = m.srv.Send(ipc.TPlayNext, ipc.PlayNextCmd{Song: m.songs.data[i]})
			return m, nil
		}
	}
	return m, nil
}

// quitKey 处理退出类按键：q 完全退出（通知后端停止播放并结束），
// Q 转入后台（仅关闭界面，后端继续播放）。ok 表示按键已被消费。
func (m Model) quitKey(msg tea.KeyPressMsg) (nm Model, cmd tea.Cmd, ok bool) {
	switch {
	case key.Matches(msg, m.keys.Quit):
		_ = m.srv.Send(ipc.TShutdown, nil)
		return m, tea.Quit, true
	case key.Matches(msg, m.keys.Detach):
		return m, tea.Quit, true
	}
	return m, nil, false
}

// noteTTL 状态栏普通提示的展示时长，到期后恢复显示播放信息。
const noteTTL = 3 * time.Second

// noteExpiredMsg 提示到期消息；id 与当前提示序号一致时才清除。
type noteExpiredMsg struct{ id int }

// setNote 设置状态栏提示并安排到期自动清除。
func (m *Model) setNote(s string) tea.Cmd {
	m.note = s
	m.noteID++
	id := m.noteID
	return tea.Tick(noteTTL, func(time.Time) tea.Msg { return noteExpiredMsg{id} })
}

// listHeight 计算列表可见行数；extra 为页面内额外占用的行数。
// 歌词区固定占用 lyricLines 行，各区块间距与 chromeFixed 行界面框架
// 一并从可用高度中扣除。
func (m Model) listHeight(extra int) int {
	h := m.height - chromeFixed - m.lyricLines - m.lyricGapAbove - m.lyricGapBelow - m.playbackGapBelow - extra
	if h < 1 {
		return 1
	}
	return h
}

// fetchPlaylists 向后端请求歌单列表。
func (m *Model) fetchPlaylists() tea.Cmd {
	if m.st.Account == nil {
		return nil
	}
	m.playlists.loading = true
	m.playlists.err = nil
	return fetchPlaylistsCmd(m.srv)
}

// fetchSongs 向后端请求歌单歌曲。
func (m *Model) fetchSongs(playlistID int64) tea.Cmd {
	return fetchSongsCmd(m.srv, playlistID)
}

func (m Model) View() tea.View {
	v := tea.NewView(renderRoot(m.buildViewState(), m.sty))
	v.AltScreen = true
	v.BackgroundColor = m.theme.BackgroundColor()
	v.WindowTitle = "木末 Molpe"
	return v
}

// buildViewState 将 Model 收敛为一帧界面的纯数据快照，供渲染层消费。
// 列表项等已有切片直接引用，不做拷贝；歌词文本逐条复制以隔离内部类型。
func (m Model) buildViewState() viewState {
	s := viewState{
		width:            m.width,
		height:           m.height,
		headerRight:      m.headerRight(),
		lyricCur:         m.lyricCur,
		lyricLines:       m.lyricLines,
		gapAbove:         m.lyricGapAbove,
		gapBelow:         m.lyricGapBelow,
		playbackGapBelow: m.playbackGapBelow,
		helpBindings:     m.ShortHelp(),
	}
	s.progress = progressView{pos: m.playPos(), ok: m.st.Playing != nil}
	if m.st.Playing != nil {
		s.progress.dur = m.st.Playing.Song.Duration.Seconds()
	}
	if len(m.lyrics) > 0 {
		s.lyrics = make([]string, len(m.lyrics))
		for i, l := range m.lyrics {
			s.lyrics[i] = l.Text
		}
	}

	switch m.page {
	case pageChecking:
		s.body = bodyView{kind: bodyNotice, notice: "正在检查登录状态…"}
	case pageLogin:
		s.body = bodyView{kind: bodyLogin, login: loginView{
			art:    m.login.art,
			status: m.login.status,
			notice: m.login.notice,
			frame:  m.login.frame,
		}}
	case pagePlaylists:
		s.body = m.playlistsBody()
	case pageSongs:
		s.body = m.songsBody()
	}

	s.status = m.statusState()

	if m.popup.open {
		s.popup = m.popupState()
	}
	if m.picker.open {
		l := m.picker.list
		items := make([]listItemView, len(l.items))
		for i, name := range l.items {
			items[i] = listItemView{primary: name}
		}
		s.picker = &listView{items: items, cursor: l.cursor, offset: l.offset, height: l.height}
	}
	return s
}

func (m Model) playlistsBody() bodyView {
	switch {
	case m.playlists.loading:
		return bodyView{kind: bodyNotice, notice: "正在加载歌单…"}
	case m.playlists.err != nil:
		return bodyView{kind: bodyNotice, notice: m.playlists.err.Error() + "，按 r 重试", isErr: true}
	default:
		l := m.playlists.list
		return bodyView{kind: bodyList, list: listView{
			items:  playlistItems(m.playlists.data, m.theme.Glyphs),
			cursor: l.cursor, offset: l.offset, height: l.height,
		}}
	}
}

func (m Model) songsBody() bodyView {
	b := bodyView{
		title:     m.songs.playlist.Name,
		titleNote: fmt.Sprintf("%d 首", m.songs.playlist.TrackCount),
	}
	switch {
	case m.songs.loading:
		b.kind, b.notice = bodyNotice, "正在加载歌曲…"
	case m.songs.err != nil:
		b.kind, b.notice, b.isErr = bodyNotice, m.songs.err.Error()+"，按 r 重试", true
	default:
		l := m.songs.list
		b.kind, b.list = bodyList, listView{
			items:  songItems(m.songs.data),
			cursor: l.cursor, offset: l.offset, height: l.height,
		}
	}
	return b
}

// headerRight 构建头部右侧的次要信息：模式 · 音质 · 音量。
// 音质显示当前曲目的实际等级，未播放时显示配置的偏好音质。
func (m Model) headerRight() string {
	q := m.st.Quality
	if m.st.Playing != nil && m.st.Playing.Level != "" {
		q = m.st.Playing.Level
	}
	return strings.Join([]string{
		modeLabel(m.st.Queue.Mode),
		qualityLabel(q),
		fmt.Sprintf("%d%%", m.st.Volume),
	}, " · ")
}

func (m Model) statusState() statusView {
	st := statusView{err: m.errNote, note: m.note}
	if m.st.Playing == nil {
		return st
	}
	st.playing = true
	st.paused = m.st.Paused
	track := m.st.Playing.Song.Name + " - " + m.st.Playing.Song.Artists
	// 逐字出现中：只展示已出现的部分，末尾加光标块。
	if len(m.revealTarget) > 0 && m.revealN < len(m.revealTarget) {
		track = string(m.revealTarget[:m.revealN]) + "▌"
	}
	st.track = track
	if pos, total, ok := m.queuePosition(); ok {
		st.pos, st.total = pos, total
	}
	st.nextUp = len(m.st.Queue.NextUp)
	return st
}

// queuePosition 返回当前曲在歌单中的位置（从 1 计）与歌单总数。
func (m Model) queuePosition() (pos, total int, ok bool) {
	if m.st.Playing == nil {
		return 0, len(m.st.Queue.Songs), false
	}
	for i, s := range m.st.Queue.Songs {
		if s.ID == m.st.Playing.Song.ID {
			return i + 1, len(m.st.Queue.Songs), true
		}
	}
	return 0, len(m.st.Queue.Songs), false
}

// popupState 将队列弹窗行拍平为纯数据快照。
func (m Model) popupState() *popupView {
	pv := &popupView{cursor: m.popup.cursor, offset: m.popup.offset, height: m.popup.height}
	for _, r := range m.popup.rows {
		if r.kind == rowHeader {
			pv.rows = append(pv.rows, popupRowView{header: true, title: r.title})
			continue
		}
		pv.rows = append(pv.rows, popupRowView{
			text:    r.song.Name + " - " + r.song.Artists,
			current: r.kind == rowCurrent,
			dim:     r.kind == rowHistory || r.kind == rowUpcoming,
		})
	}
	return pv
}

func modeLabel(mode queue.Mode) string {
	switch mode {
	case queue.ModeSequential:
		return "顺序"
	case queue.ModeLoop:
		return "循环"
	case queue.ModeRandom:
		return "随机"
	}
	return "未知"
}

// playlistNames 返回歌单名列表（仅作为列表模型的条目计数与调试占位；
// 渲染用的结构化条目由 playlistItems 在快照时构建）。
func playlistNames(playlists []netease.Playlist) []string {
	names := make([]string, len(playlists))
	for i, p := range playlists {
		names[i] = p.Name
	}
	return names
}

// songNames 返回歌曲名列表（用途同 playlistNames）。
func songNames(songs []netease.Song) []string {
	names := make([]string, len(songs))
	for i, s := range songs {
		names[i] = s.Name
	}
	return names
}

// playlistItems 将歌单数据拍平为列式列表项：序号 / 标记 / 歌单名 / 曲目数（右列）。
func playlistItems(playlists []netease.Playlist, g Glyphs) []listItemView {
	items := make([]listItemView, len(playlists))
	for i, p := range playlists {
		mark := g.PlaylistStarred // 收藏
		if p.Created {
			mark = g.PlaylistCreated // 创建
		}
		items[i] = listItemView{
			index:   strconv.Itoa(i + 1),
			marker:  mark,
			primary: p.Name,
			right:   fmt.Sprintf("%d 首", p.TrackCount),
		}
	}
	return items
}

// songItems 将歌曲数据拍平为列式列表项：序号 / 歌名 / 艺术家 / 时长（右列）。
func songItems(songs []netease.Song) []listItemView {
	items := make([]listItemView, len(songs))
	for i, s := range songs {
		d := s.Duration
		items[i] = listItemView{
			index:     strconv.Itoa(i + 1),
			primary:   s.Name,
			secondary: s.Artists,
			right:     fmt.Sprintf("%02d:%02d", int(d.Minutes()), int(d.Seconds())%60),
		}
	}
	return items
}
