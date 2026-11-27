package netease

import (
	crand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	// legacyCookieFile 是旧版保存 Cookie 的 Netscape 格式文件名，首次启动迁移后删除。
	legacyCookieFile = "cookies"
	// renewInterval 是两次续期检查的最小间隔：服务端只对签发较久的凭据换发新凭据。
	renewInterval = 24 * time.Hour
)

// session 是持久化的登录凭据与设备身份，保存为数据目录下的 session.json。
//
// 只保存 MUSIC_U（登录凭据）和官方客户端随请求携带的 __csrf、NMTID。DeviceID
// 首次生成后不变：服务端按「设备 ID + 系统类型」登记设备，每次启动都换 ID
// 等于同一账号总在换设备。
type session struct {
	MusicU   string `json:"music_u,omitempty"`
	CSRF     string `json:"csrf,omitempty"`
	NMTID    string `json:"nmtid,omitempty"`
	DeviceID string `json:"device_id"`

	IssuedAt       time.Time `json:"issued_at,omitzero"`        // 当前 MUSIC_U 的签发时间（登录或续期换发）
	RenewCheckedAt time.Time `json:"renew_checked_at,omitzero"` // 最近一次续期调用的时间
}

func (s session) logged() bool { return s.MusicU != "" }

// describe 返回凭据的时间概况，供日志诊断（不含凭据内容）。
func (s session) describe() string {
	ago := func(t time.Time) string {
		switch d := time.Since(t); {
		case t.IsZero():
			return "从未"
		case d < 24*time.Hour:
			return fmt.Sprintf("%.0f 小时前", d.Hours())
		default:
			return fmt.Sprintf("%.0f 天前", d.Hours()/24)
		}
	}
	return fmt.Sprintf("凭据签发于%s，上次续期检查于%s", ago(s.IssuedAt), ago(s.RenewCheckedAt))
}

// newDeviceID 生成 52 位大写十六进制设备 ID（官方客户端的设备 ID 形态）。
func newDeviceID() string {
	b := make([]byte, 26)
	_, _ = crand.Read(b)
	return strings.ToUpper(hex.EncodeToString(b))
}

// loadSession 读取会话文件，不存在时返回空会话；损坏的文件原样改名留存，
// 同样按未登录处理。
func loadSession(path string) session {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return session{}
	}
	var s session
	if err == nil {
		err = json.Unmarshal(data, &s)
	}
	if err != nil {
		_ = os.Rename(path, path+".bad")
		log.Printf("网易云: 登录会话文件无法读取（%v），已备份为 %s，需要重新登录",
			err, filepath.Base(path)+".bad")
		return session{}
	}
	return s
}

// saveSession 原子写入会话文件：先写同目录临时文件（0600）再改名，
// 避免写到一半退出留下损坏的凭据文件。
func saveSession(path string, s session) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("创建数据目录失败: %w", err)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化登录会话失败: %w", err)
	}
	f, err := os.CreateTemp(dir, ".session-*.tmp")
	if err != nil {
		return fmt.Errorf("创建临时文件失败: %w", err)
	}
	tmp := f.Name()
	defer os.Remove(tmp) // 改名成功后目标已不存在，失败时清理残留
	if _, err = f.Write(append(data, '\n')); err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		return fmt.Errorf("写入登录会话失败: %w", err)
	}
	return nil
}

// importLegacy 从旧版 Netscape 格式的 cookies 文件导入登录凭据（未过期的
// MUSIC_U 与 __csrf），返回是否导入成功；落盘与删除旧文件由调用方完成。
func (c *Client) importLegacy() bool {
	legacy := filepath.Join(filepath.Dir(c.path), legacyCookieFile)
	data, err := os.ReadFile(legacy)
	if err != nil {
		return false
	}
	var musicU, csrf string
	for _, line := range strings.Split(string(data), "\n") {
		// 以 #HttpOnly_ 开头的行也是条目，其余 # 行是注释。
		if strings.HasPrefix(line, "#") && !strings.HasPrefix(line, "#HttpOnly_") {
			continue
		}
		f := strings.Split(strings.TrimSuffix(line, "\r"), "\t")
		if len(f) < 7 {
			continue
		}
		if exp, err := strconv.ParseInt(f[4], 10, 64); err == nil && exp > 0 && exp <= time.Now().Unix() {
			continue
		}
		switch f[5] {
		case "MUSIC_U":
			musicU = f[6]
		case "__csrf":
			csrf = f[6]
		}
	}
	if musicU == "" {
		return false
	}
	c.sess.MusicU, c.sess.CSRF = musicU, csrf
	c.sess.IssuedAt = time.Now()
	if st, err := os.Stat(legacy); err == nil {
		c.sess.IssuedAt = st.ModTime() // 旧版每次启动都改写该文件，只能作为近似值
	}
	return true
}
