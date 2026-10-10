// request.go — 网易云接口的统一传输层。
//
// 所有接口请求（WEAPI/EAPI/linuxapi）都通过带 Chrome TLS 指纹的 req
// 客户端发送：网易云风控会检测 TLS 握手特征，标准 Go http.Client 的
// 请求可能被概率性重置/拒绝（二维码登录的 -462 与播放接口的 520 传输
// 错误均属此类）。加解密沿用 netease-music 库的导出函数，仅替换传输层。
package netease

import (
	"bytes"
	"compress/zlib"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/go-musicfox/netease-music/util"
	"github.com/imroc/req/v3"
)

// 各加密通道配套的 UA（与 netease-music 库一致）。
const (
	pcUA          = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36 Edg/124.0.0.0"
	linuxUA       = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/60.0.3112.90 Safari/537.36"
	iosAppVersion = "9.0.65"
)

// musicURL 是共享 Cookie 的作用域。
var musicURL = &url.URL{Scheme: "https", Host: "music.163.com"}

// newAPIClient 构建一次接口请求的客户端：Chrome TLS 指纹、共享 CookieJar
// （登录态与刷新 Cookie 经 Jar 持久化）、统一超时（弱网不悬挂）。
func (c *Client) newAPIClient() *req.Client {
	return req.C().
		SetCookieJar(c.jar).
		SetTimeout(15 * time.Second).
		SetTLSFingerprintChrome()
}

// jarCookie 从共享 Jar 读取 Cookie 值，缺失时返回 fallback。
func (c *Client) jarCookie(name, fallback string) string {
	return util.CookieValueByName(c.jar.Cookies(musicURL), name, fallback)
}

// post 发送加密表单 POST 并读取响应：从响应体解析业务 code（缺失按 200），
// zlib 响应体自动解压（沿用库行为）；非 JSON 响应（如风控拦截页）报错并附片段。
func (c *Client) post(ua, rawURL string, form map[string]string, cookies []*http.Cookie) (float64, []byte, error) {
	r := c.newAPIClient().R().
		SetHeader("User-Agent", ua).
		SetHeader("Referer", "https://music.163.com").
		SetFormData(form)
	if len(cookies) > 0 {
		r.SetCookies(cookies...)
	}
	resp, err := r.Post(rawURL)
	if err != nil {
		return 0, nil, err
	}
	body := resp.Bytes()
	if zr, zerr := zlib.NewReader(bytes.NewReader(body)); zerr == nil {
		if out, rerr := io.ReadAll(zr); rerr == nil {
			body = out
		}
	}
	var probe struct {
		Code *float64 `json:"code"`
	}
	if err := json.Unmarshal(body, &probe); err != nil {
		detail := string(bytes.TrimSpace(body))
		if len(detail) > 200 {
			detail = detail[:200] + "…"
		}
		return 0, nil, fmt.Errorf("响应不是 JSON（可能被风控拦截）: %s", detail)
	}
	code := float64(200)
	if probe.Code != nil {
		code = *probe.Code
	}
	return code, body, nil
}

// baseCookies 仿库的通用请求 Cookie（__remember_me / os / appver）。
func (c *Client) baseCookies() []*http.Cookie {
	osName := c.jarCookie("os", "ios")
	appver := iosAppVersion
	if osName == "pc" {
		appver = ""
	}
	return []*http.Cookie{
		{Name: "__remember_me", Value: "true"},
		{Name: "os", Value: osName},
		{Name: "appver", Value: c.jarCookie("appver", appver)},
	}
}

// doWeapi 以网页端加密方式（WEAPI）请求 https://music.163.com/weapi/<path>。
func (c *Client) doWeapi(path string, data map[string]string) (float64, []byte, error) {
	if data == nil {
		data = map[string]string{}
	}
	data["csrf_token"] = c.jarCookie("__csrf", "")
	return c.post(pcUA, "https://music.163.com/weapi/"+path, util.Weapi(data), c.baseCookies())
}

// doLinuxapi 以 Linux 客户端加密方式请求（统一经 /api/linux/forward 转发）。
func (c *Client) doLinuxapi(apiURL string, data map[string]string) (float64, []byte, error) {
	form := util.Linuxapi(map[string]interface{}{
		"method": "POST",
		"url":    apiURL,
		"params": data,
	})
	return c.post(linuxUA, "https://music.163.com/api/linux/forward", form, c.baseCookies())
}

// doEapi 以官方客户端加密方式（EAPI）请求 fullURL；apiPath 参与签名。
// header 字段按官方客户端约定以 Cookie 形式随请求发送。
func (c *Client) doEapi(fullURL, apiPath string, data map[string]string) (float64, []byte, error) {
	osName := c.jarCookie("os", "ios")
	appver := iosAppVersion
	if osName == "pc" {
		appver = ""
	}
	header := map[string]string{
		"osver":       c.jarCookie("osver", "17.4.1"),
		"deviceId":    c.jarCookie("deviceId", c.deviceId),
		"appver":      c.jarCookie("appver", appver),
		"versioncode": c.jarCookie("versioncode", "140"),
		"mobilename":  c.jarCookie("mobilename", ""),
		"buildver":    c.jarCookie("buildver", strconv.FormatInt(time.Now().Unix(), 10)),
		"resolution":  c.jarCookie("resolution", "1920x1080"),
		"__csrf":      c.jarCookie("__csrf", ""),
		"os":          osName,
		"channel":     c.jarCookie("channel", ""),
		"requestId":   strconv.FormatInt(time.Now().UnixMilli(), 10) + fmt.Sprintf("%03d", rand.IntN(1000)),
	}
	if v := c.jarCookie("MUSIC_U", ""); v != "" {
		header["MUSIC_U"] = v
	}
	if v := c.jarCookie("MUSIC_A", ""); v != "" {
		header["MUSIC_A"] = v
	}
	eapiData := make(map[string]interface{}, len(data)+1)
	for k, v := range data {
		eapiData[k] = v
	}
	eapiData["header"] = header
	form := util.Eapi(apiPath, eapiData)

	cookies := c.baseCookies()
	for k, v := range header {
		cookies = append(cookies, &http.Cookie{Name: k, Value: v, Path: "/"})
	}
	return c.post(pcUA, fullURL, form, cookies)
}
