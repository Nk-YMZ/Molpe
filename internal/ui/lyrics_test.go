package ui

import (
	"testing"
	"time"

	"molpe/internal/ipc"
	"molpe/internal/netease"
)

func TestLyricLineAt(t *testing.T) {
	lines := []netease.LyricLine{
		{Time: 1, Text: "一"},
		{Time: 5, Text: "二"},
		{Time: 10, Text: "三"},
	}
	cases := []struct {
		pos  float64
		want int
	}{
		{0.5, -1}, // 第一句之前
		{1.0, 0},
		{7.9, 1},
		{10.0, 2},
		{999, 2}, // 超过最后一句
	}
	for _, c := range cases {
		if got := lyricLineAt(lines, c.pos); got != c.want {
			t.Fatalf("lyricLineAt(%v) = %d，预期 %d", c.pos, got, c.want)
		}
	}
	if got := lyricLineAt(nil, 1); got != -1 {
		t.Fatalf("无歌词时应为 -1，实际 %d", got)
	}
}

func TestLyricsSeekReschedules(t *testing.T) {
	lines := []netease.LyricLine{{Time: 1, Text: "一"}, {Time: 5, Text: "二"}, {Time: 10, Text: "三"}, {Time: 30, Text: "四"}}
	for _, paused := range []bool{false, true} {
		m := Model{page: pageSongs, theme: DefaultTheme(), lyrics: lines, lyricSongID: 1, lyricLines: 5, width: 120, height: 40}
		st := ipc.State{Playing: &ipc.Playing{Song: netease.Song{ID: 1}}, Paused: paused, Pos: 2}
		m.st, m.stAt = st, time.Now()
		_ = m.resetLyricTick()
		for _, target := range []float64{12, 3, 40, 0, 6} {
			previousSeq := m.lyricSeq
			st.Pos = target
			_ = m.applyState(st)
			if m.lyricCur != lyricLineAt(lines, target) {
				t.Fatalf("跳转 %.1f 秒后应立即校位，暂停 %v", target, paused)
			}
			if m.lyricSeq <= previousSeq {
				t.Fatal("跳转后应作废旧定时器")
			}
			current := m.lyricCur
			next, _ := m.Update(lyricTickMsg{seq: previousSeq})
			m = next.(Model)
			if m.lyricCur != current {
				t.Fatal("旧定时器不能覆盖跳转位置")
			}
			shouldTick := !paused && target < 30
			if m.lyricTicking != shouldTick {
				t.Fatalf("跳转后歌词计时状态错误 %.1f %v", target, paused)
			}
		}
		st.Loading = true
		_ = m.applyState(st)
		if m.lyricTicking {
			t.Fatal("加载期间应停止歌词计时")
		}
	}
}

func TestLyricsReplacementResetsTimer(t *testing.T) {
	m := Model{st: ipc.State{Playing: &ipc.Playing{Song: netease.Song{ID: 1}}, Pos: 7}, stAt: time.Now(), lyricTicking: true, lyricSeq: 3}
	_ = m.applyLyrics(ipc.LyricsMsg{SongID: 1, Lines: []netease.LyricLine{{Time: 0, Text: "一"}, {Time: 5, Text: "二"}, {Time: 8, Text: "三"}}})
	if m.lyricCur != 1 || m.lyricSeq <= 3 || !m.lyricTicking {
		t.Fatal("更新歌词应按当前进度重建下一句计时")
	}
}
