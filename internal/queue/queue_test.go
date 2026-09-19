package queue

import (
	"testing"

	"molpe/internal/netease"
)

func songs(ids ...int64) []netease.Song {
	ss := make([]netease.Song, len(ids))
	for i, id := range ids {
		ss[i] = netease.Song{ID: id}
	}
	return ss
}

func mustNext(t *testing.T, q *Queue) netease.Song {
	t.Helper()
	s, ok := q.Next()
	if !ok {
		t.Fatal("Next() 返回 false，预期有下一首")
	}
	return s
}

func TestSequential(t *testing.T) {
	q := New(songs(1, 2, 3), ModeSequential, Options{})
	q.Play(netease.Song{ID: 1})
	if s := mustNext(t, q); s.ID != 2 {
		t.Fatalf("顺序播放下一首 = %d，预期 2", s.ID)
	}
	if s := mustNext(t, q); s.ID != 3 {
		t.Fatalf("顺序播放下一首 = %d，预期 3", s.ID)
	}
	if _, ok := q.Next(); ok {
		t.Fatal("歌单末尾应停止，Next() 返回 true")
	}
}

func TestLoop(t *testing.T) {
	q := New(songs(1, 2), ModeLoop, Options{})
	q.Play(netease.Song{ID: 2})
	if s := mustNext(t, q); s.ID != 1 {
		t.Fatalf("列表循环应回到开头，得到 %d", s.ID)
	}
	if s := mustNext(t, q); s.ID != 2 {
		t.Fatalf("列表循环下一首 = %d，预期 2", s.ID)
	}
}

func TestRandomExcludesCurrent(t *testing.T) {
	q := New(songs(1, 2), ModeRandom, Options{})
	q.Play(netease.Song{ID: 1})
	for range 50 {
		if s := mustNext(t, q); s.ID != 2 {
			t.Fatalf("随机播放不得选中当前曲，得到 %d", s.ID)
		}
		q.Play(netease.Song{ID: 1})
	}
}

func TestRandomSingleSongRepeats(t *testing.T) {
	q := New(songs(1), ModeRandom, Options{})
	q.Play(netease.Song{ID: 1})
	if s := mustNext(t, q); s.ID != 1 {
		t.Fatalf("单曲歌单应重复播放，得到 %d", s.ID)
	}
}

func TestPrevNextHistoryNavigation(t *testing.T) {
	q := New(songs(1, 2, 3), ModeSequential, Options{})
	q.Play(netease.Song{ID: 1})
	mustNext(t, q) // 2
	mustNext(t, q) // 3

	if s, ok := q.Prev(); !ok || s.ID != 2 {
		t.Fatalf("Prev() = %v, %v，预期 2, true", s.ID, ok)
	}
	if s, ok := q.Prev(); !ok || s.ID != 1 {
		t.Fatalf("Prev() = %v, %v，预期 1, true", s.ID, ok)
	}
	if _, ok := q.Prev(); ok {
		t.Fatal("已处于历史最早一首，Prev() 应返回 false")
	}
	// 沿历史前进，不触发模式选取
	if s := mustNext(t, q); s.ID != 2 {
		t.Fatalf("历史前进 = %d，预期 2", s.ID)
	}
	if s := mustNext(t, q); s.ID != 3 {
		t.Fatalf("历史前进 = %d，预期 3", s.ID)
	}
	// 历史已走完，按模式选取：3 已是末尾，顺序模式应停止
	if _, ok := q.Next(); ok {
		t.Fatal("历史走完且顺序模式到末尾，Next() 应返回 false")
	}
}

func TestPlayTruncatesForwardHistory(t *testing.T) {
	q := New(songs(1, 2, 3, 4), ModeSequential, Options{})
	q.Play(netease.Song{ID: 1})
	mustNext(t, q) // 2
	mustNext(t, q) // 3
	q.Prev()       // 2
	q.Prev()       // 1
	q.Play(netease.Song{ID: 4})
	// 截断后历史末尾是 4（歌单最后一首），顺序模式应停止
	if _, ok := q.Next(); ok {
		t.Fatal("Play 截断前进历史后，顺序模式在歌单末尾应停止")
	}
	// 且回退只能回到 1，不能再前进到被截断的 2/3
	if s, ok := q.Prev(); !ok || s.ID != 1 {
		t.Fatalf("Prev() = %v, %v，预期 1, true", s.ID, ok)
	}
	if s := mustNext(t, q); s.ID != 4 {
		t.Fatalf("沿历史前进应到 4，得到 %d", s.ID)
	}
}

func TestHistoryLimit(t *testing.T) {
	q := New(songs(1), ModeLoop, Options{HistoryLimit: 5})
	q.Play(netease.Song{ID: 1})
	for range 10 {
		mustNext(t, q)
	}
	back := 0
	for {
		if _, ok := q.Prev(); !ok {
			break
		}
		back++
	}
	if back != 4 { // 历史共 5 首，最多回退 4 次
		t.Fatalf("回退次数 = %d，预期 4（历史上限 5）", back)
	}
}

func TestNextUpPriorityAndFIFO(t *testing.T) {
	q := New(songs(1, 2, 3), ModeSequential, Options{})
	q.Play(netease.Song{ID: 1})
	q.PlayNext(netease.Song{ID: 100})
	q.PlayNext(netease.Song{ID: 200})
	if s := mustNext(t, q); s.ID != 100 {
		t.Fatalf("下一首应优先播放 nextUp 队首，得到 %d", s.ID)
	}
	if s := mustNext(t, q); s.ID != 200 {
		t.Fatalf("nextUp 应先进先出，得到 %d", s.ID)
	}
	if s := mustNext(t, q); s.ID != 1 { // nextUp 耗尽后回到模式选取；当前曲 200 不在歌单中，从歌单开头播
		t.Fatalf("nextUp 耗尽后应按模式选取，得到 %d", s.ID)
	}
}

func TestNextUpPreservedOnSwitch(t *testing.T) {
	q := New(songs(1, 2), ModeSequential, Options{})
	q.Play(netease.Song{ID: 1})
	q.PlayNext(netease.Song{ID: 100})
	q.SetPlaylist(songs(7, 8, 9))
	q.Play(netease.Song{ID: 7})
	if s := mustNext(t, q); s.ID != 100 {
		t.Fatalf("切换歌单后 nextUp 应保留，得到 %d", s.ID)
	}
}

func TestPrevDoesNotConsumeNextUp(t *testing.T) {
	q := New(songs(1, 2), ModeSequential, Options{})
	q.Play(netease.Song{ID: 1})
	mustNext(t, q) // 2
	q.PlayNext(netease.Song{ID: 100})
	if s, ok := q.Prev(); !ok || s.ID != 1 {
		t.Fatalf("Prev() = %v, %v，预期 1, true", s.ID, ok)
	}
	// 回退后 Next 优先沿历史前进，仍不消费 nextUp
	if s := mustNext(t, q); s.ID != 2 {
		t.Fatalf("回退后 Next 应沿历史前进到 2，得到 %d", s.ID)
	}
	if s := mustNext(t, q); s.ID != 100 {
		t.Fatalf("历史走完后 Next 才消费 nextUp，得到 %d", s.ID)
	}
}

func TestHistoryKeptAcrossPlaylistSwitch(t *testing.T) {
	q := New(songs(1, 2), ModeSequential, Options{})
	q.Play(netease.Song{ID: 1})
	mustNext(t, q) // 2
	q.SetPlaylist(songs(7, 8))
	q.Play(netease.Song{ID: 7})
	if s, ok := q.Prev(); !ok || s.ID != 2 {
		t.Fatalf("历史应跨歌单保留，Prev() = %v，预期 2", s.ID)
	}
}

func TestEmptyPlaylist(t *testing.T) {
	q := New(nil, ModeLoop, Options{})
	if _, ok := q.Next(); ok {
		t.Fatal("空歌单 Next() 应返回 false")
	}
}

func TestSnapshotRestore(t *testing.T) {
	q := New(songs(1, 2, 3), ModeRandom, Options{})
	q.Play(netease.Song{ID: 1})
	q.Play(netease.Song{ID: 2}) // 历史 [1, 2]
	q.PlayNext(netease.Song{ID: 100})
	q.PlayNext(netease.Song{ID: 200})
	q.Prev() // 回退到 1

	r := New(nil, ModeSequential, Options{})
	r.Restore(q.Snapshot())

	if r.Mode() != ModeRandom {
		t.Fatalf("恢复后模式 = %v，预期 ModeRandom", r.Mode())
	}
	if cur, ok := r.Current(); !ok || cur.ID != 1 {
		t.Fatalf("恢复后当前曲 = %v, %v，预期 1, true", cur.ID, ok)
	}
	if got := len(r.Songs()); got != 3 {
		t.Fatalf("恢复后歌单长度 = %d，预期 3", got)
	}
	if got := r.NextUp(); len(got) != 2 || got[0].ID != 100 || got[1].ID != 200 {
		t.Fatalf("恢复后下一首队列 = %v，预期 [100 200]", got)
	}
	// 恢复后行为延续：历史前进
	if s := mustNext(t, r); s.ID != 2 {
		t.Fatalf("恢复后 Next = %d，预期沿历史前进到 2", s.ID)
	}
}

func TestRestoreSanitizes(t *testing.T) {
	r := New(nil, ModeSequential, Options{})
	r.Restore(State{Mode: Mode(99), Hpos: 5})
	if r.Mode() != ModeLoop {
		t.Fatalf("非法模式应收敛为 ModeLoop，得到 %v", r.Mode())
	}
	if _, ok := r.Current(); ok {
		t.Fatal("空历史下 Current 应为 false")
	}
	if _, ok := r.Prev(); ok {
		t.Fatal("空历史下 Prev 应为 false")
	}
}

func TestSetMode(t *testing.T) {
	q := New(songs(1, 2), ModeSequential, Options{})
	q.Play(netease.Song{ID: 2})
	if _, ok := q.Next(); ok {
		t.Fatal("顺序模式末尾应停止")
	}
	q.Prev() // 回到 1？不：Prev 到历史上一首。此处历史只有 2，Prev 失败
	q.SetMode(ModeLoop)
	if s := mustNext(t, q); s.ID != 1 {
		t.Fatalf("切换为列表循环后应回到开头，得到 %d", s.ID)
	}
}
