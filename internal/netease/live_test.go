package netease_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"molpe/internal/netease"
)

// 手动联调测试：用本机真实登录态把木末用到的接口全部走一遍（访问真实接口）。
//
//	MOLPE_LIVE_SESSION=~/.local/share/molpe/session.json \
//		go test ./internal/netease -run TestLive -v
//
// 也可以指向旧版的 cookies 文件，顺带验证迁移。未设置环境变量时自动跳过。
// 会话文件会先复制到临时目录，不会改动原文件，也不会触发续期。
func TestLive(t *testing.T) {
	src := os.Getenv("MOLPE_LIVE_SESSION")
	if src == "" {
		t.Skip("手动联调测试：请设置 MOLPE_LIVE_SESSION 后运行")
	}
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, filepath.Base(src)), data, 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := netease.NewClient(filepath.Join(dir, "session.json"))
	if err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	acc, err := c.AccountInfo()
	if err != nil || acc == nil {
		t.Fatalf("账号信息：acc=%v err=%v", acc, err)
	}
	playlists, err := c.UserPlaylists(acc.ID)
	if err != nil || len(playlists) == 0 {
		t.Fatalf("歌单列表：%d 个，err=%v", len(playlists), err)
	}
	t.Logf("账号信息与 %d 个歌单正常，耗时 %s", len(playlists), time.Since(start).Round(time.Millisecond))

	for _, p := range playlists[:min(2, len(playlists))] {
		songs, err := c.PlaylistSongs(p.ID)
		if err != nil {
			t.Fatalf("歌单 %d：%v", p.ID, err)
		}
		if len(songs) != p.TrackCount {
			t.Errorf("歌单 %d：取到 %d 首，歌单共 %d 首", p.ID, len(songs), p.TrackCount)
		}
		t.Logf("歌单 %d：%d 首，首曲「%s - %s」%s 封面 %s",
			p.ID, len(songs), songs[0].Name, songs[0].Artists, songs[0].Duration, songs[0].CoverURL)
	}

	first := playlists[0]
	songs, err := c.PlaylistSongs(first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if lines, err := c.Lyrics(songs[0].ID, true); err != nil || len(lines) == 0 {
		t.Errorf("歌词 %d：%d 行，err=%v", songs[0].ID, len(lines), err)
	}
	for _, s := range songs[:min(5, len(songs))] {
		u, err := c.SongURL(s.ID, "hires")
		if err != nil {
			t.Errorf("播放地址 %d：%v", s.ID, err)
			continue
		}
		t.Logf("播放地址 %d：level=%s type=%s size=%d", s.ID, u.Level, u.Type, u.Size)
		time.Sleep(300 * time.Millisecond)
	}
}
