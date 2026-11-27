package netease

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestRequestError(t *testing.T) {
	// 业务错误：优先带出服务端的 message，没有 message 时带出响应体片段。
	e := requestError("获取歌曲播放地址", -460, []byte(`{"code":-460,"message":"检测到您的网络环境存在风险，请稍后再试"}`))
	if got := e.Error(); got != "获取歌曲播放地址: code=-460，检测到您的网络环境存在风险，请稍后再试" {
		t.Fatalf("应直接给出服务端提示: %q", got)
	}
	e = requestError("获取歌单", 404, []byte(`{"msg":"not found"}`))
	if !strings.Contains(e.Error(), "code=404") || !strings.Contains(e.Error(), "not found") {
		t.Fatalf("业务错误应带 code 与响应体: %v", e)
	}
	if !IsStatusError(e) {
		t.Fatal("业务错误应可被识别为服务端状态")
	}
	// 过长的响应体截断，避免错误提示刷屏。
	e = requestError("获取歌单", 500, bytes.Repeat([]byte("x"), 500))
	if !strings.HasSuffix(e.Error(), "…") || len(e.Error()) > 260 {
		t.Fatalf("响应体应截断: 长度 %d", len(e.Error()))
	}
	// 空 body：仅 code。
	e = requestError("获取歌单", 500, nil)
	if !strings.Contains(e.Error(), "code=500") || strings.Contains(e.Error(), "body=") {
		t.Fatalf("空 body 应仅带 code: %v", e)
	}
	// 传输失败与未登录不是服务端状态。
	if IsStatusError(errors.New("x（网络传输错误）: timeout")) || IsStatusError(ErrNotLoggedIn) {
		t.Fatal("传输错误不应被当作业务状态")
	}
}

func TestNormalizeQuality(t *testing.T) {
	if got := NormalizeQuality("hires"); got != "hires" {
		t.Errorf("NormalizeQuality(hires) = %q", got)
	}
	if got := NormalizeQuality("bogus"); got != DefaultQuality {
		t.Errorf("NormalizeQuality(bogus) = %q, 期望 %q", got, DefaultQuality)
	}
}
