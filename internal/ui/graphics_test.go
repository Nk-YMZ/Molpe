package ui

import (
	"bytes"
	"encoding/base64"
	"errors"
	"image"
	"image/png"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/kitty"

	"molpe/internal/ipc"
	"molpe/internal/netease"
)

func graphicsFixture() Model {
	m := Model{page: pagePlaying, theme: DefaultTheme(), keys: defaultKeyMap(), width: 146, height: 41, lyricLines: 5, lyricGapBelow: 1, playbackGapBelow: 1}
	m.st.Playing = &ipc.Playing{Song: netease.Song{ID: 1, CoverURL: "https://example.test/cover"}}
	m.cover.url = "https://example.test/cover"
	m.cover.image = coverFixture()
	m.refreshCoverArt()
	m.graphics = graphicsModel{allowed: true, prefix: 0x4d000000, profile: colorprofile.TrueColor, queryOK: true, unicodeOK: true}
	return m
}

func graphicsReply(id int, payload string) uv.KittyGraphicsEvent {
	return uv.KittyGraphicsEvent{Options: kitty.Options{ID: id}, Payload: []byte(payload)}
}

func TestGraphicsPolicy(t *testing.T) {
	for _, name := range []string{"TMUX", "STY", "MOLPE_COVER_MODE"} {
		t.Setenv(name, "")
	}
	t.Setenv("TERM", "xterm-kitty")
	t.Setenv("TERM_PROGRAM", "kitty")
	if !newGraphicsModel().allowed {
		t.Fatal("直接终端应允许能力查询")
	}
	for _, item := range [][2]string{{"TMUX", "/tmp/x"}, {"STY", "screen"}, {"TERM", "screen-256color"}, {"TERM", "tmux-256color"}, {"MOLPE_COVER_MODE", "blocks"}, {"TERM_PROGRAM", "Apple_Terminal"}} {
		t.Run(item[0]+item[1], func(t *testing.T) {
			t.Setenv(item[0], item[1])
			g := newGraphicsModel()
			if g.allowed || g.init() != nil {
				t.Fatal("兼容模式不应发送图片查询")
			}
		})
	}
}

func TestGraphicsDetection(t *testing.T) {
	for _, name := range []string{"kitty(0.28.0)", "kitty(0.43.1)", "kitty(1.0.0)", "ghostty 1.2.3"} {
		if !supportsUnicodeImages(name) {
			t.Fatalf("应支持 %s", name)
		}
	}
	for _, name := range []string{"kitty(0.27.1)", "kitty(0.9.0)", "WezTerm 20240203", "iTerm2 3.6.0", "Apple_Terminal", "kitty(bad)"} {
		if supportsUnicodeImages(name) {
			t.Fatalf("不可推断占位符支持 %s", name)
		}
	}
	m := graphicsFixture()
	m.graphics.queryOK, m.graphics.unicodeOK = false, false
	next, _ := m.Update(graphicsReply(graphicsQueryID, "OK"))
	m = next.(Model)
	if m.graphics.id != 0 {
		t.Fatal("仅基础协议响应不应启用占位符")
	}
	next, _ = m.Update(uv.KittyGraphicsEvent{Options: kitty.Options{ID: graphicsFeatureID, VirtualPlacement: true}, Payload: []byte("OK")})
	m = next.(Model)
	if m.graphics.id == 0 {
		t.Fatal("基础解码和占位符能力均通过后应准备图片")
	}
	m = graphicsFixture()
	m.graphics.unicodeOK = false
	next, _ = m.Update(tea.TerminalVersionMsg{Name: "kitty(0.40.0)"})
	if next.(Model).graphics.id == 0 {
		t.Fatal("旧 Kitty 可通过实际版本响应确认占位符")
	}
	m = graphicsFixture()
	m.graphics.queryOK, m.graphics.unicodeOK = false, false
	next, _ = m.Update(graphicsProbeTimeoutMsg{})
	m = next.(Model)
	next, _ = m.Update(graphicsReply(graphicsQueryID, "OK"))
	if next.(Model).graphics.allowed {
		t.Fatal("超时后迟到查询不得切换显示模式")
	}
}

func TestGraphicsEncoding(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 1024, 1024))
	fixture := coverFixture()
	for y := range 1024 {
		for x := range 1024 {
			img.Set(x, y, fixture.At(x%4, y%4))
		}
	}
	var source bytes.Buffer
	if err := png.Encode(&source, img); err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeCover(source.Bytes())
	if err != nil || decoded.Bounds().Dx() != 1024 {
		t.Fatal("原生图片应保留 1024 像素分辨率")
	}
	data, err := encodeGraphics(decoded, 0x4d000001, 44, 22)
	if err != nil {
		t.Fatal(err)
	}
	var payload strings.Builder
	count := 0
	for _, chunk := range strings.Split(data, "\x1b_G")[1:] {
		header, body, ok := strings.Cut(strings.TrimSuffix(chunk, "\x1b\\"), ";")
		if !ok || len(body) > 4096 {
			t.Fatal("图片应使用不超过 4096 字节的直接传输块")
		}
		if count == 0 {
			for _, part := range []string{"a=T", "f=100", "U=1", "C=1", "c=44", "r=22", "p=1"} {
				if !strings.Contains(header, part) {
					t.Fatalf("缺少 %s", part)
				}
			}
		}
		payload.WriteString(body)
		count++
	}
	raw, err := base64.StdEncoding.DecodeString(payload.String())
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(raw))
	if err != nil || cfg.Width != 1024 || cfg.Height != 1024 || count < 2 {
		t.Fatalf("PNG 分块传输不符 %v %d", err, count)
	}
}

func TestGraphicsLifecycle(t *testing.T) {
	m := graphicsFixture()
	encode := m.syncGraphics()
	firstID := m.graphics.id
	encoded := encode().(graphicsEncodedMsg)
	next, cmd := m.Update(encoded)
	m = next.(Model)
	if cmd == nil || m.graphics.ready {
		t.Fatal("传输后必须等待终端确认")
	}
	if strings.ContainsRune(m.buildViewState().player.art, kitty.Placeholder) {
		t.Fatal("确认前应继续显示色块")
	}
	next, _ = m.Update(graphicsReply(firstID, "OK"))
	m = next.(Model)
	if !m.graphics.ready || !strings.ContainsRune(m.buildViewState().player.art, kitty.Placeholder) {
		t.Fatal("确认后应使用原生图片")
	}
	serial := m.graphics.serial
	if m.syncGraphics() != nil || m.graphics.serial != serial {
		t.Fatal("普通重绘不能重复传图")
	}
	next, _ = m.Update(tea.WindowSizeMsg{Width: 180, Height: 52})
	m = next.(Model)
	if m.graphics.id == 0 || m.graphics.id == firstID || m.graphics.ready {
		t.Fatal("缩放应准备新尺寸并暂用色块")
	}
	next, cmd = m.Update(encoded)
	m = next.(Model)
	if cmd != nil {
		t.Fatal("迟到的编码结果应丢弃")
	}
	next, cmd = m.Update(graphicsReply(firstID, "OK"))
	m = next.(Model)
	raw := cmd().(tea.RawMsg).Msg.(string)
	if !strings.Contains(raw, "d=I") || strings.Contains(raw, "d=A") {
		t.Fatal("迟到响应只清理本前端旧图片")
	}
	m.graphics.ready = true
	next, _ = m.Update(qualityKey('t'))
	m = next.(Model)
	if !m.picker.open || m.graphics.id != 0 || m.graphics.ready {
		t.Fatalf("弹窗必须清除图片 open=%v id=%d ready=%v err=%s", m.picker.open, m.graphics.id, m.graphics.ready, m.errNote)
	}
	next, _ = m.Update(qualityKey(tea.KeyEscape))
	m = next.(Model)
	if m.graphics.id == 0 {
		t.Fatal("关闭弹窗应恢复原生图片准备")
	}
	next, _ = m.Update(qualityKey('b'))
	m = next.(Model)
	if m.page == pagePlaying || m.graphics.id != 0 {
		t.Fatal("返回浏览应清除图片")
	}
}

func TestGraphicsFallback(t *testing.T) {
	for _, reason := range []string{"error", "timeout", "encode", "color"} {
		t.Run(reason, func(t *testing.T) {
			m := graphicsFixture()
			_ = m.syncGraphics()
			id := m.graphics.id
			var msg tea.Msg
			switch reason {
			case "error":
				msg = graphicsReply(id, "ENOSPC: unavailable")
			case "timeout":
				msg = graphicsTimeoutMsg{id: id}
			case "encode":
				msg = graphicsEncodedMsg{id: id, err: errors.New("encoding failed")}
			case "color":
				msg = tea.ColorProfileMsg{Profile: colorprofile.ANSI256}
			}
			next, _ := m.Update(msg)
			m = next.(Model)
			if m.graphics.id != 0 || m.graphics.ready || strings.ContainsRune(m.buildViewState().player.art, kitty.Placeholder) {
				t.Fatal("失败应回退到色块")
			}
			next, _ = m.Update(graphicsReply(id, "OK"))
			m = next.(Model)
			if m.graphics.ready {
				t.Fatal("失败后迟到确认不可恢复旧图片")
			}
		})
	}
}

func TestGraphicsPlaceholderLayout(t *testing.T) {
	m := graphicsFixture()
	_ = m.syncGraphics()
	m.graphics.ready = true
	m.lyrics = []netease.LyricLine{{Text: "封面下方歌词"}}
	m.lyricCur = 0
	frame := renderRoot(m.buildViewState(), testStyles())
	lines := strings.Split(frame, "\n")
	if len(lines) != m.height {
		t.Fatal("原生图片不得改变界面高度")
	}
	cells := 0
	for _, line := range lines {
		if ansi.StringWidth(line) > m.width {
			t.Fatal("原生图片不得溢出窗口宽度")
		}
		cells += strings.Count(line, string(kitty.Placeholder))
	}
	if cells != m.graphics.width*m.graphics.height || !strings.Contains(frame, "封面下方歌词") || !strings.Contains(frame, "播放列表") {
		t.Fatal("原生图片布局缺失")
	}
	_ = m.quitGraphics()
	if m.graphics.allowed || m.graphics.id != 0 {
		t.Fatal("退出应禁用传输并清理图片")
	}
}
