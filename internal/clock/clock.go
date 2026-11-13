// Package clock abstracts time so tests can drive it deterministically. It is the
// only production package allowed to read or wait on real time.
package clock

import "time"

// Clock is the source of time and timers.
type Clock interface {
	Now() time.Time
	NewTimer(d time.Duration) Timer
	NewTicker(d time.Duration) Ticker
	AfterFunc(d time.Duration, f func()) Timer
}

// Timer mirrors time.Timer. C is nil for timers made by AfterFunc.
type Timer interface {
	C() <-chan time.Time
	Stop() bool
	Reset(d time.Duration) bool
}

// Ticker mirrors time.Ticker.
type Ticker interface {
	C() <-chan time.Time
	Stop()
}

type system struct{}

// Real returns the system clock.
func Real() Clock { return system{} }

func (system) Now() time.Time { return time.Now() }

func (system) NewTimer(d time.Duration) Timer { return realTimer{time.NewTimer(d)} }

func (system) NewTicker(d time.Duration) Ticker { return realTicker{time.NewTicker(d)} }

func (system) AfterFunc(d time.Duration, f func()) Timer { return realTimer{time.AfterFunc(d, f)} }

type realTimer struct{ t *time.Timer }

func (r realTimer) C() <-chan time.Time        { return r.t.C }
func (r realTimer) Stop() bool                 { return r.t.Stop() }
func (r realTimer) Reset(d time.Duration) bool { return r.t.Reset(d) }

type realTicker struct{ t *time.Ticker }

func (r realTicker) C() <-chan time.Time { return r.t.C }
func (r realTicker) Stop()               { r.t.Stop() }
