package server

import (
	"log"
	"time"

	"molpe/internal/netease"
)

// loginResult 登录态查询结果；startup 为 true 表示启动时的首次确认。
type loginResult struct {
	account *netease.Account
	err     error
	startup bool
}

// qrNewResult 二维码获取结果；seq 用于丢弃已被新会话取代的过期响应。
type qrNewResult struct {
	seq int
	qr  *netease.QRLogin
	err error
}

// qrTickResult 二维码轮询定时器消息。
type qrTickResult struct {
	seq int
}

// qrCheckResult 二维码状态轮询结果。
type qrCheckResult struct {
	seq  int
	code int
	err  error
}

// checkLogin 异步确认登录态；startup 表示启动时的首次确认（确认成功后若配置
// 开启自动开播则恢复播放）。已登录且到了续期检查时间时顺带续期：续期由
// 后台自动完成，无需重新扫码，只有服务端判定凭据失效才会回到登录页。
func (s *Server) checkLogin(startup bool) {
	go func() {
		acc, err := s.client.AccountInfo()
		if err == nil && acc != nil && s.client.RenewDue() {
			switch renewed, rerr := s.client.RefreshLogin(); {
			case rerr != nil:
				log.Printf("续期登录态失败: %v", rerr)
			case renewed:
				log.Print("登录态已续期，服务端换发了新凭据")
			default:
				log.Print("登录态无需续期：服务端未换发凭据")
			}
		}
		s.results <- loginResult{account: acc, err: err, startup: startup}
	}()
}

// handleLogin 处理登录态查询结果。网络错误与“确认未登录”严格区分：
// 查询失败保留现有登录态（仅提示），只有确认未登录才清除账号；
// 确认未登录且无可用二维码会话（无会话或已过期）时自动重建，
// 覆盖启动与全局刷新两个入口。
func (s *Server) handleLogin(r loginResult) {
	s.checked = true
	if r.err != nil {
		log.Printf("检查登录状态失败: %v", r.err)
		s.pushNote("检查登录状态失败："+r.err.Error(), true)
		s.pushState()
		return
	}
	if r.account == nil && s.account != nil {
		// 登录态在服务端失效：连同凭据的时间信息一起记下，便于定位被要求
		// 重新登录的原因（凭据过期、未续期，还是被风控作废）。
		log.Printf("登录态失效（%s）", s.client.SessionInfo())
	}
	s.account = r.account
	if r.startup && s.account != nil {
		log.Printf("已登录（%s）", s.client.SessionInfo())
	}
	if s.account == nil && (s.qr == nil || s.qrCode == netease.QRCodeExpired) {
		s.newQR()
	}
	s.pushState()
	if r.startup && r.account != nil && s.autoPlay && s.playing != nil {
		// 配置开启自动开播时，继续播放上次退出时的歌曲。
		s.paused = false
		s.playSong(s.playing.Song)
	}
}

// newQR 生成新的登录二维码（确认未登录后自动创建，含过期重建）。
func (s *Server) newQR() {
	s.qrSeq++
	seq := s.qrSeq
	s.stopQRPoll()
	s.qr = nil
	s.pushState() // 前端据 QR 为空显示“正在获取二维码…”
	go func() {
		qr, err := s.client.NewQRLogin()
		s.results <- qrNewResult{seq: seq, qr: qr, err: err}
	}()
}

// handleQRNew 处理二维码获取结果。
func (s *Server) handleQRNew(r qrNewResult) {
	if r.seq != s.qrSeq {
		return // 过期会话
	}
	if r.err != nil {
		s.pushNote(r.err.Error()+"，按 r 重试", true)
		return
	}
	s.qr = r.qr
	s.qrCode = netease.QRCodeWaiting
	s.pushState()
	s.scheduleQRPoll()
}

// scheduleQRPoll 安排下一次二维码状态轮询。
func (s *Server) scheduleQRPoll() {
	seq := s.qrSeq
	s.qrTimer = time.AfterFunc(qrPollInterval, func() {
		s.results <- qrTickResult{seq: seq}
	})
}

// stopQRPoll 停止二维码轮询定时器。
func (s *Server) stopQRPoll() {
	if s.qrTimer != nil {
		s.qrTimer.Stop()
		s.qrTimer = nil
	}
}

// handleQRTick 轮询定时器到期：发起一次状态检查。
func (s *Server) handleQRTick(r qrTickResult) {
	if r.seq != s.qrSeq || s.qr == nil {
		return
	}
	unikey := s.qr.UniKey
	go func() {
		code, err := s.client.CheckQRLogin(unikey)
		s.results <- qrCheckResult{seq: r.seq, code: code, err: err}
	}()
}

// handleQRCheck 处理二维码状态：成功则确认登录态，过期则停止轮询，
// 其余状态继续轮询。轮询仅在登录会话进行期间运行。
func (s *Server) handleQRCheck(r qrCheckResult) {
	if r.seq != s.qrSeq || s.qr == nil {
		return
	}
	switch {
	case r.err != nil && netease.IsStatusError(r.err):
		// 服务端明确拒绝（如风控）：停止轮询，按 r 刷新时重建二维码。
		s.stopQRPoll()
		s.qr = nil
		s.pushNote(r.err.Error()+"，按 r 重试", true)
		s.pushState()
	case r.err != nil:
		s.scheduleQRPoll() // 网络异常时静默重试
	case r.code == netease.QRCodeSuccess:
		s.stopQRPoll()
		s.qr = nil
		s.pushState()
		s.checkLogin(false)
	case r.code == netease.QRCodeExpired:
		s.qrCode = r.code
		s.pushState()
		// 过期即停止轮询，等待用户手动刷新。
	default:
		s.qrCode = r.code
		s.pushState()
		s.scheduleQRPoll()
	}
}
