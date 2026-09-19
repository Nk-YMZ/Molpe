package queue

import (
	"molpe/internal/netease"
)

// State 是队列的可持久化快照，用于程序退出后下次启动恢复。
type State struct {
	Mode    Mode           `json:"mode"`
	Songs   []netease.Song `json:"songs,omitempty"`
	History []netease.Song `json:"history,omitempty"`
	Hpos    int            `json:"hpos"`
	NextUp  []netease.Song `json:"next_up,omitempty"`
}

// Snapshot 导出当前队列状态的快照。
func (q *Queue) Snapshot() State {
	return State{
		Mode:    q.mode,
		Songs:   q.Songs(),
		History: append([]netease.Song(nil), q.history...),
		Hpos:    q.hpos,
		NextUp:  q.NextUp(),
	}
}

// Restore 用快照覆盖队列状态；非法的模式与位置自动收敛到安全值。
func (q *Queue) Restore(s State) {
	q.mode = s.Mode
	if q.mode < ModeSequential || q.mode > ModeRandom {
		q.mode = ModeLoop
	}
	q.SetPlaylist(s.Songs)
	q.nextUp = append([]netease.Song(nil), s.NextUp...)
	q.history = append([]netease.Song(nil), s.History...)
	if over := len(q.history) - q.historyLimit(); over > 0 {
		q.history = q.history[over:]
	}
	q.hpos = s.Hpos
	if q.hpos >= len(q.history) {
		q.hpos = len(q.history) - 1
	}
	if q.hpos < -1 {
		q.hpos = -1
	}
}
