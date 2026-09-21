package server

import (
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

// checkLogin 异步确认登录态；startup 表示启动时的首次确认
// （确认成功后若配置开启自动开播则恢复播放）。
func (s *Server) checkLogin(startup bool) {
	go func() {
		_ = s.client.RefreshLogin() // 刷新失败不影响本地登录态判断
		acc, err := s.client.AccountInfo()
		s.results <- loginResult{account: acc, err: err, startup: startup}
	}()
}

// handleLogin 处理登录态查询结果。
func (s *Server) handleLogin(r loginResult) {
	s.checked = true
	s.account = r.account
	if r.err != nil {
		s.pushNote("检查登录状态失败："+r.err.Error(), true)
	}
	s.pushState()
	if r.startup && r.account != nil && s.autoPlay && s.playing != nil {
		// 配置开启自动开播时，继续播放上次退出时的歌曲。
		s.paused = false
		s.playSong(s.playing.Song)
	}
}

// newQR 生成新的登录二维码（首次进入登录页或手动刷新）。
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
