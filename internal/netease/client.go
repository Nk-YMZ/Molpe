// Package netease 封装对网易云音乐接口的访问。
//
// 全部接口走 EAPI（官方客户端通道），并以固定的 PC 客户端身份请求。加解密、
// 会话与传输层为自研实现（仅标准库）；传输使用 req/v3 模拟 Chrome TLS 指纹，
// 以通过服务端对握手特征的风控识别。
package netease

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"maps"
	"math/rand/v2"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/imroc/req/v3"
)

// PC 客户端档案。os/appver/osver/channel 与 User-Agent 必须是一套真实存在的
// 客户端组合，实测（2026-11）：头字段残缺（如空 appver）时 interface 域名认不出
// 登录态，且播放地址的 Hi-Res 被标成 lossless；os=pc 配手机端 UA 取播放地址
// 稳定返回 -460（网络环境存在风险）。
const (
	deviceOS      = "pc"
	deviceAppver  = "3.1.29.205117"
	deviceOsver   = "Microsoft-Windows-10-Professional-build-19045-64bit"
	deviceChannel = "netease"
	deviceUA      = "Mozilla/5.0 (Windows NT 10.0; WOW64) AppleWebKit/537.36 (KHTML, like Gecko) Safari/537.36 Chrome/91.0.4472.164 NeteaseMusicDesktop/3.1.29.205117"
)

// eapiHosts 是 EAPI 入口地址，按优先级排列（四个入口实测等价）。某个入口传输
// 失败或返回非 JSON（如网关 502 页面）时顺延到下一个，成功后保持使用该入口。
var eapiHosts = []string{
	"https://interfacepc.music.163.com",
	"https://interface3.music.163.com",
	"https://interface.music.163.com",
	"https://music.163.com",
}

// requestTimeout 是一次接口调用（含入口顺延）的总时限，弱网下不悬挂。
const requestTimeout = 15 * time.Second

// Client 以 PC 客户端身份访问网易云接口，可在多个 goroutine 中并发使用。
type Client struct {
	path  string // 会话文件
	mu    sync.Mutex
	sess  session
	http  *req.Client
	hosts []string // 接口入口地址（含协议）
	host  int      // 当前使用的入口下标
}

// NewClient 创建客户端：加载会话文件（不存在则新建），首次升级时从旧版
// cookies 文件迁移登录凭据。没有登录凭据时同样创建成功，只是处于未登录态。
func NewClient(sessionFile string) (*Client, error) {
	c := &Client{
		path:  sessionFile,
		http:  req.C().SetTLSFingerprintChrome(),
		hosts: eapiHosts,
	}
	c.sess = loadSession(sessionFile)
	if c.sess.DeviceID == "" {
		c.sess.DeviceID = newDeviceID()
	}
	migrated := !c.sess.logged() && c.importLegacy()
	if err := saveSession(sessionFile, c.sess); err != nil {
		return nil, err
	}
	if migrated {
		_ = os.Remove(filepath.Join(filepath.Dir(sessionFile), legacyCookieFile))
		log.Printf("网易云: 已从旧版 cookies 文件迁移登录凭据")
	}
	return c, nil
}

// SetEndpoints 替换接口入口地址（含协议），供测试注入本地服务。
func (c *Client) SetEndpoints(urls []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.hosts, c.host = slices.Clone(urls), 0
}

// LoggedIn 报告本地是否存有登录凭据。
func (c *Client) LoggedIn() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sess.logged()
}

// SessionInfo 返回当前登录凭据的时间概况，供日志诊断（不含凭据内容）。
func (c *Client) SessionInfo() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sess.describe()
}

// call 发送一次 EAPI 请求，返回业务状态码与响应体。
//
// 返回的 error 只表示请求没能完成（带「网络传输错误」字样）；服务端的业务
// 状态码通过 code 交给调用方判断。响应下发的 MUSIC_U/__csrf/NMTID 会并入会话。
func (c *Client) call(op, apiPath string, data map[string]any) (float64, []byte, error) {
	form, cookie, err := c.prepare(apiPath, data)
	if err != nil {
		return 0, nil, fmt.Errorf("%s: %w", op, err)
	}
	c.mu.Lock()
	start, hosts := c.host, c.hosts
	c.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	order := append(slices.Clone(hosts[start:]), hosts[:start]...)
	var lastErr error
	for i, host := range order {
		resp, err := c.http.R().SetContext(ctx).
			SetHeader("User-Agent", deviceUA).
			SetHeader("Cookie", cookie).
			SetFormData(form).
			Post(host + "/eapi/" + strings.TrimPrefix(apiPath, "/api/"))
		if err == nil && resp.StatusCode != http.StatusOK {
			err = fmt.Errorf("HTTP %d", resp.StatusCode)
		}
		if err == nil && !json.Valid(resp.Bytes()) {
			err = errors.New("响应不是 JSON")
		}
		if err == nil {
			c.mu.Lock()
			c.host = (start + i) % len(hosts)
			c.mu.Unlock()
			body := resp.Bytes()
			c.absorb(resp.Header.Values("Set-Cookie"))
			code := 200.0 // 响应没有 code 字段时按成功处理
			var head struct {
				Code *float64 `json:"code"`
			}
			_ = json.Unmarshal(body, &head)
			if head.Code != nil {
				code = *head.Code
			}
			return code, body, nil
		}
		lastErr = err
		if i < len(order)-1 {
			log.Printf("网易云: %s 在 %s 失败: %v，切换入口", op, host, err)
		}
	}
	return 0, nil, fmt.Errorf("%s（网络传输错误）: %w", op, lastErr)
}

// prepare 生成一次请求的密文与 Cookie 头：头字段按官方 PC 客户端固定，
// 凭据与会话标识（MUSIC_U、NMTID、__csrf）取自当前会话。
func (c *Client) prepare(apiPath string, data map[string]any) (form map[string]string, cookie string, err error) {
	if data == nil {
		data = map[string]any{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	header := map[string]string{
		"osver":       deviceOsver,
		"deviceId":    c.sess.DeviceID,
		"os":          deviceOS,
		"appver":      deviceAppver,
		"versioncode": "140",
		"mobilename":  "",
		"buildver":    strconv.FormatInt(now.Unix(), 10),
		"resolution":  "1920x1080",
		"__csrf":      c.sess.CSRF,
		"channel":     deviceChannel,
		"requestId":   fmt.Sprintf("%d_%04d", now.UnixMilli(), rand.IntN(1000)),
	}
	if c.sess.MusicU != "" {
		header["MUSIC_U"] = c.sess.MusicU
	}
	if c.sess.NMTID != "" {
		header["NMTID"] = c.sess.NMTID
	}
	data["e_r"] = false // 不要求服务端加密响应体
	data["header"] = header
	form, err = eapi(apiPath, data)
	return form, cookieHeader(header), err
}

// cookieHeader 把头字段按键名排序拼成 Cookie 串。凭据 MUSIC_U 与会话标识 NMTID
// 因大写排在最前：实测空值字段（如空 appver）排在 MUSIC_U 之前时，interface
// 域名会认不出登录态。
func cookieHeader(h map[string]string) string {
	var b strings.Builder
	for i, k := range slices.Sorted(maps.Keys(h)) {
		if i > 0 {
			b.WriteString("; ")
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(h[k])
	}
	return b.String()
}

// absorb 并入服务端下发的凭据类 Cookie 并立即落盘。续期成功会换发新的 MUSIC_U，
// 新值必须马上保存，否则下次启动就是未登录态。
func (c *Client) absorb(setCookies []string) {
	if len(setCookies) == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now, changed := time.Now(), false
	for _, raw := range setCookies {
		ck, err := http.ParseSetCookie(raw)
		if err != nil {
			continue
		}
		var dst *string
		switch ck.Name {
		case "MUSIC_U":
			dst = &c.sess.MusicU
		case "__csrf":
			dst = &c.sess.CSRF
		case "NMTID":
			dst = &c.sess.NMTID
		default:
			continue
		}
		value := ck.Value
		if ck.MaxAge < 0 {
			value = "" // 服务端清除（如退出登录）
		}
		if *dst == value {
			continue
		}
		if dst == &c.sess.MusicU && value != "" {
			c.sess.IssuedAt, c.sess.RenewCheckedAt = now, now // 登录或续期换发：一个间隔内不再续期
		}
		*dst = value
		changed = true
	}
	if changed {
		if err := saveSession(c.path, c.sess); err != nil {
			log.Printf("网易云: 保存登录会话失败: %v", err)
		}
	}
}
