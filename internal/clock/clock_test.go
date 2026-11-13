package clock_test

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdnix/kubelook/internal/clock"
	"github.com/bdnix/kubelook/internal/testutil"
)

var t0 = time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)

func TestReal_Now_IsCloseToSystemTime(t *testing.T) {
	d := clock.Real().Now().Sub(time.Now())
	assert.Less(t, d.Abs(), time.Second)
}

func TestReal_TimerTickerAndAfterFunc_Work(t *testing.T) {
	c := clock.Real()

	tm := c.NewTimer(time.Millisecond)
	testutil.Recv(t, tm.C())
	assert.False(t, tm.Stop())
	assert.False(t, tm.Reset(time.Millisecond))
	testutil.Recv(t, tm.C())

	tk := c.NewTicker(time.Millisecond)
	testutil.Recv(t, tk.C())
	tk.Stop()

	done := make(chan struct{})
	c.AfterFunc(time.Millisecond, func() { close(done) })
	testutil.Recv(t, done)
}

func TestFake_Now_AdvancesWithAdvance(t *testing.T) {
	f := clock.NewFake(t0)
	assert.Equal(t, t0, f.Now())
	f.Advance(90 * time.Second)
	assert.Equal(t, t0.Add(90*time.Second), f.Now())
}

func TestFakeTimer_FiresOnceWhenDue(t *testing.T) {
	f := clock.NewFake(t0)
	tm := f.NewTimer(10 * time.Second)

	f.Advance(9 * time.Second)
	testutil.NoRecv(t, tm.C())
	f.Advance(time.Second)
	assert.Equal(t, t0.Add(10*time.Second), testutil.Recv(t, tm.C()))
	f.Advance(time.Minute)
	testutil.NoRecv(t, tm.C())
}

func TestFakeTimer_StopBeforeFiring_ReturnsTrueAndNeverFires(t *testing.T) {
	f := clock.NewFake(t0)
	tm := f.NewTimer(10 * time.Second)
	assert.True(t, tm.Stop())
	assert.False(t, tm.Stop())
	f.Advance(time.Minute)
	testutil.NoRecv(t, tm.C())
}

func TestFakeTimer_Reset_ReArms(t *testing.T) {
	f := clock.NewFake(t0)
	tm := f.NewTimer(10 * time.Second)
	f.Advance(10 * time.Second)
	testutil.Recv(t, tm.C())

	assert.False(t, tm.Reset(5*time.Second))
	f.Advance(5 * time.Second)
	assert.Equal(t, t0.Add(15*time.Second), testutil.Recv(t, tm.C()))

	tm2 := f.NewTimer(time.Hour)
	assert.True(t, tm2.Reset(time.Second))
	f.Advance(time.Second)
	testutil.Recv(t, tm2.C())
}

func TestFakeTicker_TicksEveryPeriodAndDropsForSlowReaders(t *testing.T) {
	f := clock.NewFake(t0)
	tk := f.NewTicker(5 * time.Second)

	for i := 1; i <= 3; i++ {
		f.Advance(5 * time.Second)
		assert.Equal(t, t0.Add(time.Duration(i)*5*time.Second), testutil.Recv(t, tk.C()))
	}

	f.Advance(12 * time.Second) // two ticks due, buffer of one: the slow reader misses one
	testutil.Recv(t, tk.C())
	testutil.NoRecv(t, tk.C())

	tk.Stop()
	f.Advance(time.Minute)
	testutil.NoRecv(t, tk.C())
}

func TestFakeAfterFunc_RunsSynchronouslyInsideAdvance(t *testing.T) {
	f := clock.NewFake(t0)
	var ranAt time.Time
	tm := f.AfterFunc(3*time.Second, func() { ranAt = f.Now() })

	f.Advance(2 * time.Second)
	assert.True(t, ranAt.IsZero())
	f.Advance(time.Second)
	assert.Equal(t, t0.Add(3*time.Second), ranAt)
	assert.Nil(t, tm.C())
	assert.False(t, tm.Stop())
}

func TestFake_AdvanceFiresInOrderAndSetsNowPerTimer(t *testing.T) {
	f := clock.NewFake(t0)
	var seen []time.Duration
	for _, d := range []time.Duration{3, 1, 2} {
		f.AfterFunc(d*time.Second, func() { seen = append(seen, f.Now().Sub(t0)) })
	}
	f.Advance(10 * time.Second)
	assert.Equal(t, []time.Duration{time.Second, 2 * time.Second, 3 * time.Second}, seen)
	assert.Equal(t, t0.Add(10*time.Second), f.Now())
}

func TestFake_BlockUntil_ReturnsOnceTimersAreRegistered(t *testing.T) {
	f := clock.NewFake(t0)
	done := make(chan struct{})
	go func() {
		f.BlockUntil(2)
		close(done)
	}()
	f.NewTimer(time.Second)
	testutil.NoRecv(t, done)
	f.NewTicker(time.Second)
	testutil.Recv(t, done)
}

func TestFake_BlockUntil_AlreadySatisfied(t *testing.T) {
	f := clock.NewFake(t0)
	f.BlockUntil(0)
	f.NewTimer(time.Second)
	f.BlockUntil(1)
}

func TestFake_Concurrent_AdvanceAndNewTimer_DoNotRace(t *testing.T) {
	f := clock.NewFake(t0)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				tm := f.NewTimer(time.Second)
				tm.Stop()
				f.NewTicker(time.Second).Stop()
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				f.Advance(time.Second)
			}
		}()
	}
	wg.Wait()
	require.False(t, f.Now().Before(t0))
}
