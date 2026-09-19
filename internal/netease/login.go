package netease

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-musicfox/netease-music/service"
	"github.com/go-musicfox/netease-music/util"
	"github.com/imroc/req/v3"
)

// 二维码登录状态码（网易云接口定义）。
const (
	QRCodeExpired = 800 // 二维码已过期
	QRCodeWaiting = 801 // 等待扫码
	QRCodeScanned = 802 // 已扫码，等待在手机上确认
	QRCodeSuccess = 803 // 登录成功
)

// qrLoginUserAgent 与二维码登录请求配套使用的浏览器 UA。
const qrLoginUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/114.0.0.0 Safari/537.36"

// qrKeyClient 专用于获取二维码 unikey。
// 背景：网易云二维码登录接口检测 TLS 指纹，标准 Go http.Client 的握手特征
// 易被风控拒绝（返回 -462），这里通过 req 模拟 Chrome 的 TLS ClientHello。
// unikey 接口不能携带共享 Jar 中的 Cookie，因此单独构建。
var qrKeyClient = req.C().
	SetUserAgent(qrLoginUserAgent).
	SetCookieJar(nil).
	SetTimeout(15 * time.Second).
	SetTLSFingerprintChrome()

// qrCheckClient 绑定当前会话 Jar，轮询二维码状态（登录 Cookie 需写入 Jar）。
func qrCheckClient(jar http.CookieJar) *req.Client {
	return req.C().
		SetUserAgent(qrLoginUserAgent).
		SetCookieJar(jar).
		SetTimeout(15 * time.Second).
		SetTLSFingerprintChrome()
}

// QRLogin 表示一次二维码登录会话。
type QRLogin struct {
	UniKey string
	URL    string // 二维码内容，供终端渲染
}

// NewQRLogin 获取新的登录二维码。
func (c *Client) NewQRLogin() (*QRLogin, error) {
	data := map[string]interface{}{
		"type":         1,
		"noCheckToken": true,
	}
	params, err := util.ApiParamsEncode(data)
	if err != nil {
		return nil, fmt.Errorf("加密请求参数失败: %w", err)
	}

	resp, err := qrKeyClient.R().
		SetHeaders(map[string]string{
			"Referer": "https://music.163.com/",
			"Origin":  "https://music.163.com",
		}).
		SetFormData(params).
		Post("https://music.163.com/weapi/login/qrcode/unikey")
	if err != nil {
		return nil, fmt.Errorf("获取登录二维码失败: %w", err)
	}

	var result struct {
		Code   int    `json:"code"`
		UniKey string `json:"unikey"`
	}
	if err := json.Unmarshal(resp.Bytes(), &result); err != nil {
		return nil, fmt.Errorf("解析二维码响应失败: %w", err)
	}
	if result.Code != 200 || result.UniKey == "" {
		return nil, fmt.Errorf("获取登录二维码失败: code=%d body=%s", result.Code, resp.Bytes())
	}

	chainID := util.GenerateChainID(c.jar)
	return &QRLogin{
		UniKey: result.UniKey,
		URL:    "http://music.163.com/login?codekey=" + result.UniKey + "&chainId=" + chainID,
	}, nil
}

// CheckQRLogin 轮询二维码状态，返回 QRCode* 状态码。
// 登录成功时服务端下发的 Cookie 已写入客户端 Jar。
func (c *Client) CheckQRLogin(unikey string) (int, error) {
	util.ApplyRequestStrategy(c.jar)

	data := map[string]interface{}{
		"type":         1,
		"noCheckToken": true,
		"key":          unikey,
	}
	params, err := util.ApiParamsEncode(data)
	if err != nil {
		return 0, fmt.Errorf("加密请求参数失败: %w", err)
	}

	resp, err := qrCheckClient(c.jar).R().
		SetHeaders(map[string]string{
			"Referer": "https://music.163.com/",
			"Origin":  "https://music.163.com",
		}).
		SetFormData(params).
		Post("https://music.163.com/weapi/login/qrcode/client/login")
	if err != nil {
		return 0, fmt.Errorf("检查二维码状态失败: %w", err)
	}

	var result struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(resp.Bytes(), &result); err != nil {
		return 0, fmt.Errorf("解析二维码状态失败: %w", err)
	}
	return result.Code, nil
}

// Account 是当前登录账号的基本信息。
type Account struct {
	ID       int64
	Nickname string
}

// AccountInfo 查询当前登录账号；未登录时返回 (nil, nil)。
func (c *Client) AccountInfo() (*Account, error) {
	s := &service.UserAccountService{}
	code, body := s.AccountInfo()
	if len(body) == 0 {
		return nil, fmt.Errorf("查询账号信息失败: code=%v", code)
	}
	var resp struct {
		Account *struct {
			ID int64 `json:"id"`
		} `json:"account"`
		Profile *struct {
			Nickname string `json:"nickname"`
		} `json:"profile"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("解析账号信息失败: %w", err)
	}
	if resp.Account == nil {
		return nil, nil
	}
	acc := &Account{ID: resp.Account.ID}
	if resp.Profile != nil {
		acc.Nickname = resp.Profile.Nickname
	}
	return acc, nil
}

// RefreshLogin 刷新登录态，延长 Cookie 有效期。
func (c *Client) RefreshLogin() error {
	s := &service.LoginRefreshService{}
	code, _, err := s.LoginRefresh()
	if err != nil {
		return fmt.Errorf("刷新登录态失败: %w", err)
	}
	if code != 200 {
		return fmt.Errorf("刷新登录态失败: code=%v", code)
	}
	return nil
}
