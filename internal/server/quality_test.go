package server

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"molpe/internal/config"
	"molpe/internal/ipc"
	"molpe/internal/netease"
	"molpe/internal/player"
	"molpe/internal/queue"
)

func qualityServer(t *testing.T) *Server {
	t.Helper()
	return &Server{
		cfg:     config.DefaultConfig(),
		dirs:    config.Dirs{Config: t.TempDir()},
		quality: "lossless",
		queue:   queue.New(nil, queue.ModeLoop, queue.Options{}),
	}
}

func TestQualitySave(t *testing.T) {
	s := qualityServer(t)
	for _, level := range netease.QualityLevels() {
		s.handleMessage(ipc.NewMessage(0, ipc.TSetQuality, ipc.QualityCmd{Quality: level}))
		cfg, err := config.LoadConfig(s.dirs.Config)
		if err != nil || cfg.Quality != level || s.state().Quality != level {
			t.Fatalf("音质未同步到配置及快照: %q, %v", level, err)
		}
	}
	for _, level := range []string{"", "invalid", "HIRES"} {
		s.setQuality(level)
		if s.quality != "hires" || s.cfg.Quality != "hires" {
			t.Fatal("非法音质不应改变偏好")
		}
	}
}

func TestQualitySaveError(t *testing.T) {
	s := qualityServer(t)
	s.dirs.Config = filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(s.dirs.Config, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	s.setQuality("hires")
	if s.quality != "lossless" || s.cfg.Quality != "lossless" {
		t.Fatal("保存失败时不应改变正在使用的音质")
	}
	s.cfgErr = errors.New("配置损坏")
	s.dirs.Config = t.TempDir()
	s.setQuality("hires")
	if s.quality != "lossless" {
		t.Fatal("不应覆盖损坏的配置")
	}
}

func TestQualityReload(t *testing.T) {
	for _, paused := range []bool{true, false} {
		s := qualityServer(t)
		client, err := netease.NewClient(filepath.Join(t.TempDir(), "cookies"))
		if err != nil {
			t.Fatal(err)
		}
		s.client, s.results = client, make(chan any, 8)
		song := netease.Song{ID: 1, Name: "测试曲", Duration: 100 * time.Second}
		s.queue.SetPlaylist([]netease.Song{song})
		s.queue.Play(song)
		before := s.queue.Snapshot()
		s.playing = &ipc.Playing{Song: song, Level: "lossless"}
		s.paused = paused
		s.anchor.set(37, paused)
		s.setQuality("hires")
		if s.startOffset < 37*time.Second || s.startOffset > 38*time.Second || s.paused != paused || !s.loading {
			t.Fatalf("切换应保留进度与暂停意图: offset=%v paused=%v", s.startOffset, s.paused)
		}
		offset, oldSeq := s.startOffset, s.playSeq
		s.setQuality("exhigh")
		if s.startOffset != offset || s.startOffsetSeq != s.playSeq || s.playSeq <= oldSeq {
			t.Fatal("连续切换应保留原定位并作废旧请求")
		}
		s.handleSongURL(songURLResult{seq: oldSeq, err: errors.New("过期结果")})
		if !s.loading || s.quality != "exhigh" || s.playing.Level != "" {
			t.Fatal("旧请求不得改变新音质或伪造实际等级")
		}
		if !reflect.DeepEqual(before, s.queue.Snapshot()) {
			t.Fatal("切换音质不应改变队列或历史")
		}
	}
}

func TestQualityGap(t *testing.T) {
	s := qualityServer(t)
	song := netease.Song{ID: 1, Duration: time.Minute}
	s.gapSong, s.playing, s.paused = &song, &ipc.Playing{Song: song}, true
	s.setQuality("hires")
	if s.gapSong == nil || s.loading || !s.paused || s.quality != "hires" {
		t.Fatal("切换音质应保留歌曲间隔，待到期后按新音质播放")
	}
}

// 模拟接口返回音频地址，验证新流定位、暂停和实际音质回填。
func TestQualityStream(t *testing.T) {
	if _, err := exec.LookPath("mpv"); err != nil {
		t.Skip("未安装 mpv，跳过音质切换集成测试")
	}
	s := qualityServer(t)
	client, err := netease.NewClient(filepath.Join(t.TempDir(), "cookies"))
	if err != nil {
		t.Fatal(err)
	}
	p, err := player.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	s.client, s.results, s.player = client, make(chan any, 8), p
	s.playing = &ipc.Playing{Song: netease.Song{ID: 1, Duration: 100 * time.Second}, Level: "lossless"}
	s.paused = true
	s.anchor.set(37, true)
	s.setQuality("hires")
	s.handleSongURL(songURLResult{
		seq:  s.playSeq,
		song: s.playing.Song,
		url:  &netease.SongURL{URL: "av://lavfi:sine=frequency=440:duration=100", Level: "lossless"},
	})
	time.Sleep(500 * time.Millisecond)
	pos, err := p.Position()
	if err != nil || pos < 36.5 || pos > 37.5 {
		t.Fatalf("新流应保持在 37s: %.3f, %v", pos, err)
	}
	time.Sleep(300 * time.Millisecond)
	pos2, err := p.Position()
	if err != nil || pos2-pos > .1 || !s.paused || s.loading {
		t.Fatal("暂停中的音质切换不应自行播放")
	}
	if s.state().Quality != "hires" || s.playing.Level != "lossless" {
		t.Fatal("应保留 Hi-Res 偏好，同时报告接口返回的无损等级")
	}
}
