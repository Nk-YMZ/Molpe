package ipc

import (
	"bufio"
	"bytes"
	"testing"
)

func TestEncodeDecodeRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	in := []Message{
		NewMessage(0, TState, State{Checked: true, Volume: 42, Quality: "hires"}),
		NewMessage(7, TSongs, SongsMsg{PlaylistID: 123}),
		NewMessage(0, TShutdown, nil),
	}
	for _, m := range in {
		if err := Encode(&buf, m); err != nil {
			t.Fatalf("Encode 失败: %v", err)
		}
	}
	r := bufio.NewReader(&buf)
	var got []Message
	for _, want := range in {
		m, err := Decode(r)
		if err != nil {
			t.Fatalf("Decode 失败: %v", err)
		}
		if m.ID != want.ID || m.Type != want.Type {
			t.Fatalf("消息头不符: got %+v, want %+v", m, want)
		}
		got = append(got, m)
	}
	// 校验载荷内容。
	var st State
	if err := got[0].DecodeData(&st); err != nil || st.Volume != 42 || st.Quality != "hires" {
		t.Fatalf("状态载荷解析不符: %v %+v", err, st)
	}
	var sm SongsMsg
	if err := got[1].DecodeData(&sm); err != nil || sm.PlaylistID != 123 {
		t.Fatalf("载荷解析不符: %v %+v", err, sm)
	}
}

func TestDecodeEmptyPayload(t *testing.T) {
	var buf bytes.Buffer
	if err := Encode(&buf, NewMessage(0, TToggle, nil)); err != nil {
		t.Fatalf("Encode 失败: %v", err)
	}
	msg, err := Decode(bufio.NewReader(&buf))
	if err != nil {
		t.Fatalf("Decode 失败: %v", err)
	}
	var v struct{ X int }
	if err := msg.DecodeData(&v); err != nil {
		t.Fatalf("空载荷解析应成功: %v", err)
	}
}
