package netease

import (
	"crypto/aes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

// eapiKey 是官方客户端接口（EAPI）的 AES-128-ECB 密钥。
var eapiKey = []byte("e82ckenh8dichen8")

// eapi 按官方客户端方式加密一次请求：明文为
// 「接口路径-36cd479b6b5-参数 JSON-36cd479b6b5-摘要」，经 AES-ECB 加密后
// 输出大写十六进制。摘要是不带盐的同一段内容的 MD5，服务端据此校验完整性。
func eapi(apiPath string, data map[string]any) (map[string]string, error) {
	text, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("序列化请求参数失败: %w", err)
	}
	sum := md5.Sum([]byte("nobody" + apiPath + "use" + string(text) + "md5forencrypt"))
	plain := apiPath + "-36cd479b6b5-" + string(text) + "-36cd479b6b5-" + hex.EncodeToString(sum[:])
	ct, err := aesECBEncrypt([]byte(plain), eapiKey)
	if err != nil {
		return nil, fmt.Errorf("加密请求参数失败: %w", err)
	}
	return map[string]string{"params": strings.ToUpper(hex.EncodeToString(ct))}, nil
}

// aesECBEncrypt 以 PKCS#7 填充后逐块加密（标准库未提供 ECB 模式）。
func aesECBEncrypt(plain, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	pad := aes.BlockSize - len(plain)%aes.BlockSize
	buf := make([]byte, len(plain)+pad)
	copy(buf, plain)
	for i := len(plain); i < len(buf); i++ {
		buf[i] = byte(pad)
	}
	out := make([]byte, len(buf))
	for i := 0; i < len(buf); i += aes.BlockSize {
		block.Encrypt(out[i:i+aes.BlockSize], buf[i:i+aes.BlockSize])
	}
	return out, nil
}
