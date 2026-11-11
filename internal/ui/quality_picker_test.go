package ui

import (
	"bufio"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"molpe/internal/ipc"
)

func qualityKey(code rune) tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: code})
}

func TestQualityKeys(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "x")
	listener, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	commands := make(chan ipc.Message, 1)
	state := ipc.State{Checked: true, Quality: "hires", QualitySelection: true}
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = ipc.Encode(conn, ipc.NewMessage(0, ipc.TState, state))
		msg, err := ipc.Decode(bufio.NewReader(conn))
		if err == nil {
			commands <- msg
		}
	}()
	client, _, err := ipc.Dial(sock)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	m := Model{srv: client, st: state, keys: defaultKeyMap(), width: 80, height: 30, page: pageLogin}
	next, _ := m.handleKey(qualityKey('a'))
	m = next.(Model)
	if !m.quality.open || m.quality.list.Selected() != 4 {
		t.Fatal("a 应打开音质弹窗，并定位到当前偏好")
	}
	next, _ = m.handleKey(qualityKey(tea.KeyUp))
	m = next.(Model)
	next, _ = m.handleKey(qualityKey(tea.KeyEnter))
	if next.(Model).quality.open {
		t.Fatal("确认后应关闭弹窗")
	}
	select {
	case msg := <-commands:
		var cmd ipc.QualityCmd
		if msg.Type != ipc.TSetQuality || msg.DecodeData(&cmd) != nil || cmd.Quality != "lossless" {
			t.Fatalf("发送的音质命令不符: %+v", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("未发送音质选择")
	}
}

func TestQualityCancel(t *testing.T) {
	m := Model{st: ipc.State{Quality: "hires", QualitySelection: true}, keys: defaultKeyMap(), height: 30}
	m.openQualityPicker()
	next, _ := m.handleKey(qualityKey(tea.KeyEscape))
	m = next.(Model)
	if m.quality.open || m.st.Quality != "hires" {
		t.Fatal("取消应保持原音质")
	}
	m.st.QualitySelection = false
	next, _ = m.handleKey(qualityKey('a'))
	m = next.(Model)
	if m.quality.open || !strings.Contains(m.note, "后端版本较旧") {
		t.Fatal("连接旧后端时应提示重启，避免静默忽略选择")
	}
}

func TestQualityRender(t *testing.T) {
	m := Model{st: ipc.State{Quality: "hires", Playing: &ipc.Playing{Level: "lossless"}}, keys: defaultKeyMap(), width: 40, height: 14}
	m.openQualityPicker()
	view := m.buildViewState().quality
	out := strip(renderQualityPicker(*view, testStyles(), m.width, m.height))
	for _, text := range []string{"选择音质", "Hi-Res", "当前偏好", "实际音质：无损"} {
		if !strings.Contains(out, text) {
			t.Fatalf("缺少 %q: %s", text, out)
		}
	}
	for _, line := range strings.Split(out, "\n") {
		if ansi.StringWidth(line) > m.width {
			t.Fatal("弹窗超出窗口宽度")
		}
	}
}

func TestDecodedQualityDisplay(t *testing.T) {
	m := Model{st: ipc.State{Quality: "hires", Playing: &ipc.Playing{Level: "lossless", DecodedLevel: "hires", Codec: "flac", SampleRate: 48000, SampleFormat: "s32"}}, keys: defaultKeyMap(), width: 80, height: 30}
	if !strings.Contains(m.headerRight(), "Hi-Res") {
		t.Fatal("头部应使用校正后的等级")
	}
	m.openQualityPicker()
	view := m.buildViewState().quality
	screen := strip(renderQualityPicker(*view, testStyles(), m.width, m.height))
	for _, part := range []string{"Hi-Res", "接口标记无损", "flac", "48 kHz", "s32"} {
		if !strings.Contains(screen, part) {
			t.Fatalf("缺少实际音质详情 %s", part)
		}
	}
}
