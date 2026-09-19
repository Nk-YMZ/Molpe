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
	// ModeRandom 随机播放：下一首在歌单中除当前歌曲外等概率选取。
	ModeRandom
)

const defaultHistoryLimit = 100

// Options 队列行为的可调参数，为后续配置化预留。
type Options struct {
	// HistoryLimit 历史队列上限；<= 0 时使用默认值 100。
	HistoryLimit int
}

// Queue 播放队列。不是并发安全的，由调用方保证单 goroutine 访问。
type Queue struct {
	mode Mode
	opts Options

	songs []netease.Song // 当前歌单

	history []netease.Song // 实际播放过的歌曲，按时间顺序
	hpos    int            // 当前曲在 history 中的下标，-1 表示尚未播放

	nextUp []netease.Song // “下一首播放”队列，先进先出
}

// New 创建播放队列。songs 为初始歌单（可为空）。
func New(songs []netease.Song, mode Mode, opts Options) *Queue {
	q := &Queue{mode: mode, opts: opts, hpos: -1}
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
	if q.hpos < 0 || q.hpos >= len(q.history) {
		return netease.Song{}, false
	}
	return q.history[q.hpos], true
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
	return q.history[q.hpos], true
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
		return q.history[q.hpos], true
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
		return q.songs[q.randomIndex(cur)], true
	default: // ModeSequential
		if cur+1 >= len(q.songs) {
			return netease.Song{}, false
		}
		return q.songs[cur+1], true
	}
}

// currentIndex 返回当前曲在歌单中的下标；未播放或当前曲不在歌单中时为 -1。
func (q *Queue) currentIndex() int {
	cur, ok := q.Current()
	if !ok {
		return -1
	}
	for i, s := range q.songs {
		if s.ID == cur.ID {
			return i
		}
	}
	return -1
}

// randomIndex 在歌单内等概率选取一个下标；当前曲在歌单中时排除当前曲。
// 歌单仅一首歌时重复播放该曲。
func (q *Queue) randomIndex(cur int) int {
	n := len(q.songs)
	if n <= 1 || cur < 0 {
		return rand.IntN(n)
	}
	i := rand.IntN(n - 1)
	if i >= cur {
		i++
	}
	return i
}
