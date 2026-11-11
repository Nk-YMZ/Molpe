package ui

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"molpe/internal/ipc"
	"molpe/internal/netease"
)

func coverFixture() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := range 4 {
		for x := range 4 {
			c := color.RGBA{R: 255, A: 255}
			if y >= 2 {
				c = color.RGBA{B: 255, A: 255}
			}
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

func TestCoverPixels(t *testing.T) {
	out := renderCoverImage(coverFixture(), 2, 1, color.Black)
	if !strings.Contains(out, "38;2;255;0;0m") || !strings.Contains(out, "48;2;0;0;255m") || strip(out) != "▀▀" {
		t.Fatalf("半格颜色不符: %q", out)
	}
	transparent := image.NewRGBA(image.Rect(0, 0, 2, 2))
	out = renderCoverImage(transparent, 1, 1, color.RGBA{G: 255, A: 255})
	if !strings.Contains(out, "38;2;0;255;0m") {
		t.Fatal("透明封面应合成到主题背景")
	}
}

func TestCoverQuadrants(t *testing.T) {
	red, blue := color.RGBA{R: 255, A: 255}, color.RGBA{B: 255, A: 255}
	for mask := range 16 {
		var pixels [4]color.RGBA
		for i := range pixels {
			pixels[i] = blue
			if mask&(1<<i) != 0 {
				pixels[i] = red
			}
		}
		fg, bg, glyph := fitCoverCell(pixels)
		glyphMask := -1
		for i, symbol := range coverQuadrants {
			if symbol == glyph {
				glyphMask = i
				break
			}
		}
		if glyphMask < 0 {
			t.Fatalf("未知象限字符 %q", glyph)
		}
		for i, want := range pixels {
			got := bg
			if glyphMask&(1<<i) != 0 {
				got = fg
			}
			if got != want {
				t.Fatalf("象限 %04b 的像素 %d 不符: %v != %v", mask, i, got, want)
			}
		}
	}
}

func TestLargeCover(t *testing.T) {
	previous := playingLayout(120, 28, 5)
	larger := playingLayout(180, 50, 5)
	maximum := playingLayout(300, 80, 5)
	if previous.coverWidth < 30 || larger.coverWidth <= previous.coverWidth || maximum.coverWidth != 96 {
		t.Fatalf("大窗口应获得更多色块: %+v %+v %+v", previous, larger, maximum)
	}
	if larger.listWidth < 40 || larger.coverHeight*2 != larger.coverWidth {
		t.Fatal("高分辨率布局应保留列表空间和封面比例")
	}
}

func TestCoverCache(t *testing.T) {
	var data bytes.Buffer
	if err := png.Encode(&data, coverFixture()); err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_, _ = w.Write(data.Bytes())
	}))
	defer server.Close()
	cache := &coverCache{}
	for range 2 {
		img, err := cache.load(context.Background(), server.URL)
		if err != nil || img.Bounds().Dx() != 4 {
			t.Fatalf("封面读取失败: %v", err)
		}
	}
	if requests.Load() != 1 {
		t.Fatal("同一封面应复用缓存")
	}
	for _, suffix := range []string{"1", "2", "3", "4", "5", "6", "7", "8", "9"} {
		if _, err := cache.load(context.Background(), server.URL+"/"+suffix); err != nil {
			t.Fatal(err)
		}
	}
	if len(cache.images) != 8 || len(cache.order) != 8 {
		t.Fatal("内存缓存应有大小上限")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := cache.load(ctx, server.URL+"/canceled"); err == nil {
		t.Fatal("取消后不应继续下载")
	}
}

func TestCoverInvalid(t *testing.T) {
	if _, err := decodeCover([]byte("invalid")); err == nil {
		t.Fatal("应拒绝损坏图片")
	}
	var oversized bytes.Buffer
	_ = png.Encode(&oversized, image.NewRGBA(image.Rect(0, 0, 4097, 1)))
	if _, err := decodeCover(oversized.Bytes()); err == nil {
		t.Fatal("应限制解码尺寸")
	}
	cache := &coverCache{}
	if _, err := cache.load(context.Background(), "file:///tmp/image.png"); err == nil {
		t.Fatal("应拒绝非 HTTP 封面")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "8388609")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	if _, err := cache.load(context.Background(), server.URL); err == nil {
		t.Fatal("应限制下载大小")
	}
}

func TestCoverState(t *testing.T) {
	m := Model{page: pagePlaying, width: 80, height: 30, theme: DefaultTheme(), lyricGapBelow: 1, playbackGapBelow: 1}
	m.st.Playing = &ipc.Playing{Song: netease.Song{ID: 1, CoverURL: "https://example.test/a"}}
	_ = m.syncCover()
	oldSeq := m.cover.seq
	m.st.Playing = &ipc.Playing{Song: netease.Song{ID: 2, CoverURL: "https://example.test/b"}}
	_ = m.syncCover()
	next, _ := m.Update(coverLoadedMsg{url: "https://example.test/a", seq: oldSeq, image: coverFixture()})
	m = next.(Model)
	if m.cover.image != nil || m.cover.url != "https://example.test/b" {
		t.Fatal("旧封面不得覆盖新歌曲")
	}
	next, _ = m.Update(coverLoadedMsg{url: m.cover.url, seq: m.cover.seq, image: coverFixture()})
	m = next.(Model)
	if m.cover.art == "" {
		t.Fatal("应生成缓存色块")
	}
	cache := m.cover.art
	m.refreshCoverArt()
	if m.cover.art != cache {
		t.Fatal("同尺寸刷新应复用色块")
	}
	next, _ = m.Update(tea.WindowSizeMsg{Width: 40, Height: 15})
	m = next.(Model)
	if m.cover.art != "" {
		t.Fatal("窄窗口应回退到紧凑歌词布局")
	}
	next, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 35})
	m = next.(Model)
	if m.cover.art == "" {
		t.Fatal("放大窗口应复用已解码图片")
	}
	if m.cover.cancel != nil {
		m.cover.cancel()
	}
}

func TestPlayerLayout(t *testing.T) {
	for _, lyricLines := range []int{1, 5, 15} {
		for _, size := range [][2]int{{300, 90}, {180, 52}, {146, 41}, {120, 40}, {80, 24}, {60, 20}, {40, 14}, {20, 10}} {
			m := Model{page: pagePlaying, theme: DefaultTheme(), keys: defaultKeyMap(), width: size[0], height: size[1], lyricLines: lyricLines, lyricGapBelow: 1, playbackGapBelow: 1}
			m.st.Playing = &ipc.Playing{Song: netease.Song{ID: 1, Name: "当前歌曲", Artists: "演唱者", Album: "专辑名称"}}
			m.st.Paused = true
			m.st.Queue.Songs = []netease.Song{m.st.Playing.Song, {ID: 2, Name: "列表第二首", Artists: "列表歌手"}}
			m.syncPlayerList(nil, true)
			m.cover.image = coverFixture()
			m.refreshCoverArt()
			m.lyrics = []netease.LyricLine{{Text: "上一句歌词"}, {Text: "当前歌词"}, {Text: "下一句歌词"}}
			m.lyricCur = 1
			screen := strip(renderContent(m.buildViewState(), testStyles()))
			lines := strings.Split(screen, "\n")
			if len(lines) != size[1] {
				t.Fatalf("%v 高度不符: %d", size, len(lines))
			}
			for _, line := range lines {
				if ansi.StringWidth(line) > size[0] {
					t.Fatalf("%v 行溢出: %q", size, line)
				}
			}
			if size[0] >= 58 && m.playerHeight() >= 16 {
				lastCover, lyricRow, listRow := -1, -1, -1
				for row, line := range lines {
					if strings.Contains(line, "▀") {
						lastCover = row
					}
					if strings.Contains(line, "当前歌词") {
						lyricRow = row
					}
					if strings.Contains(line, "列表第二首") {
						listRow = row
						if !strings.Contains(line, "▀") || strings.Index(line, "列表第二首") < strings.LastIndex(line, "▀") {
							t.Fatalf("%v 应在封面右侧显示列表", size)
						}
					}
				}
				if lastCover < 0 || lyricRow <= lastCover || listRow < 0 {
					t.Fatalf("%v 应显示封面下方歌词和右侧列表，位置 %d %d %d", size, lastCover, lyricRow, listRow)
				}
			}
		}
	}
}

func TestPlayerNavigation(t *testing.T) {
	m := Model{page: pageSongs, keys: defaultKeyMap(), width: 80, height: 30, st: ipc.State{Playing: &ipc.Playing{}}}
	next, _ := m.handleKey(qualityKey('v'))
	m = next.(Model)
	if m.page != pagePlaying || m.playerReturn != pageSongs {
		t.Fatal("v 应进入播放页并记住浏览位置")
	}
	next, _ = m.handleKey(qualityKey(tea.KeyEscape))
	m = next.(Model)
	if m.page != pageSongs {
		t.Fatal("Esc 应返回歌曲列表")
	}
}

func TestPlayerListState(t *testing.T) {
	songs := []netease.Song{{ID: 1, Name: "第一首"}, {ID: 2, Name: "第二首"}, {ID: 3, Name: "第三首"}}
	st := ipc.State{Playing: &ipc.Playing{Song: songs[1]}, Paused: true}
	st.Queue.Songs = songs
	m := Model{page: pagePlaying, keys: defaultKeyMap(), width: 146, height: 41, lyricLines: 5, theme: DefaultTheme()}
	_ = m.applyState(st)
	if m.playerList.Selected() != 1 {
		t.Fatal("初始列表应定位到当前曲")
	}
	next, _ := m.handleKey(qualityKey('j'))
	m = next.(Model)
	if m.playerList.Selected() != 2 {
		t.Fatal("j 应选择下一行")
	}
	_ = m.applyState(st)
	if m.playerList.Selected() != 2 {
		t.Fatal("普通状态推送不能重置用户选择")
	}
	list := m.buildViewState().player.list
	if list.cursor != 2 || list.items[1].marker == "" || list.items[2].marker != "" {
		t.Fatal("当前播放标记应独立于选择光标")
	}
	st.Queue.Songs = songs[1:]
	_ = m.applyState(st)
	if m.playerList.Selected() != 1 {
		t.Fatal("列表删除条目后应按 ID 保留选择")
	}
	st.Playing = &ipc.Playing{Song: songs[1]}
	next, _ = m.Update(tea.WindowSizeMsg{Width: 40, Height: 14})
	m = next.(Model)
	if m.playerList.Selected() != 1 {
		t.Fatal("缩放应保留选择")
	}
	st.Playing = &ipc.Playing{Song: songs[2]}
	_ = m.applyState(st)
	if m.playerList.Selected() != 1 {
		t.Fatal("切歌应定位当前曲")
	}
	next, _ = m.handleKey(qualityKey('g'))
	m = next.(Model)
	if m.playerList.Selected() != 0 {
		t.Fatal("g 应跳转顶部")
	}
	next, _ = m.handleKey(qualityKey('G'))
	if next.(Model).playerList.Selected() != 1 {
		t.Fatal("G 应跳转底部")
	}
}

func TestPlayerListCommands(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "x")
	listener, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	commands := make(chan ipc.Message, 2)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = ipc.Encode(conn, ipc.NewMessage(0, ipc.TState, ipc.State{}))
		reader := bufio.NewReader(conn)
		for range 2 {
			msg, err := ipc.Decode(reader)
			if err != nil {
				return
			}
			commands <- msg
		}
	}()
	client, _, err := ipc.Dial(sock)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	songs := []netease.Song{{ID: 1, Name: "第一首"}, {ID: 2, Name: "第二首"}}
	m := Model{srv: client, page: pagePlaying, keys: defaultKeyMap(), width: 100, height: 35, lyricLines: 5}
	m.st.Playing = &ipc.Playing{Song: songs[0]}
	m.st.Queue.Songs = songs
	m.syncPlayerList(nil, true)
	m.playerList.Move(1)
	for _, code := range []rune{tea.KeyEnter, 'n'} {
		next, _ := m.handleKey(qualityKey(code))
		m = next.(Model)
		select {
		case msg := <-commands:
			if code == tea.KeyEnter {
				var cmd ipc.PlaySongCmd
				if msg.Type != ipc.TPlaySong || msg.DecodeData(&cmd) != nil || cmd.Song.ID != 2 || len(cmd.Playlist) != 2 {
					t.Fatalf("回车应从完整当前歌单播放选中曲: %+v", msg)
				}
			} else {
				var cmd ipc.PlayNextCmd
				if msg.Type != ipc.TPlayNext || msg.DecodeData(&cmd) != nil || cmd.Song.ID != 2 {
					t.Fatalf("n 应添加选中曲: %+v", msg)
				}
			}
		case <-time.After(time.Second):
			t.Fatal("未收到播放列表命令")
		}
	}
}

func TestCoverCDNRequest(t *testing.T) {
	for _, address := range []string{"http://p1.music.126.net/a.jpg?token=public&param=4000y4000", "https://music.126.net/a.jpg?token=public"} {
		u, err := coverRequestURL(address)
		if err != nil || u.Scheme != "https" || u.Query().Get("param") != "1024y1024" || u.Query().Get("token") != "public" {
			t.Fatalf("CDN 缩略图参数不符 %v %v", u, err)
		}
	}
	external := "https://music.126.net.example.org/a.jpg?param=10y10"
	u, err := coverRequestURL(external)
	if err != nil || u.String() != external {
		t.Fatal("外部图片源应使用原 URL")
	}
	var data bytes.Buffer
	_ = png.Encode(&data, coverFixture())
	original := http.DefaultClient
	defer func() { http.DefaultClient = original }()
	http.DefaultClient = &http.Client{Transport: coverTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("param") != "1024y1024" {
			t.Fatal("请求必须在下载前携带缩略参数")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(data.Bytes())), ContentLength: int64(data.Len()), Header: make(http.Header)}, nil
	})}
	if _, err := (&coverCache{}).load(context.Background(), "https://p1.music.126.net/large.jpg"); err != nil {
		t.Fatal(err)
	}
}

type coverTransport func(*http.Request) (*http.Response, error)

func (transport coverTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return transport(r)
}

func TestCoverFailureDetails(t *testing.T) {
	original := http.DefaultClient
	defer func() { http.DefaultClient = original }()
	for _, code := range []int{403, 500} {
		http.DefaultClient = &http.Client{Transport: coverTransport(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
		})}
		_, err := (&coverCache{}).load(context.Background(), "https://p1.music.126.net/a.jpg")
		if err == nil || !strings.Contains(err.Error(), "HTTP") {
			t.Fatal("应透传 HTTP 失败原因")
		}
	}
	http.DefaultClient = &http.Client{Transport: coverTransport(func(r *http.Request) (*http.Response, error) { return nil, context.DeadlineExceeded })}
	_, err := (&coverCache{}).load(context.Background(), "https://p1.music.126.net/a.jpg?secret=hidden")
	var urlErr *url.Error
	if err == nil || !errors.Is(err, context.DeadlineExceeded) || errors.As(err, &urlErr) || strings.Contains(err.Error(), "hidden") {
		t.Fatal("网络错误应保留原因并省略完整请求地址")
	}
	m := Model{page: pagePlaying, theme: DefaultTheme(), keys: defaultKeyMap(), width: 146, height: 41, lyricLines: 5, lyricGapBelow: 1, playbackGapBelow: 1}
	m.st.Playing = &ipc.Playing{Song: netease.Song{ID: 1}}
	next, _ := m.Update(coverLoadedMsg{url: "", seq: 0, err: errors.New("封面服务器返回 HTTP 403")})
	m = next.(Model)
	screen := strip(renderRoot(m.buildViewState(), testStyles()))
	if !strings.Contains(screen, "HTTP 403") || !strings.Contains(screen, "按 r 重试") {
		t.Fatal("播放页应显示可读失败原因和重试入口")
	}
}
