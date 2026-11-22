package telemetry

import (
	"math"
	"os"
	"runtime/metrics"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bdnix/kubelook/internal/clock"
)

const (
	defaultRefusePercent = 80
	sampleEvery          = time.Second
)

// MemGuardDeps are the dependencies of MemGuard.
type MemGuardDeps struct {
	Clock clock.Clock
	// Usage and Limit read the memory in use and the memory limit in bytes. A limit of 0 means none or unknown.
	Usage, Limit func() uint64
	// Percent is the share of the limit at which new work is refused; ≤ 0 → 80.
	Percent int
}

// MemGuard says whether the server is too full to take on new work. It only reads; it never changes the
// Go memory limit (GOMEMLIMIT is set by the Helm chart).
type MemGuard struct {
	d MemGuardDeps

	mu     sync.Mutex
	sample time.Time
	busy   bool
}

// NewMemGuard returns a guard.
func NewMemGuard(d MemGuardDeps) *MemGuard {
	if d.Percent <= 0 {
		d.Percent = defaultRefusePercent
	}
	return &MemGuard{d: d}
}

// Busy reports whether the memory in use is at or above the threshold. The memory is read at most once a second.
func (g *MemGuard) Busy() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := g.d.Clock.Now()
	if !g.sample.IsZero() && now.Sub(g.sample) < sampleEvery {
		return g.busy
	}
	g.sample = now
	limit := g.d.Limit()
	g.busy = limit > 0 && float64(g.d.Usage()) >= float64(limit)*float64(g.d.Percent)/100
	return g.busy
}

// CgroupLimit returns a reader of the memory limit: the number in the cgroup file at path. When the file says
// "max", is missing or unreadable, goLimit supplies the limit instead (debug.SetMemoryLimit(-1) in production);
// a Go limit of math.MaxInt64 or less than one means there is none, and the reader returns 0.
func CgroupLimit(path string, goLimit func() int64) func() uint64 {
	return func() uint64 {
		if b, err := os.ReadFile(path); err == nil { //nolint:gosec // the path is a fixed cgroup file or a test's temporary file
			if n, err := strconv.ParseUint(strings.TrimSpace(string(b)), 10, 64); err == nil {
				return n
			}
		}
		if n := goLimit(); n > 0 && n != math.MaxInt64 {
			return uint64(n)
		}
		return 0
	}
}

// GoMemoryUsage is the memory the Go runtime holds from the system and has not given back:
// /memory/classes/total:bytes minus /memory/classes/heap/released:bytes.
func GoMemoryUsage() uint64 {
	samples := []metrics.Sample{{Name: "/memory/classes/total:bytes"}, {Name: "/memory/classes/heap/released:bytes"}}
	metrics.Read(samples)
	if samples[0].Value.Kind() != metrics.KindUint64 || samples[1].Value.Kind() != metrics.KindUint64 {
		return 0
	}
	total, released := samples[0].Value.Uint64(), samples[1].Value.Uint64()
	if released > total {
		return total
	}
	return total - released
}
