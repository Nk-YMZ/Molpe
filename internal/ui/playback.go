package ui

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"molpe/internal/mpris"
	"molpe/internal/netease"
)

type songURLFetchedMsg struct {
	song netease.Song
	url  *netease.SongURL
	err  error
	seq  int // 请求序号，用于丢弃过期响应
}

type mprisEventMsg mpris.Event

// playerEndedMsg 表示当前曲目自然播完（mpv end-file/eof），应自动连播。
type playerEndedMsg struct{}

// progressInterval 进度条刷新间隔。仅在播放中运行，暂停时完全停止。
const progressInterval = time.Second

// progressTickMsg 进度条刷新定时器消息；seq 与 Model.progressSeq 不一致时
// 说明是暂停/切歌后残留的过期定时器，直接丢弃。
type progressTickMsg struct{ seq int }

// scheduleProgressTick 安排进度条刷新：播放中每秒唤醒一次读取播放位置；
// 暂停、无播放、已在运行时不启动。
func (m *Model) scheduleProgressTick() tea.Cmd {
	if m.progressTicking || m.paused || m.playing == nil {
		return nil
	}
	if _, err := m.player.get(); err != nil {
		return nil
	}
	m.progressTicking = true
	m.progressSeq++
	seq := m.progressSeq
	return tea.Tick(progressInterval, func(time.Time) tea.Msg { return progressTickMsg{seq} })
}

// playCmd 拉取歌曲播放地址。每次发起都会递增请求序号，
// 快速连续切歌时先发出的慢响应会被 handleSongURL 按序号丢弃。
func (m *Model) playCmd(song netease.Song) tea.Cmd {
	m.playSeq++
	seq := m.playSeq
	return func() tea.Msg {
		u, err := m.client.SongURL(song.ID, m.quality)
		return songURLFetchedMsg{song: song, url: u, err: err, seq: seq}
	}
}

// listenMprisCmd 等待一次桌面控制事件；服务仅随进程退出回收，
// 进程退出前该监听 goroutine 由 Bubble Tea 一并结束。
func listenMprisCmd(svc *mpris.Service) tea.Cmd {
	if svc == nil {
		return nil
	}
	return func() tea.Msg {
		ev, ok := <-svc.Events()
		if !ok {
			return nil
		}
		return mprisEventMsg(ev)
	}
}

// listenEndCmd 等待一次 mpv 自然播完事件。
func listenEndCmd(endCh <-chan struct{}) tea.Cmd {
	return func() tea.Msg {
		<-endCh
		return playerEndedMsg{}
	}
}
