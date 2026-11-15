package logs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	corev1 "k8s.io/api/core/v1"

	"github.com/bdnix/kubelook/internal/apperr"
)

const (
	opHistory = "logs.history"
	// maxHistoryRequest bounds the lines one history request asks for.
	maxHistoryRequest = 100000
)

// historyRequest is the "d" of a logs.history op: the n lines before a timestamp in one source's log.
type historyRequest struct {
	Source Target `json:"source"`
	Before string `json:"before"`
	Lines  int    `json:"lines"`
}

// history loads older lines of one source. Kubernetes serves only the current log file, so it asks for the
// last "already loaded plus n" lines and keeps those older than the reader's oldest line. It answers with a
// "history" frame and, when nothing older can be had, a "historyEnd" frame saying why.
func (s *stream) history(ctx context.Context, raw json.RawMessage) error {
	var req historyRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return fmt.Errorf("logs.history needs {source, before, lines}: %w", apperr.ErrInvalid)
	}
	before, err := time.Parse(time.RFC3339Nano, req.Before)
	if err != nil {
		return fmt.Errorf("before must be an RFC 3339 time: %w", apperr.ErrInvalid)
	}
	if req.Lines <= 0 || req.Lines > maxHistoryRequest {
		return fmt.Errorf("lines must be between 1 and %d: %w", maxHistoryRequest, apperr.ErrInvalid)
	}
	u := s.upstreamOf(req.Source)
	if u == nil {
		return fmt.Errorf("that container is not one of this channel's sources: %w", apperr.ErrInvalid)
	}
	var total int64
	for _, o := range s.ups {
		total += o.loaded.Load()
	}
	allowed := int64(s.d.historyMax) - total
	if allowed <= 0 {
		return s.historyEnd(u, "cap")
	}
	n := min(int64(req.Lines), allowed)
	tail := u.loaded.Load() + n
	opts := &corev1.PodLogOptions{Container: u.target.Container, Timestamps: true, TailLines: &tail}
	rc, err := s.d.source.Stream(ctx, s.d.sess, s.d.cluster, s.d.namespace, u.target.Pod, opts)
	if err != nil {
		return err
	}
	all, err := readAll(ctx, rc)
	_ = rc.Close()
	if err != nil {
		return err
	}

	var older []Line
	for _, l := range all {
		if ts, err := time.Parse(time.RFC3339Nano, l.TS); err == nil && ts.Before(before) {
			older = append(older, l)
		}
	}
	take := older[max(0, len(older)-int(n)):]
	u.loaded.Add(int64(len(take)))
	var out []Line
	for _, l := range take {
		if s.d.filter.match(l.Text) {
			if s.d.tagged {
				l.Pod, l.Container = u.target.Pod, u.target.Container
			}
			out = append(out, l)
		}
	}
	if len(out) > 0 {
		if err := s.sink.Send(s.ch, "history", map[string]any{"pod": u.target.Pod, "container": u.target.Container, "lines": out}); err != nil {
			return err
		}
	}
	switch {
	case int64(len(all)) < tail:
		return s.historyEnd(u, "start")
	case total+int64(len(take)) >= int64(s.d.historyMax):
		return s.historyEnd(u, "cap")
	}
	return nil
}

func (s *stream) upstreamOf(t Target) *upstream {
	for _, u := range s.ups {
		if u.target == t {
			return u
		}
	}
	return nil
}

func (s *stream) historyEnd(u *upstream, reason string) error {
	return s.sink.Send(s.ch, "historyEnd", map[string]any{"pod": u.target.Pod, "container": u.target.Container, "reason": reason})
}

// readAll reads a whole log (a non-following request) into lines.
func readAll(ctx context.Context, r io.Reader) ([]Line, error) {
	out := make(chan Line)
	ends := make(chan end, 1)
	go readLines(ctx, r, out, ends, 0, nil)
	var lines []Line
	for {
		select {
		case l := <-out:
			lines = append(lines, l)
		case e := <-ends:
			if errors.Is(e.err, io.EOF) {
				return lines, nil
			}
			return lines, e.err
		}
	}
}
