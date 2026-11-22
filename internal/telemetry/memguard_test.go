package telemetry_test

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdnix/kubelook/internal/clock"
	"github.com/bdnix/kubelook/internal/telemetry"
)

type memEnv struct {
	clk          *clock.Fake
	usage, limit uint64
	reads        int
	guard        *telemetry.MemGuard
}

func newMem(percent int) *memEnv {
	e := &memEnv{clk: clock.NewFake(time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)), limit: 1000}
	e.guard = telemetry.NewMemGuard(telemetry.MemGuardDeps{
		Clock: e.clk, Percent: percent,
		Usage: func() uint64 { e.reads++; return e.usage },
		Limit: func() uint64 { return e.limit },
	})
	return e
}

func TestMemGuard_BelowTheThreshold_IsNotBusy_AtOrAboveItIs(t *testing.T) {
	for _, tc := range []struct {
		usage uint64
		busy  bool
	}{{0, false}, {799, false}, {800, true}, {801, true}, {5000, true}} {
		e := newMem(80)
		e.usage = tc.usage
		assert.Equal(t, tc.busy, e.guard.Busy(), "usage %d of 1000 at 80%%", tc.usage)
	}
}

func TestMemGuard_UnknownLimit_IsNeverBusy(t *testing.T) {
	e := newMem(80)
	e.limit, e.usage = 0, math.MaxUint64
	assert.False(t, e.guard.Busy())
}

func TestMemGuard_HugeLimit_DoesNotOverflow(t *testing.T) {
	e := newMem(80)
	e.limit, e.usage = math.MaxInt64, 1<<40
	assert.False(t, e.guard.Busy())
	e.clk.Advance(time.Second)
	e.usage = math.MaxInt64
	assert.True(t, e.guard.Busy())
}

func TestMemGuard_IsSampledAtMostOncePerSecond(t *testing.T) {
	e := newMem(80)
	e.usage = 100
	assert.False(t, e.guard.Busy())
	assert.False(t, e.guard.Busy())
	assert.Equal(t, 1, e.reads)

	e.usage = 900
	e.clk.Advance(999 * time.Millisecond)
	assert.False(t, e.guard.Busy(), "still the sample of a moment ago")
	assert.Equal(t, 1, e.reads)

	e.clk.Advance(time.Millisecond)
	assert.True(t, e.guard.Busy())
	assert.Equal(t, 2, e.reads)
}

func TestMemGuard_NonPositivePercent_UsesTheDefaultOfEighty(t *testing.T) {
	e := newMem(0)
	e.usage = 799
	assert.False(t, e.guard.Busy())
	e.clk.Advance(time.Second)
	e.usage = 800
	assert.True(t, e.guard.Busy())
}

func writeMax(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "memory.max")
	require.NoError(t, os.WriteFile(p, []byte(content), 0o600))
	return p
}

func TestCgroupLimit_ReadsTheFile(t *testing.T) {
	read := telemetry.CgroupLimit(writeMax(t, "536870912\n"), func() int64 { return 1 })
	assert.EqualValues(t, 536870912, read())
}

func TestCgroupLimit_Max_FallsBackToTheGoMemoryLimit(t *testing.T) {
	read := telemetry.CgroupLimit(writeMax(t, "max\n"), func() int64 { return 12345 })
	assert.EqualValues(t, 12345, read())
}

func TestCgroupLimit_GoLimitMaxInt64_MeansNoLimit(t *testing.T) {
	read := telemetry.CgroupLimit(writeMax(t, "max\n"), func() int64 { return math.MaxInt64 })
	assert.EqualValues(t, 0, read())
}

func TestCgroupLimit_MissingOrUnreadableFile_FallsBackToo(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope")
	assert.EqualValues(t, 77, telemetry.CgroupLimit(missing, func() int64 { return 77 })())
	assert.EqualValues(t, 77, telemetry.CgroupLimit(writeMax(t, "garbage"), func() int64 { return 77 })())
	assert.EqualValues(t, 0, telemetry.CgroupLimit(missing, func() int64 { return -1 })())
}

func TestHeapUsage_IsPositiveAndSmallerThanTheTotalMemory(t *testing.T) {
	assert.Positive(t, telemetry.GoMemoryUsage())
}
