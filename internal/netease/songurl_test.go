package netease

import (
	"strings"
	"testing"
)

func TestRequestError(t *testing.T) {
	// 520 哨兵码：归类为传输错误并带出响应体中的真实原因。
	e := requestError("获取播放地址", 520, []byte(`Post "https://x": dial tcp: i/o timeout`))
	if !strings.Contains(e.Error(), "网络传输错误") || !strings.Contains(e.Error(), "i/o timeout") {
		t.Fatalf("520 应归类为传输错误并带出原因: %v", e)
	}
	// 业务错误：带 code 与 body。
	e = requestError("获取歌单", 404, []byte(`{"msg":"not found"}`))
	if !strings.Contains(e.Error(), "code=404") || !strings.Contains(e.Error(), "not found") {
		t.Fatalf("业务错误应带 code 与 body: %v", e)
	}
	// 空 body：仅 code。
	e = requestError("获取歌单", 500, nil)
	if !strings.Contains(e.Error(), "code=500") {
		t.Fatalf("空 body 应仅带 code: %v", e)
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
