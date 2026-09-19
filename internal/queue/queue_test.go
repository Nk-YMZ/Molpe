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

func TestHistoryAndPos(t *testing.T) {
	q := New(songs(1, 2, 3), ModeLoop, Options{})
	if q.HistoryPos() != -1 {
		t.Fatal("未播放时 HistoryPos 应为 -1")
	}
	q.Play(netease.Song{ID: 2})
	h := q.History()
	if len(h) != 1 || h[0].ID != 2 {
		t.Fatalf("History = %v，预期 [2]", h)
	}
	h[0].ID = 99 // 副本，不影响内部状态
	if q.History()[0].ID != 2 {
		t.Fatal("History 应返回副本")
	}
	if q.HistoryPos() != 0 {
		t.Fatalf("HistoryPos = %d，预期 0", q.HistoryPos())
	}
}

func TestUpcoming(t *testing.T) {
	q := New(songs(1, 2, 3), ModeLoop, Options{})
	if _, u := q.Upcoming(); len(u) != 0 {
		t.Fatal("未播放时 Upcoming 应为空")
	}
	q.Play(netease.Song{ID: 2})
	base, u := q.Upcoming()
	if base != 2 || len(u) != 1 || u[0].ID != 3 {
		t.Fatalf("Upcoming = (%d, %v)，预期 (2, [3])", base, u)
	}
	q.Play(netease.Song{ID: 3})
	if _, u := q.Upcoming(); len(u) != 0 {
		t.Fatal("歌单末尾 Upcoming 应为空（不考虑循环回卷）")
	}
}

func TestRemoveHistory(t *testing.T) {
	q := New(songs(1, 2, 3, 4), ModeSequential, Options{})
	for _, id := range []int64{1, 2, 3} {
		q.Play(netease.Song{ID: id})
	}
	q.RemoveHistory(0) // 删除当前曲之前的历史
	if q.HistoryPos() != 1 {
		t.Fatalf("删除前置历史后 HistoryPos = %d，预期 1", q.HistoryPos())
	}
	if s, ok := q.Current(); !ok || s.ID != 3 {
		t.Fatal("删除前置历史不应影响当前曲")
	}
	q.RemoveHistory(q.HistoryPos()) // 当前曲不可通过 RemoveHistory 删除
	if len(q.History()) != 2 {
		t.Fatal("RemoveHistory 不得删除当前曲")
	}
}

func TestRemoveNextUp(t *testing.T) {
	q := New(songs(1, 2), ModeLoop, Options{})
	q.PlayNext(netease.Song{ID: 1})
	q.PlayNext(netease.Song{ID: 2})
	q.RemoveNextUp(0)
	if nu := q.NextUp(); len(nu) != 1 || nu[0].ID != 2 {
		t.Fatalf("NextUp = %v，预期 [2]", nu)
	}
	q.RemoveNextUp(5) // 越界为无操作
	if len(q.NextUp()) != 1 {
		t.Fatal("越界 RemoveNextUp 应为无操作")
	}
}

func TestRemovePlaylist(t *testing.T) {
	q := New(songs(1, 2, 3), ModeSequential, Options{})
	q.Play(netease.Song{ID: 1})
	q.RemovePlaylist(1) // 删除当前曲之后的 2
	if s := mustNext(t, q); s.ID != 3 {
		t.Fatalf("删除后续歌单曲后 Next = %d，预期 3", s.ID)
	}
	if _, ok := q.Next(); ok {
		t.Fatal("删除后顺序模式应在新末尾停止")
	}
}

func TestRemoveCurrent(t *testing.T) {
	q := New(songs(1, 2, 3), ModeSequential, Options{})
	q.Play(netease.Song{ID: 2})
	q.RemoveCurrent()
	if _, ok := q.Current(); ok {
		t.Fatal("RemoveCurrent 后应无当前曲")
	}
	if q.HistoryPos() != -1 {
		t.Fatalf("RemoveCurrent 后 HistoryPos = %d，预期 -1", q.HistoryPos())
	}
	// 顺序模式应从原位置之后继续（2 被移出歌单，下一首为 3）。
	if s := mustNext(t, q); s.ID != 3 {
		t.Fatalf("RemoveCurrent 后 Next = %d，预期 3", s.ID)
	}
	if len(q.Songs()) != 2 {
		t.Fatalf("RemoveCurrent 应将当前曲移出歌单，歌单 = %v", q.Songs())
	}
}

func TestRemoveCurrentThenLoop(t *testing.T) {
	q := New(songs(1, 2, 3), ModeLoop, Options{})
	q.Play(netease.Song{ID: 3}) // 末尾
	q.RemoveCurrent()
	// 循环模式从原位置之后继续：3 已删除，回卷到 1。
	if s := mustNext(t, q); s.ID != 1 {
		t.Fatalf("RemoveCurrent 后循环 Next = %d，预期 1", s.ID)
	}
}

func TestPosition(t *testing.T) {
	q := New(songs(1, 2, 3), ModeLoop, Options{})
	if _, _, ok := q.Position(); ok {
		t.Fatal("未播放时 Position 应为 false")
	}
	q.Play(netease.Song{ID: 2})
	if pos, total, ok := q.Position(); !ok || pos != 2 || total != 3 {
		t.Fatalf("Position = (%d, %d, %v)，预期 (2, 3, true)", pos, total, ok)
	}
	// 当前曲不在歌单中时为 false。
	q.Play(netease.Song{ID: 9})
	if _, _, ok := q.Position(); ok {
		t.Fatal("当前曲不在歌单中时 Position 应为 false")
	}
}
