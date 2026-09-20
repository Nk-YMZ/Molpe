// Package queue 实现播放器核心队列逻辑。
//
// 支持顺序播放、列表循环、随机播放三种模式；历史队列严格记录实际播放顺序
// （上限可配，默认 100 首），“上一首/下一首”沿历史前后移动；“下一首播放”
// 队列按添加时间先进先出，在任何模式下都拥有最高播放优先级，且在手动切换
// 歌曲/歌单时保留。
package queue

import (
	"math/rand/v2"

	"molpe/internal/netease"
)

// Mode 播放模式。
type Mode int

const (
	// ModeSequential 顺序播放：按歌单顺序播放，播到末尾停止。
	ModeSequential Mode = iota
	// ModeLoop 列表循环：播到末尾回到歌单开头。
	ModeLoop
	// ModeRandom 随机播放：下一首在歌单内等概率选取，但不与最近
	// RandomNoRepeat 首已播放的歌曲重复（见 Options.RandomNoRepeat）。
	ModeRandom
)

const defaultHistoryLimit = 100

// Options 队列行为的可调参数，为后续配置化预留。
type Options struct {
	// HistoryLimit 历史队列上限；<= 0 时使用默认值 100。
	HistoryLimit int
	// RandomNoRepeat 随机播放的回避窗口大小：不与最近 n 首已播放且属于
	// 当前歌单的歌曲重复（按歌曲 ID 去重）；0 表示完全随机（可立即重复
	// 当前曲），负值按 0 处理；窗口自动收敛为 min(n, 歌单长度-1)，
	// 保证候选集非空。
	RandomNoRepeat int
}

// Queue 播放队列。不是并发安全的，由调用方保证单 goroutine 访问。
type Queue struct {
	mode Mode
	opts Options

	songs []netease.Song // 当前歌单

	history []netease.Song // 实际播放过的歌曲，按时间顺序
	hpos    int            // 当前曲在 history 中的下标，-1 表示尚未播放
	cur     *netease.Song  // 当前播放曲，nil 表示无（刚被移除或尚未播放）
	anchor  int            // cur 为 nil 时 pick 的歌单位置锚点（移除当前曲后保留），-1 表示无

	nextUp []netease.Song // “下一首播放”队列，先进先出
}

// New 创建播放队列。songs 为初始歌单（可为空）。
func New(songs []netease.Song, mode Mode, opts Options) *Queue {
	q := &Queue{mode: mode, opts: opts, hpos: -1, anchor: -1}
	q.SetPlaylist(songs)
	return q
}

// Mode 返回当前播放模式。
func (q *Queue) Mode() Mode {
	return q.mode
}

// SetMode 切换播放模式，不影响歌单、历史与下一首队列。
func (q *Queue) SetMode(mode Mode) {
	q.mode = mode
}

// SetPlaylist 更换歌单（手动切换歌单/歌曲来源时调用）。
// 历史队列与“下一首播放”队列均保留。
func (q *Queue) SetPlaylist(songs []netease.Song) {
	q.songs = append([]netease.Song(nil), songs...)
}

// Songs 返回当前歌单的副本。
func (q *Queue) Songs() []netease.Song {
	return append([]netease.Song(nil), q.songs...)
}

// Current 返回当前播放的歌曲；尚未播放时 ok 为 false。
func (q *Queue) Current() (song netease.Song, ok bool) {
	if q.cur == nil {
		return netease.Song{}, false
	}
	return *q.cur, true
}

// Play 立即播放指定歌曲（手动点歌）：截断“上一首”回退出的前进历史，
// 将该曲追加为最新历史；“下一首播放”队列保留。
func (q *Queue) Play(song netease.Song) {
	q.record(song)
}

// PlayNext 将歌曲加入“下一首播放”队列尾部，越晚添加越晚播放。
func (q *Queue) PlayNext(song netease.Song) {
	q.nextUp = append(q.nextUp, song)
}

// NextUp 返回“下一首播放”队列的副本，按播放顺序排列。
func (q *Queue) NextUp() []netease.Song {
	return append([]netease.Song(nil), q.nextUp...)
}

// Prev 沿历史回退一首；已处于历史最早一首时 ok 为 false。
// 回退不消费“下一首播放”队列。
func (q *Queue) Prev() (song netease.Song, ok bool) {
	if q.hpos <= 0 {
		return netease.Song{}, false
	}
	q.hpos--
	song = q.history[q.hpos]
	q.cur = &song // 持有副本，避免历史切片原地搬移时指针失效
	return song, true
}

// Next 前进一首，优先级依次为：
//  1. 历史中“上一首”回退出的前进位置；
//  2. “下一首播放”队列队首；
//  3. 按当前播放模式从歌单选取。
//
// 无可播放歌曲时（空歌单，或顺序播放已到歌单末尾）ok 为 false。
func (q *Queue) Next() (song netease.Song, ok bool) {
	if q.hpos+1 < len(q.history) {
		q.hpos++
		song = q.history[q.hpos]
		q.cur = &song // 持有副本，避免历史切片原地搬移时指针失效
		return song, true
	}
	if len(q.nextUp) > 0 {
		song = q.nextUp[0]
		q.nextUp = q.nextUp[1:]
		q.record(song)
		return song, true
	}
	song, ok = q.pick()
	if !ok {
		return netease.Song{}, false
	}
	q.record(song)
	return song, true
}

// record 将 song 追加为当前播放：截断 hpos 之后的前进历史并维护上限。
func (q *Queue) record(song netease.Song) {
	q.history = append(q.history[:q.hpos+1], song)
	if over := len(q.history) - q.historyLimit(); over > 0 {
		q.history = append([]netease.Song(nil), q.history[over:]...)
	}
	q.hpos = len(q.history) - 1
	s := song
	q.cur = &s // 持有副本，避免历史切片原地搬移时指针失效
	q.anchor = -1
}

func (q *Queue) historyLimit() int {
	if q.opts.HistoryLimit > 0 {
		return q.opts.HistoryLimit
	}
	return defaultHistoryLimit
}

// pick 按当前模式从歌单选取下一首。
func (q *Queue) pick() (netease.Song, bool) {
	if len(q.songs) == 0 {
		return netease.Song{}, false
	}
	cur := q.currentIndex()
	switch q.mode {
	case ModeLoop:
		return q.songs[(cur+1)%len(q.songs)], true
	case ModeRandom:
		return q.randomPick()
	default: // ModeSequential
		if cur+1 >= len(q.songs) {
			return netease.Song{}, false
		}
		return q.songs[cur+1], true
	}
}

// currentIndex 返回当前曲在歌单中的下标；未播放或当前曲不在歌单中时为 -1。
// 当前曲刚被 RemoveCurrent 移除时，返回移除前保留的位置锚点。
func (q *Queue) currentIndex() int {
	if q.cur == nil {
		return q.anchor
	}
	for i, s := range q.songs {
		if s.ID == q.cur.ID {
			return i
		}
	}
	return -1
}

// randomPick 随机选取下一首：候选为歌单中不在回避窗口内的歌曲，等概率选取。
// 窗口大小经 noRepeatLimit 收敛，候选集保证非空。
func (q *Queue) randomPick() (netease.Song, bool) {
	if n := q.noRepeatLimit(); n > 0 {
		excluded := q.recentIDs(n)
		candidates := make([]int, 0, len(q.songs))
		for i, s := range q.songs {
			if _, skip := excluded[s.ID]; !skip {
				candidates = append(candidates, i)
			}
		}
		if len(candidates) > 0 {
			return q.songs[candidates[rand.IntN(len(candidates))]], true
		}
		// 兜底：窗口收敛已保证候选非空，此处防御历史与歌单不同步等异常。
	}
	return q.songs[rand.IntN(len(q.songs))], true
}

// noRepeatLimit 返回生效的回避窗口大小：min(RandomNoRepeat, 歌单长度-1)，
// 保证至少留出一首候选；负值按 0 处理（完全随机）。
func (q *Queue) noRepeatLimit() int {
	if n := min(q.opts.RandomNoRepeat, len(q.songs)-1); n > 0 {
		return n
	}
	return 0
}

// recentIDs 从当前播放位置（含当前曲）沿历史向前收集属于当前歌单的歌曲 ID，
// 按 ID 去重，最多收集 limit 个；其他歌单的历史歌曲不占窗口。
func (q *Queue) recentIDs(limit int) map[int64]struct{} {
	inPlaylist := make(map[int64]struct{}, len(q.songs))
	for _, s := range q.songs {
		inPlaylist[s.ID] = struct{}{}
	}
	ids := make(map[int64]struct{}, limit)
	for i := min(q.hpos, len(q.history)-1); i >= 0 && len(ids) < limit; i-- {
		id := q.history[i].ID
		if _, ok := inPlaylist[id]; ok {
			ids[id] = struct{}{}
		}
	}
	return ids
}

// History 返回完整历史记录的副本，按时间正序（越早越靠前）。
func (q *Queue) History() []netease.Song {
	return append([]netease.Song(nil), q.history...)
}

// HistoryPos 返回当前曲在历史记录中的下标；无当前曲时为 -1。
func (q *Queue) HistoryPos() int {
	if q.cur == nil {
		return -1
	}
	return q.hpos
}

// Position 返回当前曲在歌单中的位置（从 1 计）与歌单总数；
// 无当前曲或当前曲不在歌单中时 ok 为 false。
func (q *Queue) Position() (pos, total int, ok bool) {
	if q.cur == nil {
		return 0, len(q.songs), false
	}
	idx := q.currentIndex()
	if idx < 0 {
		return 0, len(q.songs), false
	}
	return idx + 1, len(q.songs), true
}

// Upcoming 返回歌单中当前曲之后的歌曲及其在歌单中的起始下标；
// 当前曲不在歌单中或已是歌单末尾时 songs 为空。
// 与播放模式无关（随机模式下是否展示由调用方决定），不考虑循环回卷。
func (q *Queue) Upcoming() (base int, songs []netease.Song) {
	idx := q.currentIndex()
	if idx < 0 || idx+1 >= len(q.songs) {
		return 0, nil
	}
	return idx + 1, append([]netease.Song(nil), q.songs[idx+1:]...)
}

// RemoveHistory 删除历史记录中的第 i 条；当前曲请使用 RemoveCurrent。
func (q *Queue) RemoveHistory(i int) {
	if i < 0 || i >= len(q.history) || (q.cur != nil && i == q.hpos) {
		return
	}
	q.history = append(q.history[:i], q.history[i+1:]...)
	if i < q.hpos {
		q.hpos--
	}
}

// RemoveNextUp 删除“下一首播放”队列中的第 i 首。
func (q *Queue) RemoveNextUp(i int) {
	if i < 0 || i >= len(q.nextUp) {
		return
	}
	q.nextUp = append(q.nextUp[:i], q.nextUp[i+1:]...)
}

// RemovePlaylist 删除歌单中的第 i 首歌；不影响历史与当前播放状态。
func (q *Queue) RemovePlaylist(i int) {
	if i < 0 || i >= len(q.songs) {
		return
	}
	q.songs = append(q.songs[:i], q.songs[i+1:]...)
	if q.anchor > i {
		q.anchor--
	}
}

// RemoveCurrent 移除当前播放曲：从历史与歌单中删除，队列回到无当前曲状态。
// 歌单中的位置被保留为锚点，顺序/循环模式下的 Next 会从原位置之后继续；
// 调用方通常紧接着调用 Next 跳到下一首。
func (q *Queue) RemoveCurrent() {
	if q.cur == nil {
		return
	}
	if idx := q.currentIndex(); idx >= 0 {
		q.songs = append(q.songs[:idx], q.songs[idx+1:]...)
		q.anchor = idx - 1
	} else {
		q.anchor = -1
	}
	q.history = append(q.history[:q.hpos], q.history[q.hpos+1:]...)
	q.hpos--
	q.cur = nil
}
