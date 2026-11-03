package player

import (
	"tinglan/native/internal/huawei"
	"testing"
)

func TestQueueModesAndDuplicates(t *testing.T) {
	q := Queue{Mode: "off"}
	q.Replace([]huawei.Track{{ID: "same"}, {ID: "same"}, {ID: "third"}}, 0)
	if !q.Step(1, false) || q.Index != 1 {
		t.Fatal("duplicate item collapsed")
	}
	q.Select(2)
	if q.Step(1, true) {
		t.Fatal("off mode wrapped")
	}
	q.Mode = "all"
	if !q.Step(1, true) || q.Index != 0 {
		t.Fatal("all mode did not wrap")
	}
	q.Mode = "one"
	q.Position = 10
	if !q.Step(1, true) || q.Index != 0 || q.Position != 0 {
		t.Fatal("one did not repeat")
	}
	if !q.Step(1, false) || q.Index != 1 {
		t.Fatal("manual next was blocked by repeat one")
	}
}
func TestShuffleRetainsActiveAndPrevious(t *testing.T) {
	q := Queue{}
	q.Replace([]huawei.Track{{ID: "a"}, {ID: "b"}, {ID: "c"}}, 1)
	q.SetShuffle(true)
	if q.Current().ID != "b" {
		t.Fatal("shuffle replaced current")
	}
	if !q.Step(1, false) {
		t.Fatal("next failed")
	}
	if !q.Step(-1, false) || q.Current().ID != "b" {
		t.Fatal("previous ignored actual history")
	}
	seen := make(map[int]bool)
	for _, i := range q.order {
		if seen[i] {
			t.Fatal("duplicate shuffle item")
		}
		seen[i] = true
	}
}

func TestEnqueuePreservesQueueAndTraversal(t *testing.T) {
	for _, shuffle := range []bool{false, true} {
		q := Queue{Mode: "all", Shuffle: shuffle}
		q.Replace([]huawei.Track{{ID: "a"}, {ID: "b"}, {ID: "c"}}, 1)
		q.Enqueue(huawei.Track{ID: "new"})
		if len(q.Tracks) != 4 || q.Current().ID != "new" || q.Tracks[0].ID != "a" || q.Tracks[1].ID != "b" || q.Tracks[3].ID != "c" {
			t.Fatal("existing queue replaced", q.Tracks)
		}
		if !q.Step(-1, false) || q.Current().ID != "b" {
			t.Fatal("previous song lost")
		}
		q.Enqueue(huawei.Track{ID: "new"})
		if len(q.Tracks) != 4 || q.Current().ID != "new" {
			t.Fatal("duplicate enqueued")
		}
	}
	q := Queue{}
	q.Enqueue(huawei.Track{ID: "first"})
	if q.Current() == nil || q.Current().ID != "first" {
		t.Fatal("empty queue not initialized")
	}
}
