package clock

import (
	"sort"
	"sync"
	"time"
)

// Fake is a manually advanced clock. Timers fire only inside Advance.
type Fake struct {
	mu      sync.Mutex
	cond    *sync.Cond
	now     time.Time
	pending []*fakeTimer
}

// NewFake returns a fake clock starting at start.
func NewFake(start time.Time) *Fake {
	f := &Fake{now: start}
	f.cond = sync.NewCond(&f.mu)
	return f
}

// Now returns the fake time.
func (f *Fake) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

// BlockUntil returns once at least n timers or tickers are registered.
func (f *Fake) BlockUntil(n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for len(f.pending) < n {
		f.cond.Wait()
	}
}

// Advance moves time forward by d, firing every timer that falls due in order.
// AfterFunc callbacks run synchronously on the calling goroutine.
func (f *Fake) Advance(d time.Duration) {
	f.mu.Lock()
	target := f.now.Add(d)
	for {
		t := f.nextDue(target)
		if t == nil {
			break
		}
		f.now = t.when
		f.fire(t)
	}
	f.now = target
	f.mu.Unlock()
}

// nextDue returns the earliest pending timer due at or before target. Callers hold f.mu.
func (f *Fake) nextDue(target time.Time) *fakeTimer {
	if len(f.pending) == 0 || f.pending[0].when.After(target) {
		return nil
	}
	return f.pending[0]
}

// fire runs one due timer. Callers hold f.mu; it is released around callbacks.
func (f *Fake) fire(t *fakeTimer) {
	f.remove(t)
	switch {
	case t.fn != nil:
		fn := t.fn
		f.mu.Unlock()
		fn()
		f.mu.Lock()
	default:
		select {
		case t.ch <- t.when:
		default: // a slow reader misses the tick, as with time.Ticker
		}
		if t.period > 0 {
			t.when = t.when.Add(t.period)
			f.add(t)
		}
	}
}

func (f *Fake) add(t *fakeTimer) {
	f.pending = append(f.pending, t)
	sort.SliceStable(f.pending, func(i, j int) bool { return f.pending[i].when.Before(f.pending[j].when) })
	f.cond.Broadcast()
}

func (f *Fake) remove(t *fakeTimer) bool {
	for i, p := range f.pending {
		if p == t {
			f.pending = append(f.pending[:i], f.pending[i+1:]...)
			return true
		}
	}
	return false
}

type fakeTimer struct {
	f      *Fake
	when   time.Time
	period time.Duration
	ch     chan time.Time
	fn     func()
}

// NewTimer implements Clock.
func (f *Fake) NewTimer(d time.Duration) Timer {
	f.mu.Lock()
	defer f.mu.Unlock()
	t := &fakeTimer{f: f, when: f.now.Add(d), ch: make(chan time.Time, 1)}
	f.add(t)
	return t
}

// NewTicker implements Clock.
func (f *Fake) NewTicker(d time.Duration) Ticker {
	f.mu.Lock()
	defer f.mu.Unlock()
	t := &fakeTimer{f: f, when: f.now.Add(d), period: d, ch: make(chan time.Time, 1)}
	f.add(t)
	return fakeTicker{t}
}

// AfterFunc implements Clock.
func (f *Fake) AfterFunc(d time.Duration, fn func()) Timer {
	f.mu.Lock()
	defer f.mu.Unlock()
	t := &fakeTimer{f: f, when: f.now.Add(d), fn: fn}
	f.add(t)
	return t
}

// fakeTicker narrows a periodic fakeTimer to the Ticker interface.
type fakeTicker struct{ t *fakeTimer }

func (k fakeTicker) C() <-chan time.Time { return k.t.ch }
func (k fakeTicker) Stop()               { k.t.Stop() }

func (t *fakeTimer) C() <-chan time.Time {
	if t.fn != nil {
		return nil
	}
	return t.ch
}

func (t *fakeTimer) Stop() bool {
	t.f.mu.Lock()
	defer t.f.mu.Unlock()
	return t.f.remove(t)
}

func (t *fakeTimer) Reset(d time.Duration) bool {
	t.f.mu.Lock()
	defer t.f.mu.Unlock()
	active := t.f.remove(t)
	t.when = t.f.now.Add(d)
	t.f.add(t)
	return active
}
