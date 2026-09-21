// Package server 是 molpe 的播放守护进程：聚合网易云接口、播放队列、
// mpv 播放与 MPRIS 桌面集成，通过 unix socket 向 TUI 前端提供状态与控制能力。
//
// 所有状态变更集中在单事件循环中串行处理（queue 非并发安全）；网络请求
// 在独立 goroutine 中执行，结果经 results 通道回灌事件循环。稳态播放时
// 无任何周期性定时器：唤醒源仅有 mpv/MPRIS/IPC 事件与一次性定时器
// （歌曲间隔、二维码轮询），保证后台模式的功耗仅来自播放本身。
package server

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"molpe/internal/config"
	"molpe/internal/ipc"
	"molpe/internal/mpris"
	"molpe/internal/netease"
	"molpe/internal/player"
	"molpe/internal/queue"
)

// queueStateFile 是数据目录下队列状态快照的文件名。
const queueStateFile = "queue.json"

// qrPollInterval 是二维码状态的轮询间隔（仅在登录会话进行期间运行）。
const qrPollInterval = 2 * time.Second

// Server 是播放守护进程本体。
type Server struct {
	cfg    config.Config
	cfgErr error // 非空表示配置加载失败：按默认配置运行，退出时不写回
	dirs   config.Dirs
	client *netease.Client

	quality  string // 归一化后的音质偏好
	volume   int    // 音量百分比（0-100）
	songGap  int    // 相邻歌曲静默间隔秒数
	lyricTr  bool   // 歌词是否含翻译
	autoPlay bool

	queue    *queue.Queue
	player   *player.Player // nil 表示尚未启动或已关闭
	mpris    *mpris.Service // nil 表示桌面集成不可用
	mprisErr error

	playing *ipc.Playing // 当前曲目与实际音质，nil 表示无
	paused  bool
	anchor  posAnchor

	gapSong  *netease.Song // 间隔中待开播的下一首
	gapSeq   int           // 间隔定时器序号，用于丢弃切歌/点歌后的残留
	gapAt    time.Time     // 间隔开始时刻（计算剩余时间）
	gapTimer *time.Timer   // 间隔一次性定时器，切歌/点歌时停止

	account *netease.Account
	checked bool // 登录态是否已确认

	qr      *netease.QRLogin // 进行中的二维码登录会话
	qrCode  int              // 最近一次轮询状态码
	qrSeq   int              // 二维码会话序号，用于丢弃过期轮询结果
	qrTimer *time.Timer      // 轮询定时器，会话结束时停止

	playSeq int // 播放地址请求序号，用于丢弃过期响应

	lyricsSongID int64
	lyrics       []netease.LyricLine // 当前曲目歌词缓存，供重连的前端直接下发

	ln       net.Listener
	conn     net.Conn      // 当前唯一前端连接，nil 表示无（后台模式）
	connW    *bufio.Writer // 与 conn 配对
	inbox    chan ipc.Message
	connGone chan struct{} // 当前连接断开时由读循环关闭
	acceptCh chan net.Conn

	results chan any      // 异步网络请求结果
	endCh   chan struct{} // mpv 自然播完通知
	done    chan struct{} // shutdown 时关闭，事件循环据此退出

	shutdownOnce sync.Once
}

// New 装配播放守护进程：加载配置、初始化网易云客户端、恢复队列快照、
// 启动 MPRIS 服务（不可用时降级运行）。
func New(dirs config.Dirs) (*Server, error) {
	cfg, cfgErr := config.LoadConfig(dirs.Config)
	s := &Server{
		cfg:      cfg,
		cfgErr:   cfgErr,
		dirs:     dirs,
		quality:  netease.NormalizeQuality(cfg.Quality),
		volume:   cfg.EffectiveVolume(),
		songGap:  cfg.EffectiveSongGap(),
		lyricTr:  cfg.EffectiveLyricTranslation(),
		autoPlay: cfg.AutoPlay,
		inbox:    make(chan ipc.Message, 32),
		acceptCh: make(chan net.Conn, 1),
		results:  make(chan any, 32),
		endCh:    make(chan struct{}, 1),
		done:     make(chan struct{}),
	}
	client, err := netease.NewClient(filepath.Join(dirs.Data, "cookies"))
	if err != nil {
		return nil, err
	}
	s.client = client
	s.anchor.set(0, true)
	s.queue = queue.New(nil, queue.ModeLoop, queue.Options{RandomNoRepeat: cfg.EffectiveRandomNoRepeat()})
	s.restoreQueue()
	// MPRIS 不可用时仅记录，不影响主体功能。
	s.mpris, s.mprisErr = mpris.New(s.anchor.get)
	if s.mpris != nil {
		s.mpris.SetVolume(float64(s.volume) / 100)
		s.publishState()
	}
	return s, nil
}

// ConfigError 返回启动时的配置加载错误（配置正常时为 nil）。
func (s *Server) ConfigError() error { return s.cfgErr }

// restoreQueue 从数据目录恢复上次退出时的队列状态；
// 上次播放的歌曲恢复为待播（暂停）状态。
func (s *Server) restoreQueue() {
	var st queue.State
	if err := config.LoadJSON(s.dirs.Data, queueStateFile, &st); err == nil {
		s.queue.Restore(st)
	}
	if cur, ok := s.queue.Current(); ok {
		s.playing = &ipc.Playing{Song: cur, Level: s.quality}
		s.paused = true
		s.anchor.set(0, true)
	}
}

// saveQueue 将队列状态快照持久化到数据目录。
func (s *Server) saveQueue() {
	_ = config.SaveJSON(s.dirs.Data, queueStateFile, s.queue.Snapshot())
}

// saveConfig 将配置写回配置文件；启动时配置加载失败则不写回，
// 避免覆盖用户待手动修复的文件。
func (s *Server) saveConfig() {
	if s.cfgErr != nil {
		return
	}
	_ = config.SaveConfig(s.dirs.Config, s.cfg)
}

// Run 启动 IPC 监听并进入事件循环，直到收到 shutdown 命令或终止信号。
func (s *Server) Run() error {
	sock := ipc.SocketPath(s.dirs.Cache)
	if err := os.MkdirAll(filepath.Dir(sock), 0o700); err != nil {
		return fmt.Errorf("创建 socket 目录失败: %w", err)
	}
	_ = os.Remove(sock) // 清理上次异常退出残留的 socket 文件
	ln, err := net.Listen("unix", sock)
	if err != nil {
		return fmt.Errorf("监听 IPC socket 失败: %w", err)
	}
	s.ln = ln
	go s.acceptLoop()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	// 异步确认登录态；确认后若配置开启自动开播则恢复播放。
	s.checkLogin(true)
	return s.loop(sigCh)
}

// acceptLoop 持续接受前端连接；监听关闭后结束。
func (s *Server) acceptLoop() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		s.acceptCh <- conn
	}
}

// readLoop 读取前端命令并投递到事件循环；连接断开后通知事件循环。
func (s *Server) readLoop(conn net.Conn, gone chan struct{}) {
	r := bufio.NewReader(conn)
	for {
		msg, err := ipc.Decode(r)
		if err != nil {
			close(gone)
			return
		}
		s.inbox <- msg
	}
}

func (s *Server) loop(sigCh chan os.Signal) error {
	var mprisEvents <-chan mpris.Event
	if s.mpris != nil {
		mprisEvents = s.mpris.Events()
	}

	for {
		select {
		case <-sigCh:
			s.shutdown()
		case <-s.done:
			return nil
		case conn := <-s.acceptCh:
			s.handleConn(conn)
		case <-s.connGone:
			// 前端断开（转后台或异常退出）：后端继续运行。
			s.detachClient()
		case msg := <-s.inbox:
			s.handleMessage(msg)
		case res := <-s.results:
			s.handleResult(res)
		case <-s.endCh:
			s.handleEnded()
		case ev := <-mprisEvents:
			s.handleMprisEvent(ev)
		}
	}
}

// shutdown 落盘配置与队列，释放 mpv/MPRIS/IPC 资源；可重复调用。
func (s *Server) shutdown() {
	s.shutdownOnce.Do(func() {
		defer close(s.done)
		s.saveConfig()
		s.saveQueue()
		if s.gapTimer != nil {
			s.gapTimer.Stop()
		}
		s.stopQRPoll()
		if s.player != nil {
			s.player.Close()
			s.player = nil
		}
		if s.mpris != nil {
			s.mpris.Close()
		}
		if s.conn != nil {
			s.conn.Close()
		}
		if s.ln != nil {
			s.ln.Close()
			_ = os.Remove(ipc.SocketPath(s.dirs.Cache))
		}
	})
}

// ---- 前端连接管理 ----

// handleConn 接受新前端连接：已有连接时回复 busy 并拒绝；
// 否则启动读循环并立即推送当前状态快照与歌词缓存。
func (s *Server) handleConn(conn net.Conn) {
	if s.conn != nil {
		_ = ipc.Encode(conn, ipc.NewMessage(0, ipc.TBusy, nil))
		conn.Close()
		return
	}
	s.conn = conn
	s.connW = bufio.NewWriter(conn)
	s.connGone = make(chan struct{})
	go s.readLoop(conn, s.connGone)
	// 握手响应：附上按当前时刻刷新的位置锚点。
	s.pushState()
	if s.cfgErr != nil {
		s.pushNote("配置文件有误，已按默认配置运行："+s.cfgErr.Error(), true)
	}
	if s.mprisErr != nil {
		s.pushNote("桌面媒体集成不可用："+s.mprisErr.Error(), true)
	}
	if s.lyricsSongID != 0 && s.playing != nil && s.lyricsSongID == s.playing.Song.ID {
		s.push(ipc.NewMessage(0, ipc.TLyrics, ipc.LyricsMsg{SongID: s.lyricsSongID, Lines: s.lyrics}))
	}
}

// detachClient 清除当前前端连接。
func (s *Server) detachClient() {
	if s.conn != nil {
		s.conn.Close()
		s.conn = nil
		s.connW = nil
	}
	s.connGone = nil
}

// push 向当前前端发送消息；无连接时静默丢弃。仅在事件循环中调用。
func (s *Server) push(msg ipc.Message) {
	if s.connW == nil {
		return
	}
	if err := ipc.Encode(s.connW, msg); err != nil {
		s.detachClient()
		return
	}
	if err := s.connW.Flush(); err != nil {
		s.detachClient()
	}
}

// pushState 推送全量状态快照。
func (s *Server) pushState() {
	s.push(ipc.NewMessage(0, ipc.TState, s.state()))
}

// pushNote 推送一次性提示；err 为 true 时按错误展示。
func (s *Server) pushNote(text string, err bool) {
	s.push(ipc.NewMessage(0, ipc.TNote, ipc.Note{Text: text, Err: err}))
}

// reply 应答带 ID 的请求。
func (s *Server) reply(id int, t ipc.Type, payload any) {
	s.push(ipc.NewMessage(id, t, payload))
}

// state 汇总当前状态快照。
func (s *Server) state() ipc.State {
	pos, _ := s.anchor.get()
	st := ipc.State{
		Checked: s.checked,
		Account: s.account,
		Playing: s.playing,
		Paused:  s.paused,
		Pos:     pos,
		Volume:  s.volume,
		Quality: s.quality,
		Queue:   s.queue.Snapshot(),
	}
	if s.qr != nil {
		st.QR = &ipc.QRStatus{URL: s.qr.URL, Code: s.qrCode}
	}
	if s.gapSong != nil {
		st.GapLeft = max(0, time.Duration(s.songGap)*time.Second-time.Since(s.gapAt)).Seconds()
	}
	return st
}
