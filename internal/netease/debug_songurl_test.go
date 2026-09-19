package netease_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/go-musicfox/netease-music/service"

	"mountain-air/internal/config"
	"mountain-air/internal/netease"
)

// weapiSongURL 通过 WEAPI（网页试听通道）请求播放地址，用于与 EAPI 结果对比。
func weapiSongURL(id int64, level string) (*netease.SongURL, error) {
	s := &service.SongUrlV1Service{
		ID:    strconv.FormatInt(id, 10),
		Level: service.SongQualityLevel(level),
	}
	code, body, err := s.SongUrl()
	if err != nil {
		return nil, err
	}
	if code != 200 {
		return nil, fmt.Errorf("weapi code=%v", code)
	}
	var resp struct {
		Data []struct {
			URL   string `json:"url"`
			Level string `json:"level"`
			Size  int64  `json:"size"`
			Type  string `json:"type"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	if len(resp.Data) == 0 || resp.Data[0].URL == "" {
		return nil, fmt.Errorf("weapi 无可用地址")
	}
	d := resp.Data[0]
	return &netease.SongURL{URL: d.URL, Level: d.Level, Size: d.Size, Type: d.Type}, nil
}

// 手动调试测试：使用本机已持久化的登录 Cookie，验证指定歌曲能否真正拿到 Hi-Res。
// 不会默认运行，需要显式指定环境变量：
//
//	SONG_ID=歌曲ID go test ./internal/netease -run TestDebugSongURL -v
//
// 不传 SONG_ID 时，自动扫描当前账号歌单中的歌曲（默认最多前 10 首）：
//
//	DEBUG_LIMIT=20 go test ./internal/netease -run TestDebugSongURL -v
//
// 未设置环境变量时自动跳过，不参与常规测试。
//
// 注意：部分歌曲的 SQ 与 Hi-Res 是同一个文件（母带本身就是 24bit），
// 此时两种音质的返回大小一致属正常现象，以 level 字段为准。
func TestDebugSongURL(t *testing.T) {
	// 该测试访问真实接口并使用本机登录态，仅按需手动运行。
	if os.Getenv("SONG_ID") == "" && os.Getenv("DEBUG_LIMIT") == "" {
		t.Skip("手动调试测试：请设置 SONG_ID 或 DEBUG_LIMIT 后运行")
	}
	dirs := config.DefaultDirs()
	client, err := netease.NewClient(filepath.Join(dirs.Data, "cookies"))
	if err != nil {
		t.Fatal(err)
	}
	if err := client.RefreshLogin(); err != nil {
		t.Logf("刷新登录态失败（继续尝试）: %v", err)
	}
	acc, err := client.AccountInfo()
	if err != nil {
		t.Fatal(err)
	}
	if acc == nil {
		t.Skip("本机没有可用登录态，请先运行程序完成扫码登录")
	}
	t.Logf("账号: %s (uid=%d)", acc.Nickname, acc.ID)

	// 收集待测歌曲：优先使用 SONG_ID，否则从歌单中取。
	var songs []netease.Song
	if idStr := os.Getenv("SONG_ID"); idStr != "" {
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			t.Fatalf("SONG_ID 无效: %v", err)
		}
		songs = []netease.Song{{ID: id, Name: "(指定歌曲)"}}
	} else {
		limit := 10
		if s := os.Getenv("DEBUG_LIMIT"); s != "" {
			if n, err := strconv.Atoi(s); err == nil && n > 0 {
				limit = n
			}
		}
		playlists, err := client.UserPlaylists(acc.ID)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("共 %d 个歌单，扫描前 %d 首歌...", len(playlists), limit)
		coverCount := 0
		for _, p := range playlists {
			if len(songs) >= limit {
				break
			}
			list, err := client.PlaylistSongs(p.ID)
			if err != nil {
				t.Logf("跳过歌单 %s: %v", p.Name, err)
				continue
			}
			for _, s := range list {
				if len(songs) >= limit {
					break
				}
				if s.CoverURL != "" {
					coverCount++
				}
				songs = append(songs, s)
			}
		}
		t.Logf("已取 %d 首歌，其中 %d 首带封面 URL", len(songs), coverCount)
	}

	hiresFound := 0
	for _, s := range songs {
		hr, hrErr := client.SongURL(s.ID, "hires")
		time.Sleep(300 * time.Millisecond) // 降低请求频率，避免触发风控
		sq, sqErr := client.SongURL(s.ID, "lossless")
		time.Sleep(300 * time.Millisecond)
		wh, whErr := weapiSongURL(s.ID, "hires")
		time.Sleep(300 * time.Millisecond)

		name := fmt.Sprintf("%d %s - %s", s.ID, s.Name, s.Artists)
		t.Log(name)
		if hrErr != nil {
			t.Logf("  eapi hires:    %v", hrErr)
		} else {
			t.Logf("  eapi hires:    level=%-8s size=%12d type=%s", hr.Level, hr.Size, hr.Type)
		}
		if sqErr != nil {
			t.Logf("  eapi lossless: %v", sqErr)
		} else {
			t.Logf("  eapi lossless: level=%-8s size=%12d type=%s", sq.Level, sq.Size, sq.Type)
		}
		if whErr != nil {
			t.Logf("  weapi hires:   %v", whErr)
		} else {
			t.Logf("  weapi hires:   level=%-8s size=%12d type=%s", wh.Level, wh.Size, wh.Type)
		}
		if hrErr == nil && hr.Level == "hires" {
			hiresFound++
			t.Log("  => 拿到 Hi-Res")
		} else if hrErr == nil {
			t.Logf("  => 该歌最高仅 %s", hr.Level)
		}
	}
	t.Logf("测试完成: %d 首歌曲中 %d 首拿到 Hi-Res", len(songs), hiresFound)
}
