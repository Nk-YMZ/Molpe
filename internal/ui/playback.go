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

// gapExpiredMsg 歌曲间隔到期消息；seq 与 Model.gapSeq 不一致时说明是
// 切歌/点歌后残留的过期定时器，直接丢弃。
type gapExpiredMsg struct{ seq int }

// startGap 进入歌曲间隔：下一首置为待播（暂停）状态，并安排到期自动开播。
// 间隔期间除该一次性定时器外无其他后台活动；任何播放意图（播放键/切歌/
// 点歌）都会取消间隔立即开播。
func (m Model) startGap(song netease.Song) (tea.Model, tea.Cmd) {
	m.gapSong = &song
	m.gapSeq++
	seq := m.gapSeq
	m.playing = &playingInfo{song: song, level: m.quality}
	m.paused = true
	m.progressPos = 0
	m.pos.set(0)
	// 旧歌词随上一首结束清除，残留滚动定时器由序号作废。
	m.lyrics = nil
	m.lyricCur = -1
	m.lyricTicking = false
	m.lyricSeq++
	m.publishState()
	m.refreshQueuePopup()
	gap := time.Duration(m.songGap) * time.Second
	return m, tea.Batch(
		tea.Tick(gap, func(time.Time) tea.Msg { return gapExpiredMsg{seq} }),
		listenEndCmd(m.endCh),
	)
}

// cancelGap 取消进行中的歌曲间隔：递增序号使残留定时器到期时被丢弃。
func (m *Model) cancelGap() {
	m.gapSong = nil
	m.gapSeq++
}

// playGapSong 跳过剩余间隔，立即开播待播的下一首。
func (m Model) playGapSong() (tea.Model, tea.Cmd) {
	song := *m.gapSong
	m.cancelGap()
	m.paused = false
	return m, m.playCmd(song)
}

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
