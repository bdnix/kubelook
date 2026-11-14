package session

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"sync"
)

// endedCapacity bounds the memory of session hashes that have been closed. A closer
// registered for one of them (a stream opening just after sign-out) is closed at once.
const endedCapacity = 4096

type entry struct {
	id      uint64
	name    string
	closeFn func() error
}

// Registry tracks, per session hash, the things to close when that session ends on this replica.
type Registry struct {
	mu         sync.Mutex
	next       uint64
	entries    map[string][]*entry
	ended      map[string]struct{}
	endedOrder []string
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{entries: map[string][]*entry{}, ended: map[string]struct{}{}}
}

// Register adds a closer for the session and returns a function that removes it again.
// If the session has already ended, closeFn runs immediately (its error is dropped:
// there is nobody left to report it to) and the returned function does nothing.
func (r *Registry) Register(hash, name string, closeFn func() error) (unregister func()) {
	r.mu.Lock()
	if _, ended := r.ended[hash]; ended {
		r.mu.Unlock()
		_ = closeFn()
		return func() {}
	}
	r.next++
	e := &entry{id: r.next, name: name, closeFn: closeFn}
	r.entries[hash] = append(r.entries[hash], e)
	r.mu.Unlock()
	return func() { r.remove(hash, e.id) }
}

func (r *Registry) remove(hash string, id uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	list := r.entries[hash]
	for i, e := range list {
		if e.id == id {
			r.entries[hash] = slices.Delete(list, i, i+1)
			break
		}
	}
	if len(r.entries[hash]) == 0 {
		delete(r.entries, hash)
	}
}

// Count returns the number of registered closers for the session.
func (r *Registry) Count(hash string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.entries[hash])
}

// Hashes lists the sessions that have registered closers, sorted.
func (r *Registry) Hashes() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.entries))
	for h := range r.entries {
		out = append(out, h)
	}
	sort.Strings(out)
	return out
}

// Close runs the session's closers once, in registration order, and returns their joined errors.
// Closers run without the lock held, so they may call their own unregister function.
func (r *Registry) Close(hash string) error {
	r.mu.Lock()
	list := r.entries[hash]
	delete(r.entries, hash)
	r.markEnded(hash)
	r.mu.Unlock()

	var errs []error
	for _, e := range list {
		if err := e.closeFn(); err != nil {
			errs = append(errs, fmt.Errorf("closing %s: %w", e.name, err))
		}
	}
	return errors.Join(errs...)
}

// CloseMatching runs and removes the session's closers whose name satisfies match, in
// registration order, and returns their joined errors. Unlike Close it leaves the session open:
// later registrations are kept.
func (r *Registry) CloseMatching(hash string, match func(name string) bool) error {
	r.mu.Lock()
	var hit, keep []*entry
	for _, e := range r.entries[hash] {
		if match(e.name) {
			hit = append(hit, e)
		} else {
			keep = append(keep, e)
		}
	}
	if len(keep) == 0 {
		delete(r.entries, hash)
	} else {
		r.entries[hash] = keep
	}
	r.mu.Unlock()

	var errs []error
	for _, e := range hit {
		if err := e.closeFn(); err != nil {
			errs = append(errs, fmt.Errorf("closing %s: %w", e.name, err))
		}
	}
	return errors.Join(errs...)
}

// markEnded remembers hash, forgetting the oldest entry beyond the capacity. Callers hold r.mu.
func (r *Registry) markEnded(hash string) {
	if _, ok := r.ended[hash]; ok {
		return
	}
	r.ended[hash] = struct{}{}
	r.endedOrder = append(r.endedOrder, hash)
	if len(r.endedOrder) > endedCapacity {
		delete(r.ended, r.endedOrder[0])
		r.endedOrder = r.endedOrder[1:]
	}
}
