// Package ui 提供基于 Bubble Tea 的终端界面。
package ui

import (
	"fmt"
	"strings"
	"sync"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"mountain-air/internal/mpris"
	"mountain-air/internal/netease"
	"mountain-air/internal/player"
)

type page int

const (
	pageChecking  page = iota // 启动时检查登录态
	pageLogin                 // 二维码登录
	pagePlaylists             // 歌单列表
	pageSongs                 // 歌单内歌曲
)

type keyMap struct {
	Up        key.Binding
	Down      key.Binding
	Top       key.Binding
	Bottom    key.Binding
	Enter     key.Binding
	Back      key.Binding
	Toggle    key.Binding
	Refresh   key.Binding
	RefreshQR key.Binding
	Quit      key.Binding
}

var keys = keyMap{
	Up:        key.NewBinding(key.WithKeys("k", "up"), key.WithHelp("k/↑", "上")),
	Down:      key.NewBinding(key.WithKeys("j", "down"), key.WithHelp("j/↓", "下")),
	Top:       key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "顶部")),
	Bottom:    key.NewBinding(key.WithKeys("G"), key.WithHelp("G", "底部")),
	Enter:     key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "进入")),
	Back:      key.NewBinding(key.WithKeys("b"), key.WithHelp("b", "返回")),
	Toggle:    key.NewBinding(key.WithKeys("space"), key.WithHelp("space", "播放/暂停")),
	Refresh:   key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "刷新")),
	RefreshQR: key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "刷新二维码")),
	Quit:      key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "退出")),
}

// playingInfo 记录当前播放的歌曲与实际音质。
type playingInfo struct {
	song  netease.Song
	level string
}

// playerHolder 持有播放器引用，供 MPRIS 的 Position 查询回调使用。
type playerHolder struct {
	mu sync.Mutex
	p  *player.Player
}

func (h *playerHolder) set(p *player.Player) {
	h.mu.Lock()
	h.p = p
	h.mu.Unlock()
}

func (h *playerHolder) get() (*player.Player, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.p == nil {
		return nil, fmt.Errorf("播放器未启动")
	}
	return h.p, nil
}

func (h *playerHolder) position() (float64, error) {
	h.mu.Lock()
	p := h.p
	h.mu.Unlock()
	if p == nil {
		return 0, fmt.Errorf("播放器未启动")
	}
	return p.Position()
}

// Model 是 TUI 的根模型。
type Model struct {
	client  *netease.Client
	quality string
	theme   Theme
	sty     styles

	page      page
	login     loginModel
	playlists playlistsPage
	songs     songsPage
	account   *netease.Account

	player   *playerHolder
	mpris    *mpris.Service // 为 nil 表示桌面集成不可用
	mprisErr error
	playing  *playingInfo
	paused   bool
	errNote  string // 状态栏错误提示

	help   help.Model
	width  int
	height int
}

// New 创建根模型。
func New(client *netease.Client, quality string) Model {
	theme := DefaultTheme()
	h := help.New()
	h.Styles = help.DefaultStyles(true)
	m := Model{
		client:  client,
		quality: netease.NormalizeQuality(quality),
		theme:   theme,
		sty:     newStyles(theme),
		login:   newLoginModel(client),
		player:  &playerHolder{},
		help:    h,
	}
	// MPRIS 不可用时仅记录，不影响主体功能。
	m.mpris, m.mprisErr = mpris.New(m.player.position)
	return m
}

// ShortHelp 与 FullHelp 实现 help.KeyMap，按当前页面展示按键。
func (m Model) ShortHelp() []key.Binding {
	switch m.page {
	case pageLogin:
		return []key.Binding{keys.RefreshQR, keys.Quit}
	case pagePlaylists:
		return []key.Binding{keys.Up, keys.Down, keys.Enter, keys.Refresh, keys.Quit}
	case pageSongs:
		enter := keys.Enter
		enter.SetHelp("enter", "播放")
		return []key.Binding{keys.Up, keys.Down, enter, keys.Toggle, keys.Back, keys.Quit}
	default:
		return []key.Binding{keys.Quit}
	}
}

func (m Model) FullHelp() [][]key.Binding { return [][]key.Binding{m.ShortHelp()} }

func (m Model) Init() tea.Cmd {
	return tea.Batch(checkLoginCmd(m.client, true), listenMprisCmd(m.mpris))
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.playlists.list.SetHeight(m.listHeight(0))
		m.songs.list.SetHeight(m.listHeight(2))
		return m, nil
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	case loginStatusMsg:
		if msg.err == nil && msg.account != nil {
			m.account = msg.account
			m.page = pagePlaylists
			if m.mprisErr != nil {
				m.errNote = "桌面媒体集成不可用：" + m.mprisErr.Error()
			}
			return m, m.fetchPlaylists()
		}
		m.page = pageLogin
		var cmd tea.Cmd
		m.login, cmd = m.login.refresh()
		if msg.err != nil {
			m.login.notice = "检查登录状态失败：" + msg.err.Error()
		}
		return m, cmd
	case playlistsFetchedMsg:
		m.playlists.loading = false
		m.playlists.err = msg.err
		if msg.err == nil {
			m.playlists.data = msg.playlists
			m.playlists.list.SetItems(playlistItems(msg.playlists))
		}
		return m, nil
	case songsFetchedMsg:
		m.songs.loading = false
		m.songs.err = msg.err
		if msg.err == nil {
			m.songs.data = msg.songs
			m.songs.list.SetItems(songItems(msg.songs))
		}
		return m, nil
	case songURLFetchedMsg:
		return m.handleSongURL(msg)
	case mprisEventMsg:
		return m.handleMprisEvent(mpris.Event(msg))
	}

	if m.page == pageLogin {
		var cmd tea.Cmd
		m.login, cmd = m.login.Update(msg)
		if m.login.done {
			m.page = pageChecking
			return m, checkLoginCmd(m.client, false)
		}
		return m, cmd
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if key.Matches(msg, keys.Quit) {
		m.shutdown()
		return m, tea.Quit
	}

	if key.Matches(msg, keys.Toggle) && m.playing != nil {
		m.setPaused(!m.paused)
		return m, nil
	}

	switch m.page {
	case pageLogin:
		if key.Matches(msg, keys.RefreshQR) {
			var cmd tea.Cmd
			m.login, cmd = m.login.refresh()
			return m, cmd
		}
	case pagePlaylists:
		switch {
		case key.Matches(msg, keys.Up):
			m.playlists.list.Move(-1)
		case key.Matches(msg, keys.Down):
			m.playlists.list.Move(1)
		case key.Matches(msg, keys.Top):
			m.playlists.list.GoTop()
		case key.Matches(msg, keys.Bottom):
			m.playlists.list.GoBottom()
		case key.Matches(msg, keys.Refresh):
			return m, m.fetchPlaylists()
		case key.Matches(msg, keys.Enter):
			i := m.playlists.list.Selected()
			if i < 0 || i >= len(m.playlists.data) {
				return m, nil
			}
			m.songs = songsPage{playlist: m.playlists.data[i], loading: true}
			m.songs.list.SetHeight(m.listHeight(2))
			m.page = pageSongs
			return m, fetchSongsCmd(m.client, m.songs.playlist.ID)
		}
	case pageSongs:
		switch {
		case key.Matches(msg, keys.Up):
			m.songs.list.Move(-1)
		case key.Matches(msg, keys.Down):
			m.songs.list.Move(1)
		case key.Matches(msg, keys.Top):
			m.songs.list.GoTop()
		case key.Matches(msg, keys.Bottom):
			m.songs.list.GoBottom()
		case key.Matches(msg, keys.Refresh):
			m.songs.loading = true
			return m, fetchSongsCmd(m.client, m.songs.playlist.ID)
		case key.Matches(msg, keys.Back):
			m.page = pagePlaylists
		case key.Matches(msg, keys.Enter):
			i := m.songs.list.Selected()
			if i < 0 || i >= len(m.songs.data) {
				return m, nil
			}
			return m, fetchSongURLCmd(m.client, m.songs.data[i], m.quality)
		}
	}
	return m, nil
}

// handleSongURL 处理播放地址获取结果：启动播放器（如需要）并播放。
func (m Model) handleSongURL(msg songURLFetchedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.errNote = msg.err.Error()
		return m, nil
	}
	p, err := m.player.get()
	if err != nil {
		p, err = player.Start()
		if err != nil {
			m.errNote = err.Error()
			return m, nil
		}
		m.player.set(p)
	}
	if err := p.Play(msg.url.URL); err != nil {
		m.errNote = err.Error()
		return m, nil
	}
	m.playing = &playingInfo{song: msg.song, level: msg.url.Level}
	m.paused = false
	m.errNote = ""
	m.publishState()
	return m, nil
}

// setPaused 切换暂停状态并同步桌面环境。
func (m *Model) setPaused(paused bool) {
	p, err := m.player.get()
	if err != nil {
		return
	}
	if err := p.SetPause(paused); err != nil {
		m.errNote = err.Error()
		return
	}
	m.paused = paused
	m.publishState()
}

// publishState 向 MPRIS 推送当前曲目元数据与播放状态。
func (m *Model) publishState() {
	if m.mpris == nil {
		return
	}
	if m.playing != nil {
		s := m.playing.song
		m.mpris.SetTrack(mpris.Track{
			ID:       s.ID,
			Title:    s.Name,
			Artists:  strings.Split(s.Artists, "/"),
			Album:    s.Album,
			Duration: s.Duration,
			ArtURL:   s.CoverURL,
		})
	}
	if m.paused {
		m.mpris.SetStatus("Paused")
	} else {
		m.mpris.SetStatus("Playing")
	}
}

// handleMprisEvent 处理桌面环境（媒体键、KDE 媒体组件）发来的控制事件。
func (m Model) handleMprisEvent(ev mpris.Event) (Model, tea.Cmd) {
	if m.playing == nil {
		return m, listenMprisCmd(m.mpris)
	}
	switch ev {
	case mpris.EventPlayPause:
		m.setPaused(!m.paused)
	case mpris.EventPlay:
		m.setPaused(false)
	case mpris.EventPause, mpris.EventStop:
		m.setPaused(true)
	}
	return m, listenMprisCmd(m.mpris)
}

// shutdown 释放播放器与 MPRIS 资源。
func (m *Model) shutdown() {
	if p, err := m.player.get(); err == nil {
		p.Close()
	}
	if m.mpris != nil {
		m.mpris.Close()
	}
}

// listHeight 计算列表可见行数；extra 为页面内额外占用的行数。
func (m Model) listHeight(extra int) int {
	h := m.height - 5 - extra
	if h < 1 {
		return 1
	}
	return h
}

func (m Model) fetchPlaylists() tea.Cmd {
	if m.account == nil {
		return nil
	}
	m.playlists.loading = true
	m.playlists.err = nil
	return fetchPlaylistsCmd(m.client, m.account.ID)
}

func (m Model) View() tea.View {
	v := tea.NewView(m.viewContent())
	v.AltScreen = true
	v.BackgroundColor = m.theme.Background
	v.WindowTitle = "山歌 Mountain Air"
	return v
}

func (m Model) viewContent() string {
	var body string
	switch m.page {
	case pageChecking:
		body = m.sty.Muted.Render("正在检查登录状态…")
	case pageLogin:
		body = m.login.View()
	case pagePlaylists:
		body = m.playlistsView()
	case pageSongs:
		body = m.songsView()
	}

	header := m.sty.Title.Render("山歌 Mountain Air")
	return lipgloss.JoinVertical(lipgloss.Left, header, "", body, "", m.statusView(), m.help.View(m))
}

// statusView 渲染状态栏：错误提示或当前播放信息。
func (m Model) statusView() string {
	if m.errNote != "" {
		return m.sty.Error.Render(m.errNote)
	}
	if m.playing == nil {
		return m.sty.Muted.Render("未在播放")
	}
	icon := "▶ "
	if m.paused {
		icon = "⏸ "
	}
	return m.sty.Status.Render(icon+m.playing.song.Name+" - "+m.playing.song.Artists) +
		m.sty.Muted.Render(" ["+qualityLabel(m.playing.level)+"]")
}

func (m Model) playlistsView() string {
	if m.playlists.loading {
		return m.sty.Muted.Render("正在加载歌单…")
	}
	if m.playlists.err != nil {
		return m.sty.Error.Render(m.playlists.err.Error() + "，按 r 重试")
	}
	return m.playlists.list.View(m.sty)
}

func (m Model) songsView() string {
	title := m.sty.Title.Render(fmt.Sprintf("歌单：%s", m.songs.playlist.Name))
	var body string
	switch {
	case m.songs.loading:
		body = m.sty.Muted.Render("正在加载歌曲…")
	case m.songs.err != nil:
		body = m.sty.Error.Render(m.songs.err.Error() + "，按 r 重试")
	default:
		body = m.songs.list.View(m.sty)
	}
	return lipgloss.JoinVertical(lipgloss.Left, title, "", body)
}

func playlistItems(playlists []netease.Playlist) []string {
	items := make([]string, len(playlists))
	for i, p := range playlists {
		mark := "♥" // 收藏
		if p.Created {
			mark = "✎" // 创建
		}
		items[i] = fmt.Sprintf("%s %s (%d)", mark, p.Name, p.TrackCount)
	}
	return items
}

func songItems(songs []netease.Song) []string {
	items := make([]string, len(songs))
	for i, s := range songs {
		d := s.Duration
		items[i] = fmt.Sprintf("%s - %s  %02d:%02d", s.Name, s.Artists, int(d.Minutes()), int(d.Seconds())%60)
	}
	return items
}
