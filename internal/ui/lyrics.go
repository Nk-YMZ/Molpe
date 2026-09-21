package ui

import (
	"sort"
	"time"

	tea "charm.land/bubbletea/v2"

	"molpe/internal/ipc"
	"molpe/internal/netease"
)

// lyricsMsg 是后端推送歌词的内部消息形式。
type lyricsMsg = ipc.LyricsMsg

// lyricTickMsg 歌词滚动定时器消息；seq 与 Model.lyricSeq 不一致时
// 说明是暂停/切歌后残留的过期定时器，直接丢弃。
type lyricTickMsg struct{ seq int }

// applyLyrics 应用后端推送的歌词；切歌后的过期推送直接丢弃。
func (m *Model) applyLyrics(lm ipc.LyricsMsg) tea.Cmd {
	if m.st.Playing == nil || lm.SongID != m.st.Playing.Song.ID {
		return nil
	}
	m.lyrics = lm.Lines
	m.lyricSongID = lm.SongID
	// 暂停中收到歌词也要立即定位到当前句。
	m.lyricCur = lyricLineAt(m.lyrics, m.playPos())
	return m.scheduleLyricTick()
}

// handleLyrics 处理歌词消息（Update 分支入口）。
func (m Model) handleLyrics(msg lyricsMsg) (tea.Model, tea.Cmd) {
	return m, m.applyLyrics(msg)
}

// scheduleLyricTick 安排歌词滚动定时器：按下一句歌词的时间戳精确定时
// （每句歌词只唤醒一次，避免固定频率轮询空耗电量）；暂停、无歌词、
// 已是最后一句时不启动。已在运行时不重复启动。
func (m *Model) scheduleLyricTick() tea.Cmd {
	if m.lyricTicking || m.st.Paused || len(m.lyrics) == 0 {
		return nil
	}
	pos := m.playPos()
	m.lyricCur = lyricLineAt(m.lyrics, pos)
	next := m.lyricCur + 1
	if next >= len(m.lyrics) {
		return nil // 最后一句：停止轮询
	}
	delay := max(50*time.Millisecond, time.Duration((m.lyrics[next].Time-pos)*float64(time.Second)))
	m.lyricTicking = true
	m.lyricSeq++
	seq := m.lyricSeq
	return tea.Tick(delay, func(time.Time) tea.Msg { return lyricTickMsg{seq} })
}

// lyricLineAt 返回 pos 秒处应高亮的歌词行下标；pos 早于第一句时为 -1。
func lyricLineAt(lines []netease.LyricLine, pos float64) int {
	return sort.Search(len(lines), func(i int) bool { return lines[i].Time > pos }) - 1
}
