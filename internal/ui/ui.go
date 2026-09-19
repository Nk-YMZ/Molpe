// Package ui 提供基于 Bubble Tea 的终端界面。
package ui

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"molpe/internal/config"
	"molpe/internal/mpris"
	"molpe/internal/netease"
	"molpe/internal/player"
	"molpe/internal/queue"
)

type page int

const (
	pageChecking  page = iota // 启动时检查登录态
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
		Quit:       key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "退出")),
	}
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
	client *netease.Client
	cfg    config.Config
	dirs   config.Dirs
	// cfgWritable 为 false 表示启动时配置加载失败（按默认配置运行），
	// 退出时不写回，保留原文件供用户手动修复。
	cfgWritable bool

	quality string // cfg.Quality 归一化后的音质
	volume  int    // 当前音量百分比（0-100）

	theme Theme
	sty   styles
	keys  keyMap

	page      page
	login     loginModel
	playlists playlistsPage
	songs     songsPage
	account   *netease.Account
	popup     queuePopup // 播放队列弹窗

	player   *playerHolder
	queue    *queue.Queue
	endCh    chan struct{}  // mpv 自然播完通知
	mpris    *mpris.Service // 为 nil 表示桌面集成不可用
	mprisErr error
	playing  *playingInfo
	paused   bool
	playSeq  int    // 播放地址请求序号，用于丢弃过期响应
	errNote  string // 状态栏错误提示
	note     string // 状态栏普通提示（如队列操作反馈），到期自动清除
	noteID   int    // 提示序号，防止过期定时器误清更新的提示

	lyrics       []netease.LyricLine // 当前曲目歌词（按时间排序）
	lyricCur     int                 // 当前歌词行下标，-1 表示尚未到第一句
	lyricLines   int                 // 歌词显示总行数（配置 lyric_lines）
	lyricTicking bool                // 歌词滚动定时器是否在运行
	lyricSeq     int                 // 定时器序号，用于丢弃暂停/切歌后残留的过期定时器

	help   help.Model
	width  int
	height int

	cleanupOnce *sync.Once // 保证退出清理只执行一次
}

// New 创建根模型。dirs 为配置/数据/缓存目录；
// cfgErr 非空表示配置加载失败，将以默认配置运行并在状态栏提示。
func New(client *netease.Client, cfg config.Config, dirs config.Dirs, cfgErr error) Model {
	theme := DefaultTheme()
	h := help.New()
	h.Styles = help.DefaultStyles(true)
	m := Model{
		client:      client,
		cfg:         cfg,
		dirs:        dirs,
		cfgWritable: cfgErr == nil,
		quality:     netease.NormalizeQuality(cfg.Quality),
		volume:      cfg.EffectiveVolume(),
		lyricLines:  cfg.EffectiveLyricLines(),
		theme:       theme,
		sty:         newStyles(theme),
		keys:        defaultKeyMap(),
		login:       newLoginModel(client),
		player:      &playerHolder{},
		queue:       queue.New(nil, queue.ModeLoop, queue.Options{}),
		endCh:       make(chan struct{}, 1),
		help:        h,

		cleanupOnce: &sync.Once{},
	}
	if cfgErr != nil {
		m.errNote = "配置文件有误，已按默认配置运行：" + cfgErr.Error()
	}
	// MPRIS 不可用时仅记录，不影响主体功能。
	m.mpris, m.mprisErr = mpris.New(m.player.position)
	if m.mpris != nil {
		m.mpris.SetVolume(float64(m.volume) / 100)
	}
	m.restoreQueue()
	return m
}

// restoreQueue 从数据目录恢复上次退出时的队列状态；
// 上次播放的歌曲恢复为待播（暂停）状态，由用户手动开始播放。
func (m *Model) restoreQueue() {
	var st queue.State
	if err := config.LoadJSON(m.dirs.Data, queueStateFile, &st); err != nil {
		m.errNote = "恢复队列状态失败：" + err.Error()
		return
	}
	m.queue.Restore(st)
	if cur, ok := m.queue.Current(); ok {
		m.playing = &playingInfo{song: cur, level: m.quality}
		m.paused = true
	}
	m.publishState()
}

// saveQueue 将队列状态快照持久化到数据目录。
func (m *Model) saveQueue() {
	if m.dirs.Data == "" {
		return
	}
	if err := config.SaveJSON(m.dirs.Data, queueStateFile, m.queue.Snapshot()); err != nil {
		m.errNote = "保存队列状态失败：" + err.Error()
	}
}

// ShortHelp 与 FullHelp 实现 help.KeyMap，按当前页面展示按键。
func (m Model) ShortHelp() []key.Binding {
	switch m.page {
	case pageLogin:
		return []key.Binding{m.keys.RefreshQR, m.keys.Quit}
	case pagePlaylists:
		return []key.Binding{m.keys.Up, m.keys.Down, m.keys.Enter, m.keys.Next, m.keys.Prev,
			m.keys.ModeCycle, m.keys.Queue, m.keys.VolumeUp, m.keys.Refresh, m.keys.Quit}
	case pageSongs:
		enter := m.keys.Enter
		enter.SetHelp("enter", "播放")
		return []key.Binding{m.keys.Up, m.keys.Down, enter, m.keys.Toggle, m.keys.PlayNext,
			m.keys.Next, m.keys.Prev, m.keys.ModeCycle, m.keys.Queue, m.keys.VolumeUp, m.keys.Back, m.keys.Quit}
	default:
		return []key.Binding{m.keys.Quit}
	}
}

func (m Model) FullHelp() [][]key.Binding { return [][]key.Binding{m.ShortHelp()} }

func (m Model) Init() tea.Cmd {
	return tea.Batch(checkLoginCmd(m.client, true), listenMprisCmd(m.mpris), listenEndCmd(m.endCh))
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
			cmds := []tea.Cmd{m.fetchPlaylists()}
			// 配置开启自动开播时，继续播放上次退出时的歌曲。
			if m.cfg.AutoPlay && m.playing != nil {
				m.paused = false
				cmds = append(cmds, m.playCmd(m.playing.song))
			}
			return m, tea.Batch(cmds...)
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
		// 过期响应（快速切换歌单时先发出的慢请求）直接丢弃。
		if msg.playlistID != m.songs.playlist.ID {
			return m, nil
		}
		m.songs.loading = false
		m.songs.err = msg.err
		if msg.err == nil {
			m.songs.data = msg.songs
			m.songs.list.SetItems(songItems(msg.songs))
		}
		return m, nil
	case songURLFetchedMsg:
		return m.handleSongURL(msg)
	case lyricsFetchedMsg:
		return m.handleLyricsFetched(msg)
	case lyricTickMsg:
		if msg.seq != m.lyricSeq {
			return m, nil // 过期定时器（暂停/切歌后残留）
		}
		m.lyricTicking = false
		if m.playing == nil {
			return m, nil
		}
		return m, m.scheduleLyricTick()
	case noteExpiredMsg:
		if msg.id == m.noteID {
			m.note = ""
		}
		return m, nil
	case playerEndedMsg:
		// 自然播完：按队列规则连播；无可播歌曲（顺序模式到末尾）时停止。
		song, ok := m.queue.Next()
		if !ok {
			m.playing = nil
			m.paused = false
			m.publishState()
			m.saveQueue()
			m.popup.open = false
			return m, listenEndCmd(m.endCh)
		}
		m.saveQueue()
		return m, tea.Batch(m.playCmd(song), listenEndCmd(m.endCh))
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
	// 弹窗打开时按键全部由弹窗处理。
	if m.popup.open {
		return m.updatePopup(msg)
	}

	if key.Matches(msg, m.keys.Quit) {
		m.shutdown()
		return m, tea.Quit
	}

	if key.Matches(msg, m.keys.Toggle) && m.playing != nil {
		return m.toggleOrResume()
	}

	// 队列与音量控制在非登录页全局可用。
	if m.page != pageLogin {
		switch {
		case key.Matches(msg, m.keys.Queue):
			if _, ok := m.queue.Current(); !ok {
				return m, m.setNote("当前没有正在播放的曲目")
			}
			m.openQueuePopup()
			return m, nil
		case key.Matches(msg, m.keys.Next):
			return m.nextSong()
		case key.Matches(msg, m.keys.Prev):
			return m.prevSong()
		case key.Matches(msg, m.keys.ModeCycle):
			return m, m.cycleMode()
		case key.Matches(msg, m.keys.VolumeUp):
			return m, m.adjustVolume(m.cfg.EffectiveVolumeStep())
		case key.Matches(msg, m.keys.VolumeDown):
			return m, m.adjustVolume(-m.cfg.EffectiveVolumeStep())
		}
	}

	switch m.page {
	case pageLogin:
		if key.Matches(msg, m.keys.RefreshQR) {
			var cmd tea.Cmd
			m.login, cmd = m.login.refresh()
			return m, cmd
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
			return m, fetchSongsCmd(m.client, m.songs.playlist.ID)
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
			return m, fetchSongsCmd(m.client, m.songs.playlist.ID)
		case key.Matches(msg, m.keys.Back):
			m.page = pagePlaylists
		case key.Matches(msg, m.keys.Enter):
			i := m.songs.list.Selected()
			if i < 0 || i >= len(m.songs.data) {
				return m, nil
			}
			song := m.songs.data[i]
			// 手动点歌：更新歌单队列并记录历史；下一首播放队列保留。
			// 点歌是明确的播放意图，解除暂停状态（mpv 的 pause 属性跨 loadfile 保持）。
			m.queue.SetPlaylist(m.songs.data)
			m.queue.Play(song)
			m.paused = false
			m.saveQueue()
			return m, m.playCmd(song)
		case key.Matches(msg, m.keys.PlayNext):
			i := m.songs.list.Selected()
			if i < 0 || i >= len(m.songs.data) {
				return m, nil
			}
			m.queue.PlayNext(m.songs.data[i])
			m.saveQueue()
			return m, m.setNote("已加入下一首播放：" + m.songs.data[i].Name)
		}
	}
	return m, nil
}

// noteTTL 状态栏普通提示的展示时长，到期后恢复显示播放信息。
const noteTTL = 3 * time.Second

// queueStateFile 是数据目录下队列状态快照的文件名。
const queueStateFile = "queue.json"

// noteExpiredMsg 提示到期消息；id 与当前提示序号一致时才清除。
type noteExpiredMsg struct{ id int }

// setNote 设置状态栏提示并安排到期自动清除。
func (m *Model) setNote(s string) tea.Cmd {
	m.note = s
	m.noteID++
	id := m.noteID
	return tea.Tick(noteTTL, func(time.Time) tea.Msg { return noteExpiredMsg{id} })
}

// nextSong 播放队列中的下一首（历史前进 → 下一首队列 → 播放模式）。
// 切歌是明确的播放意图，暂停状态也随之解除。
func (m Model) nextSong() (tea.Model, tea.Cmd) {
	song, ok := m.queue.Next()
	if !ok {
		return m, m.setNote("没有可播放的下一首")
	}
	m.paused = false
	m.saveQueue()
	return m, m.playCmd(song)
}

// prevSong 沿历史队列回退一首。
// prevSong 沿历史队列回退一首；暂停状态随之解除。
func (m Model) prevSong() (tea.Model, tea.Cmd) {
	song, ok := m.queue.Prev()
	if !ok {
		return m, m.setNote("没有更早的播放记录")
	}
	m.paused = false
	m.saveQueue()
	return m, m.playCmd(song)
}

// cycleMode 在顺序播放 → 列表循环 → 随机播放之间切换。
func (m *Model) cycleMode() tea.Cmd {
	switch m.queue.Mode() {
	case queue.ModeSequential:
		m.queue.SetMode(queue.ModeLoop)
	case queue.ModeLoop:
		m.queue.SetMode(queue.ModeRandom)
	default:
		m.queue.SetMode(queue.ModeSequential)
	}
	m.saveQueue()
	// 模式已实时反映在状态栏播放信息中，无需额外提示。
	return nil
}

// adjustVolume 按步进调整音量（0-100），应用到播放器与桌面环境；
// 配置统一在退出时落盘，运行期间不写盘。
func (m *Model) adjustVolume(delta int) tea.Cmd {
	v := max(0, min(100, m.volume+delta))
	if v == m.volume {
		return nil
	}
	m.volume = v
	m.cfg.Volume = &v
	if p, err := m.player.get(); err == nil {
		if err := p.SetVolume(v); err != nil {
			m.errNote = err.Error()
		}
	}
	if m.mpris != nil {
		m.mpris.SetVolume(float64(v) / 100)
	}
	// 音量已实时反映在状态栏播放信息中，无需额外提示；
	// 配置统一在退出时落盘（shutdown）。
	return nil
}

// saveConfig 将当前配置写回配置文件；启动时配置加载失败则不写回，
// 避免覆盖用户待手动修复的文件。
func (m *Model) saveConfig() {
	if !m.cfgWritable {
		return
	}
	if err := config.SaveConfig(m.dirs.Config, m.cfg); err != nil {
		m.errNote = "保存配置失败：" + err.Error()
	}
}

// toggleOrResume 切换播放/暂停；恢复待播状态下播放器尚未启动时，先拉取地址开始播放。
func (m Model) toggleOrResume() (tea.Model, tea.Cmd) {
	if m.playing == nil {
		return m, nil
	}
	if _, err := m.player.get(); err != nil {
		// 待播状态：开始播放（handleSongURL 会保留 m.paused，这里先行解除）。
		m.paused = false
		return m, m.playCmd(m.playing.song)
	}
	return m, m.setPaused(!m.paused)
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

// handleSongURL 处理播放地址获取结果：启动播放器（如需要）并播放。
// 过期响应（快速连续切歌时先发出的慢请求）直接丢弃。
func (m Model) handleSongURL(msg songURLFetchedMsg) (tea.Model, tea.Cmd) {
	if msg.seq != m.playSeq {
		return m, nil
	}
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
		p.SetEndCallback(func() {
			select {
			case m.endCh <- struct{}{}:
			default:
			}
		})
		if err := p.SetVolume(m.volume); err != nil {
			m.errNote = err.Error()
		}
		m.player.set(p)
	}
	if err := p.Play(msg.url.URL); err != nil {
		m.errNote = err.Error()
		return m, nil
	}
	// mpv 的 pause 属性跨 loadfile 保持，这里无条件把 UI 的暂停状态
	// 同步给 mpv：暂停中切歌后新曲目仍为暂停，明确开播（点歌/待播恢复/
	// 自动开播）时解除上一首遗留的暂停。
	if err := p.SetPause(m.paused); err != nil {
		m.errNote = err.Error()
	}
	m.playing = &playingInfo{song: msg.song, level: msg.url.Level}
	m.errNote = ""
	m.note = ""
	// 切歌后重置歌词并异步拉取新歌词；旧曲目的滚动定时器随之失效。
	m.lyrics = nil
	m.lyricCur = -1
	m.lyricTicking = false
	m.lyricSeq++
	m.publishState()
	m.refreshQueuePopup()
	return m, m.fetchLyricsCmd(msg.song.ID)
}

// setPaused 切换暂停状态并同步桌面环境；恢复播放时重启歌词滚动定时器
// （暂停期间歌词不滚动，定时器到期后自动停止）。
func (m *Model) setPaused(paused bool) tea.Cmd {
	p, err := m.player.get()
	if err != nil {
		return nil
	}
	if err := p.SetPause(paused); err != nil {
		m.errNote = err.Error()
		return nil
	}
	m.paused = paused
	m.publishState()
	if paused {
		return nil
	}
	return m.scheduleLyricTick()
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
	if m.playing == nil {
		m.mpris.SetStatus("Stopped")
	} else if m.paused {
		m.mpris.SetStatus("Paused")
	} else {
		m.mpris.SetStatus("Playing")
	}
}

// handleMprisEvent 处理桌面环境（媒体键、KDE 媒体组件）发来的控制事件。
func (m Model) handleMprisEvent(ev mpris.Event) (Model, tea.Cmd) {
	listen := listenMprisCmd(m.mpris)
	switch ev {
	case mpris.EventPlayPause:
		nm, cmd := m.toggleOrResume()
		return nm.(Model), tea.Batch(cmd, listen)
	case mpris.EventPlay:
		if m.playing != nil {
			if _, err := m.player.get(); err != nil {
				// 待播状态下尚未启动播放器，直接拉取地址开始播放
				m.paused = false
				return m, tea.Batch(m.playCmd(m.playing.song), listen)
			}
			return m, tea.Batch(m.setPaused(false), listen)
		}
	case mpris.EventPause, mpris.EventStop:
		if m.playing != nil {
			return m, tea.Batch(m.setPaused(true), listen)
		}
	case mpris.EventNext:
		nm, cmd := m.nextSong()
		return nm.(Model), tea.Batch(cmd, listen)
	case mpris.EventPrevious:
		nm, cmd := m.prevSong()
		return nm.(Model), tea.Batch(cmd, listen)
	}
	return m, listen
}

// shutdown 释放播放器与 MPRIS 资源，并把待保存的配置落盘；可重复调用。
func (m *Model) shutdown() {
	m.cleanupOnce.Do(func() {
		m.saveConfig()
		if p, err := m.player.get(); err == nil {
			p.Close()
		}
		if m.mpris != nil {
			m.mpris.Close()
		}
	})
}

// Cleanup 供 main 在程序退出后（含 SIGTERM、运行错误等不经按键的路径）兜底清理。
func (m Model) Cleanup() { m.shutdown() }

// listHeight 计算列表可见行数；extra 为页面内额外占用的行数。
// 歌词区固定占用 lyricLines 行，同样从可用高度中扣除。
func (m Model) listHeight(extra int) int {
	h := m.height - 5 - m.lyricLines - extra
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
	content := m.viewContent()
	if m.popup.open {
		content = m.renderQueuePopup()
	}
	v := tea.NewView(content)
	v.AltScreen = true
	v.BackgroundColor = m.theme.Background
	v.WindowTitle = "木末 Molpe"
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

	header := m.sty.Title.Render("木末 Molpe")
	return lipgloss.JoinVertical(lipgloss.Left, header, "", body, "", m.lyricsView(), m.statusView(), m.help.View(m))
}

// statusView 渲染状态栏：常驻播放信息，操作提示追加其后（到期自动消失）；
// 仅错误提示会整行替换。
func (m Model) statusView() string {
	if m.errNote != "" {
		return m.sty.Error.Render(m.errNote)
	}
	var status string
	if m.playing == nil {
		status = m.sty.Muted.Render("未在播放")
	} else {
		icon := "▶ "
		if m.paused {
			icon = "⏸ "
		}
		status = m.sty.Status.Render(icon + m.playing.song.Name + " - " + m.playing.song.Artists)
		if pos, total, ok := m.queue.Position(); ok {
			status += m.sty.Muted.Render(fmt.Sprintf(" (%d/%d)", pos, total))
		}
		status += m.sty.Muted.Render(fmt.Sprintf(" [%s] [%s] [音量 %d%%]",
			qualityLabel(m.playing.level), modeLabel(m.queue.Mode()), m.volume))
		if n := len(m.queue.NextUp()); n > 0 {
			status += m.sty.Muted.Render(fmt.Sprintf(" [待播 %d]", n))
		}
	}
	if m.note != "" {
		status += m.sty.Muted.Render(" · " + m.note)
	}
	return status
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
