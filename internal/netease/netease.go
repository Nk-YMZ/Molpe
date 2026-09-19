// Package netease 封装对网易云音乐接口的访问，
// 统一使用 github.com/go-musicfox/netease-music。
package netease

import (
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
