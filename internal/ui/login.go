package ui

import (
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/skip2/go-qrcode"

	"molpe/internal/netease"
)

// qrPollInterval 是二维码状态的轮询间隔。
const qrPollInterval = 2 * time.Second

// loginFrameInterval 是登录页状态行块状旋转帧的刷新间隔；
// 仅在登录页且二维码有效期间运行，离开登录页后彻底停止。
const loginFrameInterval = 150 * time.Millisecond

type loginModel struct {
	client  *netease.Client
	qr      *netease.QRLogin
	art     string // 二维码字符画
	status  string
	notice  string // 附加提示，如启动时检查登录态失败
	expired bool   // 二维码失效或获取失败，停止轮询
	done    bool   // 登录成功
	frame   int    // 状态行动画帧序号
}

type (
	qrFetchedMsg struct {
		qr  *netease.QRLogin
		art string
		err error
	}
	qrTickMsg    struct{}
	loginTickMsg struct{} // 状态行帧动画
	qrCheckedMsg struct {
		code int
		err  error
	}
	loginStatusMsg struct {
		account *netease.Account
		err     error
	}
)

func newLoginModel(client *netease.Client) loginModel {
	return loginModel{client: client, status: "正在获取二维码…"}
}

// checkLoginCmd 查询当前登录账号；refresh 为 true 时先刷新登录态。
func checkLoginCmd(client *netease.Client, refresh bool) tea.Cmd {
	return func() tea.Msg {
		if refresh {
			_ = client.RefreshLogin() // 刷新失败不影响本地登录态判断
		}
		acc, err := client.AccountInfo()
		return loginStatusMsg{account: acc, err: err}
	}
}

// refresh 重新获取二维码（首次进入或手动刷新）。
func (m loginModel) refresh() (loginModel, tea.Cmd) {
	m.qr = nil
	m.art = ""
	m.status = "正在获取二维码…"
	m.expired = false
	m.done = false
	return m, func() tea.Msg {
		qr, err := m.client.NewQRLogin()
		if err != nil {
			return qrFetchedMsg{err: err}
		}
		code, err := qrcode.New(qr.URL, qrcode.Medium)
		if err != nil {
			return qrFetchedMsg{err: err}
		}
		return qrFetchedMsg{qr: qr, art: code.ToSmallString(false)}
	}
}

// poll 调度下一次二维码状态检查。
func (m loginModel) poll() tea.Cmd {
	return tea.Tick(qrPollInterval, func(time.Time) tea.Msg { return qrTickMsg{} })
}

// frameTick 调度状态行帧动画的下一帧。
func (m loginModel) frameTick() tea.Cmd {
	return tea.Tick(loginFrameInterval, func(time.Time) tea.Msg { return loginTickMsg{} })
}

func (m loginModel) Update(msg tea.Msg) (loginModel, tea.Cmd) {
	switch msg := msg.(type) {
	case qrFetchedMsg:
		m.notice = ""
		if msg.err != nil {
			m.status = msg.err.Error() + "，按 r 重试"
			m.expired = true
			return m, nil
		}
		m.qr = msg.qr
		m.art = msg.art
		m.status = "请使用网易云音乐 App 扫码登录"
		return m, tea.Batch(m.poll(), m.frameTick())
	case loginTickMsg:
		if m.qr == nil || m.expired || m.done {
			return m, nil // 动画随登录页生命周期结束
		}
		m.frame++
		return m, m.frameTick()
	case qrTickMsg:
		if m.qr == nil || m.expired {
			return m, nil
		}
		unikey := m.qr.UniKey
		return m, func() tea.Msg {
			code, err := m.client.CheckQRLogin(unikey)
			return qrCheckedMsg{code: code, err: err}
		}
	case qrCheckedMsg:
		switch {
		case msg.err != nil:
			// 网络异常时静默重试
			return m, m.poll()
		case msg.code == netease.QRCodeSuccess:
			m.done = true
			m.status = "登录成功"
			return m, nil
		case msg.code == netease.QRCodeExpired:
			m.status = "二维码已过期，按 r 刷新"
			m.expired = true
			return m, nil
		case msg.code == netease.QRCodeScanned:
			m.status = "已扫码，请在手机上确认登录"
			return m, m.poll()
		default:
			return m, m.poll()
		}
	}
	return m, nil
}
