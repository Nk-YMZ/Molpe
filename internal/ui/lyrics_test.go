package ui

import (
	"testing"

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
