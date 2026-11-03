package player

import (
	"tinglan/native/internal/huawei"
	"math/rand/v2"
)

type Queue struct {
	Tracks     []huawei.Track `json:"tracks"`
	Index      int            `json:"index"`
	Position   float64        `json:"positionSeconds"`
	Mode       string         `json:"repeatMode"`
	Shuffle    bool           `json:"shuffle"`
	order      []int
	cursor     int
	history    []int
	Generation uint64 `json:"-"`
}

func (q *Queue) Replace(tracks []huawei.Track, index int) {
	q.Tracks = append([]huawei.Track(nil), tracks...)
	q.Index = max(0, min(index, len(tracks)-1))
	q.Position = 0
	q.history = nil
	q.reorder()
	q.Generation++
}
func (q *Queue) reorder() {
	q.order = make([]int, len(q.Tracks))
	for i := range q.order {
		q.order[i] = i
	}
	if q.Shuffle && len(q.order) > 0 {
		rand.Shuffle(len(q.order), func(i, j int) { q.order[i], q.order[j] = q.order[j], q.order[i] })
		for i, v := range q.order {
			if v == q.Index {
				q.order[0], q.order[i] = q.order[i], q.order[0]
				break
			}
		}
	}
	for i, v := range q.order {
		if v == q.Index {
			q.cursor = i
			break
		}
	}
}
func (q *Queue) SetShuffle(value bool) { q.Shuffle = value; q.history = nil; q.reorder() }

// Enqueue selects an existing entry or inserts a new song after the current one.
func (q *Queue) Enqueue(track huawei.Track) {
	for i, existing := range q.Tracks {
		if existing.ID == track.ID {
			q.Select(i)
			return
		}
	}
	index := min(max(0, q.Index+1), len(q.Tracks))
	q.Tracks = append(q.Tracks, huawei.Track{})
	copy(q.Tracks[index+1:], q.Tracks[index:])
	q.Tracks[index] = track
	for i, entry := range q.order {
		if entry >= index {
			q.order[i]++
		}
	}
	for i, entry := range q.history {
		if entry >= index {
			q.history[i]++
		}
	}
	// Keep the existing shuffle traversal, placing the requested song next.
	next := min(q.cursor+1, len(q.order))
	q.order = append(q.order, 0)
	copy(q.order[next+1:], q.order[next:])
	q.order[next] = index
	q.Select(index)
}
func (q *Queue) Remove(index int) bool {
	if index < 0 || index >= len(q.Tracks) {
		return false
	}
	current := index == q.Index
	q.Tracks = append(q.Tracks[:index], q.Tracks[index+1:]...)
	if index < q.Index {
		q.Index--
	}
	q.Index = max(0, min(q.Index, len(q.Tracks)-1))
	if current {
		q.Position = 0
	}
	q.history = nil
	q.reorder()
	q.Generation++
	return current
}
func (q *Queue) Current() *huawei.Track {
	if q.Index < 0 || q.Index >= len(q.Tracks) {
		return nil
	}
	return &q.Tracks[q.Index]
}
func (q *Queue) Select(index int) bool {
	if index < 0 || index >= len(q.Tracks) {
		return false
	}
	q.history = append(q.history, q.Index)
	q.Index = index
	q.Position = 0
	q.Generation++
	for i, v := range q.order {
		if v == index {
			q.cursor = i
			break
		}
	}
	return true
}
func (q *Queue) Step(delta int, ended bool) bool {
	if len(q.Tracks) == 0 {
		return false
	}
	if ended && q.Mode == "one" {
		q.Position = 0
		q.Generation++
		return true
	}
	if delta < 0 && q.Shuffle && len(q.history) > 0 {
		index := q.history[len(q.history)-1]
		q.history = q.history[:len(q.history)-1]
		q.Index = index
		q.Position = 0
		q.Generation++
		for i, v := range q.order {
			if v == index {
				q.cursor = i
				break
			}
		}
		return true
	}
	next := q.cursor + delta
	if next < 0 || next >= len(q.order) {
		if q.Mode == "off" || q.Mode == "" {
			return false
		}
		next = (next%len(q.order) + len(q.order)) % len(q.order)
	}
	return q.Select(q.order[next])
}
