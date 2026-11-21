package telemetry_test

import (
	"math"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdnix/kubelook/internal/clock"
	"github.com/bdnix/kubelook/internal/telemetry"
)

type guardEnv struct {
	clk     *clock.Fake
	usage   atomic.Uint64
	limit   atomic.Uint64
	samples atomic.Int32
	refused []string
	guard   *telemetry.MemGuard
}

func newGuard(percent int) *guardEnv {
	e := &guardEnv{clk: clock.NewFake(time.Unix(1000, 0))}
	e.limit.Store(1000)
	e.guard = telemetry.NewMemGuard(telemetry.MemGuardDeps{
		Clock: e.clk, Percent: percent,
		Usage:   func() uint64 { e.samples.Add(1); return e.usage.Load() },
		Limit:   e.limit.Load,
		Refused: func(kind string) { e.refused = append(e.refused, kind) },
	})
	return e
}

func TestMemGuard_BelowThreshold_NotBusy_AtOrAbove_Busy(t *testing.T) {
	e := newGuard(80)
	e.usage.Store(799)
	assert.False(t, e.guard.Busy())
	e.clk.Advance(time.Second)
	e.usage.Store(800)
	assert.True(t, e.guard.Busy(), "at the threshold counts as busy")
	e.clk.Advance(time.Second)
	e.usage.Store(1000)
	assert.True(t, e.guard.Busy())
	e.clk.Advance(time.Second)
	e.usage.Store(10)
	assert.False(t, e.guard.Busy(), "recovers once usage falls")
}

func TestMemGuard_UnknownLimit_NeverBusy(t *testing.T) {
	e := newGuard(80)
	e.limit.Store(0)
	e.usage.Store(math.MaxUint64)
	assert.False(t, e.guard.Busy())
}

func TestMemGuard_SampledAtMostOncePerSecond(t *testing.T) {
	e := newGuard(80)
	e.usage.Store(900)
	for range 5 {
		assert.True(t, e.guard.Busy())
	}
	assert.EqualValues(t, 1, e.samples.Load())
	e.usage.Store(0)
	e.clk.Advance(999 * time.Millisecond)
	assert.True(t, e.guard.Busy(), "still the sample of the last second")
	e.clk.Advance(time.Millisecond)
	assert.False(t, e.guard.Busy())
	assert.EqualValues(t, 2, e.samples.Load())
}

func TestMemGuard_NoPercent_DefaultsToEighty(t *testing.T) {
	e := newGuard(0)
	e.usage.Store(799)
	assert.False(t, e.guard.Busy())
	e.clk.Advance(time.Second)
	e.usage.Store(800)
	assert.True(t, e.guard.Busy())
}

func TestMemGuard_For_CountsOneRefusalPerBusyAnswerAndNothingElse(t *testing.T) {
	e := newGuard(80)
	watch, exec := e.guard.For("watch"), e.guard.For("exec")
	e.usage.Store(100)
	assert.False(t, watch.Busy())
	assert.Empty(t, e.refused)
	e.clk.Advance(time.Second)
	e.usage.Store(900)
	assert.True(t, watch.Busy())
	assert.True(t, exec.Busy())
	assert.True(t, watch.Busy())
	assert.Equal(t, []string{"watch", "exec", "watch"}, e.refused)
	assert.True(t, e.guard.Busy(), "the guard itself does not count")
	assert.Len(t, e.refused, 3)
}

func TestMemGuard_NoRefusedCallback_IsFine(t *testing.T) {
	g := telemetry.NewMemGuard(telemetry.MemGuardDeps{
		Clock: clock.NewFake(time.Unix(0, 0)), Percent: 50,
		Usage: func() uint64 { return 99 }, Limit: func() uint64 { return 100 },
	})
	assert.True(t, g.For("helm").Busy())
}

func writeLimit(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "memory.max")
	require.NoError(t, os.WriteFile(p, []byte(content), 0o600))
	return p
}

func TestCgroupLimit_ParsesTheFile(t *testing.T) {
	assert.EqualValues(t, 268435456, telemetry.CgroupLimit(writeLimit(t, "268435456\n"), func() int64 { return 1 })())
	assert.EqualValues(t, 5, telemetry.CgroupLimit(writeLimit(t, "5"), func() int64 { return 1 })())
}

func TestCgroupLimit_MaxFallsBackToTheGoMemoryLimit(t *testing.T) {
	assert.EqualValues(t, 900, telemetry.CgroupLimit(writeLimit(t, "max\n"), func() int64 { return 900 })())
	assert.EqualValues(t, 0, telemetry.CgroupLimit(writeLimit(t, "max\n"), func() int64 { return math.MaxInt64 })(), "no Go limit either: unknown")
}

func TestCgroupLimit_MissingOrUnreadableFile_FallsBackLikeMax(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope")
	assert.EqualValues(t, 700, telemetry.CgroupLimit(missing, func() int64 { return 700 })())
	assert.EqualValues(t, 0, telemetry.CgroupLimit(writeLimit(t, "garbage"), func() int64 { return math.MaxInt64 })())
	assert.EqualValues(t, 0, telemetry.CgroupLimit(writeLimit(t, "-5"), func() int64 { return math.MaxInt64 })())
}

func TestRuntimeUsage_IsPositiveAndBelowTheAddressSpace(t *testing.T) {
	u := telemetry.RuntimeUsage()
	assert.Positive(t, u)
	assert.Less(t, u, uint64(1)<<47)
}
