package ws

import (
	"container/list"
	"context"
	"sync"
)

// item is one outbound frame.
type item struct {
	ch        int
	droppable bool
	data      []byte
}

// queue is the bounded outbound queue of a connection. Pushing never blocks.
type queue struct {
	mu     sync.Mutex
	cap    int
	items  *list.List
	drops  map[int]int
	closed bool
	wake   chan struct{}
}

func newQueue(capacity int) *queue {
	return &queue{cap: capacity, items: list.New(), drops: map[int]int{}, wake: make(chan struct{}, 1)}
}

func (q *queue) signal() {
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

// evict removes the oldest droppable item (of channel ch when ch >= 0, of any channel otherwise)
// and counts the drop. It reports whether one was removed. Callers hold q.mu.
func (q *queue) evict(ch int) bool {
	for e := q.items.Front(); e != nil; e = e.Next() {
		it := e.Value.(item)
		if it.droppable && (ch < 0 || it.ch == ch) {
			q.items.Remove(e)
			q.drops[it.ch]++
			return true
		}
	}
	return false
}

// push adds a frame that must be delivered. When the queue is full it makes room by evicting the
// oldest droppable frame; with none to evict it returns ErrSlowConsumer.
func (q *queue) push(it item) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return ErrClosed
	}
	if q.items.Len() >= q.cap && !q.evict(-1) {
		return ErrSlowConsumer
	}
	q.items.PushBack(it)
	q.signal()
	return nil
}

// pushDroppable adds a frame that may be dropped. When the queue is full the oldest droppable frame
// of the same channel is evicted; with none, the new frame itself is dropped.
func (q *queue) pushDroppable(it item) {
	it.droppable = true
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return
	}
	if q.items.Len() >= q.cap && !q.evict(it.ch) {
		q.drops[it.ch]++
		return
	}
	q.items.PushBack(it)
	q.signal()
}

// pushForce adds a control frame regardless of capacity.
func (q *queue) pushForce(it item) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return
	}
	q.items.PushBack(it)
	q.signal()
}

// dropped returns how many frames of the channel were dropped.
func (q *queue) dropped(ch int) int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.drops[ch]
}

// close stops the queue: next returns false from now on and pushes are refused.
func (q *queue) close() {
	q.mu.Lock()
	q.closed = true
	q.mu.Unlock()
	q.signal()
}

// next blocks for the next frame. It returns false when the queue is closed or ctx is done.
func (q *queue) next(ctx context.Context) (item, bool) {
	for {
		q.mu.Lock()
		if q.closed {
			q.mu.Unlock()
			return item{}, false
		}
		if e := q.items.Front(); e != nil {
			q.items.Remove(e)
			q.mu.Unlock()
			return e.Value.(item), true
		}
		q.mu.Unlock()
		select {
		case <-q.wake:
		case <-ctx.Done():
			return item{}, false
		}
	}
}
