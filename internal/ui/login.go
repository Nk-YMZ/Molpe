package ui

import (
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/skip2/go-qrcode"

	"molpe/internal/ipc"
	"molpe/internal/netease"
)

// loginFrameInterval 是登录页状态行块状旋转帧的刷新间隔；
// 仅在登录页且二维码有效期间运行，离开登录页后彻底停止。
const loginFrameInterval = 150 * time.Millisecond

// loginModel 是登录页的本地展示状态。二维码会话由后端持有并轮询，
// 前端只负责把二维码 URL 渲染为字符画与维护状态行动画。
type loginModel struct {
	art         string // 二维码字符画
	renderedURL string // art 对应的二维码内容
	status      string
	notice      string // 附加提示（如登录页相关的操作说明）
	frame       int    // 状态行动画帧序号
}

// qrArtMsg 二维码字符画渲染结果；url 用于丢弃过期渲染。
type qrArtMsg struct {
	url string
	art string
	err error
}

// loginTickMsg 状态行帧动画消息。
type loginTickMsg struct{}

// qrChanged 判断二维码会话是否发生变化（出现/消失/内容/状态码变化）。
func qrChanged(prev, cur *ipc.QRStatus) bool {
	if prev == nil || cur == nil {
		return prev != cur
	}
	return prev.URL != cur.URL || prev.Code != cur.Code
}

// updateQR 根据后端推送的二维码会话状态更新登录页文案，
// 并在二维码内容变化时异步重新渲染字符画。
func (m *Model) updateQR(qr *ipc.QRStatus) tea.Cmd {
	if qr == nil {
		m.login.art = ""
		m.login.renderedURL = ""
		m.login.status = "正在获取二维码…"
		return nil
	}
	switch qr.Code {
	case netease.QRCodeExpired:
		m.login.status = "二维码已过期，按 r 刷新"
	case netease.QRCodeScanned:
		m.login.status = "已扫码，请在手机上确认登录"
	default:
		m.login.status = "请使用网易云音乐 App 扫码登录"
	}
	if qr.URL == m.login.renderedURL {
		return nil
	}
	m.login.renderedURL = qr.URL
	m.login.art = ""
	url := qr.URL
	return func() tea.Msg {
		code, err := qrcode.New(url, qrcode.Medium)
		if err != nil {
			return qrArtMsg{url: url, err: err}
		}
		return qrArtMsg{url: url, art: code.ToSmallString(false)}
	}
}

// scheduleLoginTick 安排登录页状态行帧动画的下一帧；仅登录页且二维码
// 有效（未过期）时运行，离开登录页或二维码过期后彻底停止。
func (m *Model) scheduleLoginTick() tea.Cmd {
	if m.loginTicking || m.page != pageLogin || m.st.QR == nil || m.st.QR.Code == netease.QRCodeExpired {
		return nil
	}
	m.loginTicking = true
	return tea.Tick(loginFrameInterval, func(time.Time) tea.Msg { return loginTickMsg{} })
}
