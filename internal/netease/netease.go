// Package netease 封装对网易云音乐接口的访问，
// 统一使用 github.com/go-musicfox/netease-music。
package netease

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/go-musicfox/netease-music/util"
	"github.com/telanflow/cookiejar"
)

// Client 持有访问网易云接口所需的会话状态。
type Client struct {
	jar http.CookieJar
}

// NewClient 创建客户端，Cookie 持久化到 cookieFile，
// 并注册为 netease-music 的全局 Jar，后续所有接口请求共用该会话。
func NewClient(cookieFile string) (*Client, error) {
	// netease-music 的请求默认无超时，弱网/网络异常时会永久悬挂，
	// 这里统一设置上限，卡住的操作最终会以错误形式回到界面。
	util.HTTPClientTimeout = 15 * time.Second
	if err := os.MkdirAll(filepath.Dir(cookieFile), 0o700); err != nil {
		return nil, fmt.Errorf("创建数据目录失败: %w", err)
	}
	// cookiejar 新建文件时使用默认权限，这里先以 0600 创建，保护登录凭据。
	f, err := os.OpenFile(cookieFile, os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("创建 Cookie 文件失败: %w", err)
	}
	if err := f.Close(); err != nil {
		return nil, fmt.Errorf("创建 Cookie 文件失败: %w", err)
	}

	jar, err := cookiejar.NewFileJar(cookieFile, nil)
	if err != nil {
		return nil, fmt.Errorf("加载 Cookie 失败: %w", err)
	}
	util.SetGlobalCookieJar(jar)
	return &Client{jar: jar}, nil
}

// requestError 构造接口请求错误。code==520 是 netease-music 库对传输层
// 失败（DNS/连接/超时等）自造的哨兵码，真实原因在响应体中；
// 其余非 200 为服务端业务响应。诊断弱网/风控问题依赖 body 中的细节。
func requestError(op string, code float64, body []byte) error {
	detail := string(bytes.TrimSpace(body))
	if len(detail) > 200 {
		detail = detail[:200] + "…"
	}
	if code == 520 {
		return fmt.Errorf("%s（网络传输错误）: %s", op, detail)
	}
	if detail == "" {
		return fmt.Errorf("%s: code=%v", op, code)
	}
	return fmt.Errorf("%s: code=%v body=%s", op, code, detail)
}
