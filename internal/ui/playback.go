package ui

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"molpe/internal/ipc"
)

// serverEventMsg 包装一条后端推送的事件消息。
type serverEventMsg ipc.Message

// serverGoneMsg 表示与后端的连接已断开。
type serverGoneMsg struct{}

// reconnectTimeout 连接中断后的重连等待上限（覆盖后端自动重启的耗时）。
const reconnectTimeout = 15 * time.Second

// reconnectedMsg 重连成功，携带新连接与最新快照。
type reconnectedMsg struct {
	client *ipc.Client
	st     ipc.State
}

// reconnectFailedMsg 重连超时。
type reconnectFailedMsg struct{}

// reconnectCmd 循环尝试重连后端直到超时（后端自动重启通常在 1 秒内完成）。
func (m Model) reconnectCmd() tea.Cmd {
	return func() tea.Msg {
		deadline := time.Now().Add(reconnectTimeout)
		for {
			c, st, err := ipc.Dial(ipc.SocketPath(m.dirs.Cache))
			if err == nil {
				return reconnectedMsg{client: c, st: st}
			}
			if time.Now().After(deadline) {
				return reconnectFailedMsg{}
			}
			time.Sleep(200 * time.Millisecond)
		}
	}
}

// listenServerCmd 等待一次后端事件或断开通知；每次事件处理后重新挂起。
func listenServerCmd(c *ipc.Client) tea.Cmd {
	return func() tea.Msg {
		select {
		case msg := <-c.Events():
			return serverEventMsg(msg)
		case <-c.Gone():
			return serverGoneMsg{}
		}
	}
}

// playPos 返回当前播放位置（秒）：以快照中的位置锚点为基准本地外推，
// 无需向后端或 mpv 周期查询。暂停/无播放/加载中（地址未就绪，无音频出声）
// 时返回锚点值。
func (m Model) playPos() float64 {
	if m.st.Playing == nil {
		return 0
	}
	if m.st.Paused || m.st.Loading {
		return m.st.Pos
	}
	return m.st.Pos + time.Since(m.stAt).Seconds()
}

// progressInterval 进度条刷新间隔。仅在播放中运行，暂停时完全停止。
const progressInterval = time.Second

// progressTickMsg 进度条刷新定时器消息；seq 与 Model.progressSeq 不一致时
// 说明是暂停/切歌后残留的过期定时器，直接丢弃。
type progressTickMsg struct{ seq int }

// scheduleProgressTick 安排进度条刷新：播放中每秒唤醒一次重绘（位置在
// 渲染时按锚点外推计算）；暂停、无播放、加载中、已在运行时不启动。
func (m *Model) scheduleProgressTick() tea.Cmd {
	if m.progressTicking || m.st.Paused || m.st.Loading || m.st.Playing == nil {
		return nil
	}
	m.progressTicking = true
	m.progressSeq++
	seq := m.progressSeq
	return tea.Tick(progressInterval, func(time.Time) tea.Msg { return progressTickMsg{seq} })
}

// revealInterval 曲名逐字出现的字符间隔；整段动画约半秒内完成并彻底停止。
const revealInterval = 24 * time.Millisecond

// revealTickMsg 逐字出现定时器消息；seq 与 Model.revealSeq 不一致时
// 说明是切歌后残留的过期定时器，直接丢弃。
type revealTickMsg struct{ seq int }

// scheduleRevealTick 安排下一个字符的出现；文本完整出现后自动停止，
// 不常驻后台。
func (m *Model) scheduleRevealTick() tea.Cmd {
	if m.revealTicking || len(m.revealTarget) == 0 || m.revealN >= len(m.revealTarget) {
		return nil
	}
	m.revealTicking = true
	m.revealSeq++
	seq := m.revealSeq
	return tea.Tick(revealInterval, func(time.Time) tea.Msg { return revealTickMsg{seq} })
}
