package netease

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// 二维码登录状态码（网易云接口定义）。
const (
	QRCodeExpired = 800 // 二维码已过期
	QRCodeWaiting = 801 // 等待扫码
	QRCodeScanned = 802 // 已扫码，等待在手机上确认
	QRCodeSuccess = 803 // 登录成功
)

// qrType 是二维码登录接口的 type 参数，决定申请到的 key 属于哪类登录：
// 1 为网页端（手机扫码时提示“登录网页版”），3 为桌面客户端。
const qrType = 3

// ErrNotLoggedIn 表示本地没有登录凭据，请求未发出。
var ErrNotLoggedIn = errors.New("未登录")

// StatusError 是服务端对请求返回的非预期业务状态（如参数错误、风控），
// 区别于请求没能完成的传输错误。
type StatusError struct {
	Op     string
	Code   float64
	Detail string // 响应体片段，便于诊断
}

func (e *StatusError) Error() string {
	if e.Detail == "" {
		return fmt.Sprintf("%s: code=%v", e.Op, e.Code)
	}
	return fmt.Sprintf("%s: code=%v，%s", e.Op, e.Code, e.Detail)
}

// requestError 把非 200 的业务状态码构造成错误：优先带出服务端给的 message
// （如风控提示），没有时带出截断后的响应体，便于诊断。
func requestError(op string, code float64, body []byte) error {
	var payload struct {
		Message string `json:"message"`
	}
	_ = json.Unmarshal(body, &payload)
	detail := payload.Message
	if detail == "" {
		detail = string(bytes.TrimSpace(body))
	}
	if runeCount := len([]rune(detail)); runeCount > 200 {
		detail = string([]rune(detail)[:200]) + "…"
	}
	return &StatusError{Op: op, Code: code, Detail: detail}
}

// IsStatusError 判断错误是否为服务端返回的业务状态（而非传输失败）。
func IsStatusError(err error) bool {
	var e *StatusError
	return errors.As(err, &e)
}

// QRLogin 表示一次二维码登录会话。
type QRLogin struct {
	UniKey string
	URL    string // 二维码内容，供终端渲染
}

// NewQRLogin 获取新的登录二维码。
func (c *Client) NewQRLogin() (*QRLogin, error) {
	code, body, err := c.call("获取登录二维码", "/api/login/qrcode/unikey", map[string]any{"type": qrType})
	if err != nil {
		return nil, err
	}
	if code != 200 {
		return nil, requestError("获取登录二维码", code, body)
	}
	var resp struct {
		UniKey string `json:"unikey"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("解析二维码响应失败: %w", err)
	}
	if resp.UniKey == "" {
		return nil, errors.New("获取登录二维码失败: 响应缺少 unikey")
	}
	return &QRLogin{UniKey: resp.UniKey, URL: "https://music.163.com/login?codekey=" + resp.UniKey}, nil
}

// CheckQRLogin 轮询二维码状态，返回 QRCode* 状态码；登录成功时服务端
// 下发的 MUSIC_U 已并入会话。状态码不属于登录流程（如风控）时返回错误。
func (c *Client) CheckQRLogin(unikey string) (int, error) {
	code, body, err := c.call("检查二维码状态", "/api/login/qrcode/client/login",
		map[string]any{"type": qrType, "key": unikey})
	if err != nil {
		return 0, err
	}
	switch code {
	case QRCodeExpired, QRCodeWaiting, QRCodeScanned, QRCodeSuccess:
		return int(code), nil
	}
	return int(code), requestError("扫码登录未被接受", code, body)
}

// Account 是当前登录账号的基本信息。
type Account struct {
	ID       int64
	Nickname string
}

// AccountInfo 查询当前登录账号；未登录（本地无凭据，或服务端判定未登录、
// 只是匿名账号）时返回 (nil, nil)。
func (c *Client) AccountInfo() (*Account, error) {
	if !c.LoggedIn() {
		return nil, nil // 没有凭据即未登录，不必请求
	}
	code, body, err := c.call("查询账号信息", "/api/nuser/account/get", map[string]any{})
	if err != nil {
		return nil, err
	}
	if code != 200 {
		return nil, requestError("查询账号信息", code, body)
	}
	var resp struct {
		Account *struct {
			ID        int64 `json:"id"`
			Anonymous bool  `json:"anonimous"`
		} `json:"account"`
		Profile *struct {
			Nickname string `json:"nickname"`
		} `json:"profile"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("解析账号信息失败: %w", err)
	}
	if resp.Account == nil || resp.Account.ID == 0 || resp.Account.Anonymous || resp.Profile == nil {
		return nil, nil
	}
	return &Account{ID: resp.Account.ID, Nickname: resp.Profile.Nickname}, nil
}

// RenewDue 报告是否到了续期检查时间：已登录，且距上次续期调用（或登录）
// 满一个间隔。
func (c *Client) RenewDue() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sess.logged() && time.Since(c.sess.RenewCheckedAt) >= renewInterval
}

// RefreshLogin 续期登录态，服务端换发新 MUSIC_U 时返回 true（新凭据已落盘）；
// 对签发不久的凭据服务端不再换发，返回 (false, nil)。请求失败时不记检查时间，
// 下次启动或手动刷新时会再试。
func (c *Client) RefreshLogin() (bool, error) {
	c.mu.Lock()
	before := c.sess.IssuedAt
	c.mu.Unlock()
	code, body, err := c.call("刷新登录态", "/api/login/token/refresh", nil)
	if err != nil {
		return false, err
	}
	c.mu.Lock()
	c.sess.RenewCheckedAt = time.Now()
	renewed := c.sess.IssuedAt.After(before)
	err = saveSession(c.path, c.sess)
	c.mu.Unlock()
	if err != nil {
		return renewed, err
	}
	if code != 200 {
		return false, requestError("刷新登录态", code, body)
	}
	return renewed, nil
}
