package ui

import (
	"sort"
	"time"

	tea "charm.land/bubbletea/v2"

	"molpe/internal/netease"
)

// lyricFallbackInterval 播放位置不可用时的兜底轮询间隔。
const lyricFallbackInterval = 500 * time.Millisecond

// lyricsFetchedMsg 歌词获取结果；songID 用于丢弃切歌后的过期响应。
type lyricsFetchedMsg struct {
	songID int64
	lines  []netease.LyricLine
	err    error
}

// lyricTickMsg 歌词滚动定时器消息；seq 与 Model.lyricSeq 不一致时
// 说明是暂停/切歌后残留的过期定时器，直接丢弃。
type lyricTickMsg struct{ seq int }

// fetchLyricsCmd 拉取歌曲歌词。
func (m *Model) fetchLyricsCmd(songID int64) tea.Cmd {
	return func() tea.Msg {
		lines, err := m.client.Lyrics(songID, m.cfg.EffectiveLyricTranslation())
		return lyricsFetchedMsg{songID: songID, lines: lines, err: err}
	}
}

// scheduleLyricTick 安排歌词滚动定时器：按下一句歌词的时间戳精确定时
// （每句歌词只唤醒一次，避免固定频率轮询空耗电量）；播放位置不可用时
// 退化为兜底轮询；暂停、无歌词、已是最后一句时不启动。已在运行时不重复启动。
func (m *Model) scheduleLyricTick() tea.Cmd {
	if m.lyricTicking || m.paused || len(m.lyrics) == 0 {
		return nil
	}
	p, err := m.player.get()
	if err != nil {
		return nil
	}
	pos, err := p.Position()
	if err != nil {
		// 位置暂不可用（如加载中）：兜底轮询直到可用。
		m.lyricTicking = true
		m.lyricSeq++
		seq := m.lyricSeq
		return tea.Tick(lyricFallbackInterval, func(time.Time) tea.Msg { return lyricTickMsg{seq} })
	}
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

// handleLyricsFetched 处理歌词获取结果；切歌后的过期响应直接丢弃。
func (m Model) handleLyricsFetched(msg lyricsFetchedMsg) (tea.Model, tea.Cmd) {
	if m.playing == nil || msg.songID != m.playing.song.ID {
		return m, nil
	}
	if msg.err != nil {
		return m, m.setNote("获取歌词失败：" + msg.err.Error())
	}
	m.lyrics = msg.lines
	m.updateLyricLine() // 暂停中切歌也要立即定位到当前句
	return m, m.scheduleLyricTick()
}

// updateLyricLine 查询播放位置并更新当前歌词行；位置暂不可用时保持现状。
func (m *Model) updateLyricLine() {
	p, err := m.player.get()
	if err != nil {
		return
	}
	pos, err := p.Position()
	if err != nil {
		return
	}
	m.lyricCur = lyricLineAt(m.lyrics, pos)
}

// lyricLineAt 返回 pos 秒处应高亮的歌词行下标；pos 早于第一句时为 -1。
func lyricLineAt(lines []netease.LyricLine, pos float64) int {
	return sort.Search(len(lines), func(i int) bool { return lines[i].Time > pos }) - 1
}
