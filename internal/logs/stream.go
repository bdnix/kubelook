package logs

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bdnix/kubelook/internal/clock"
	"github.com/bdnix/kubelook/internal/kube"
	"github.com/bdnix/kubelook/internal/session"
	"github.com/bdnix/kubelook/internal/ws"
)

// Limits of one stream.
const (
	flushEvery    = 100 * time.Millisecond
	flushLines    = 500
	maxBuffered   = 2000
	maxLineBytes  = 64 << 10
	truncatedMark = "… [truncated]"
	// busyBacklog is the outbound queue length from which a stream holds its lines back.
	busyBacklog = ws.QueueCapacity / 2
)

// Line is one log line: its RFC 3339 timestamp (requested from the API server) and its text.
type Line struct {
	Pod       string `json:"pod,omitempty"`       // set when several containers are followed
	Container string `json:"container,omitempty"` // likewise
	TS        string `json:"ts"`
	Text      string `json:"text"`
	src       int    // the upstream the line came from
}

// Sink is where a stream sends frames; *ws.Conn implements it.
type Sink interface {
	Send(ch int, typ string, d any) error
	Backlog() int
	CloseChannel(ch int, reason string)
}

type streamDeps struct {
	metrics   Metrics
	clock     clock.Clock
	keepAlive func(ctx context.Context)
	maxPerSec int
	onEnd     func(ctx context.Context) // called once when the stream ends, whatever the reason
	// What the history op needs to read older lines.
	source             Source
	sess               session.Session
	cluster, namespace string
	filter             *lineFilter
	historyMax         int
	// tagged is set when the request named its sources: lines and end frames then say which container they are from.
	tagged bool
}

// upstream is one container's log.
type upstream struct {
	target Target
	rc     io.ReadCloser
	// loaded counts the lines read from this container: the live ones and those loaded by history.
	loaded atomic.Int64
}

// sourceFailure is a container that could not be opened.
type sourceFailure struct {
	target Target
	err    error
}

// end is the end of one upstream.
type end struct {
	src int
	err error
}

// stream reads the logs of one or more containers and forwards them as one.
type stream struct {
	d      streamDeps
	sink   Sink
	ch     int
	follow bool
	ups    []*upstream
	failed []sourceFailure
	cancel context.CancelFunc
	done   chan struct{}

	closeOnce sync.Once
}

// start begins forwarding. The returned stream is a ws.Channel.
func start(ctx context.Context, d streamDeps, sink Sink, ch int, follow bool, ups []*upstream, failed []sourceFailure) *stream {
	ctx, cancel := context.WithCancel(ctx)
	s := &stream{d: d, sink: sink, ch: ch, follow: follow, ups: ups, failed: failed, cancel: cancel, done: make(chan struct{})}
	go s.run(ctx)
	return s
}

// Handle takes one op, logs.history; everything else is refused: a log channel is driven by the server.
func (s *stream) Handle(ctx context.Context, op string, d json.RawMessage) error {
	if op == opHistory {
		return s.history(ctx, d)
	}
	return errNoOps
}

// Close stops the stream and waits for it to finish.
func (s *stream) Close() {
	s.closeOnce.Do(func() {
		s.d.metrics.LogStreamClosed()
		s.cancel()
		for _, u := range s.ups {
			_ = u.rc.Close()
		}
		<-s.done
	})
}

// run owns the pending lines. A small goroutine reads and splits the stream; everything else
// happens here, one event at a time.
func (s *stream) run(ctx context.Context) {
	reason := s.loop(ctx)
	if s.d.onEnd != nil {
		s.d.onEnd(ctx)
	}
	close(s.done)
	if reason != "" {
		// After done is closed: closing the channel calls Close, which waits for done.
		s.sink.CloseChannel(s.ch, reason)
	}
}

// loop forwards lines until the stream ends and returns the reason to close the channel, if any.
func (s *stream) loop(ctx context.Context) string {
	for _, f := range s.failed {
		s.sourceFailed(f.target, f.err)
	}
	lines := make(chan Line)
	ends := make(chan end, len(s.ups))
	for i, u := range s.ups {
		var tag *Target
		if s.d.tagged {
			tag = &u.target
		}
		go readLines(ctx, u.rc, lines, ends, i, tag)
	}
	alive := len(s.ups)

	tick := s.d.clock.NewTicker(flushEvery)
	defer tick.Stop()
	bucket := newBucket(s.d.clock, s.d.maxPerSec)
	var pending []Line
	dropped := 0

	flush := func(force bool) {
		if len(pending) > 0 && (force || s.sink.Backlog() < busyBacklog) {
			for len(pending) > 0 {
				n := min(len(pending), flushLines)
				if err := s.sink.Send(s.ch, "lines", map[string]any{"lines": pending[:n]}); err != nil {
					return
				}
				pending = pending[n:]
			}
			pending = nil
		}
		if dropped > 0 && len(pending) == 0 {
			if err := s.sink.Send(s.ch, "dropped", map[string]any{"count": dropped}); err == nil {
				dropped = 0
			}
		}
	}

	for {
		select {
		case <-ctx.Done():
			return ""
		case l := <-lines:
			s.ups[l.src].loaded.Add(1)
			if !s.d.filter.match(l.Text) {
				continue // filtered out: not a drop
			}
			if !bucket.allow() {
				dropped++
				s.d.metrics.LogLinesDropped(1)
				continue
			}
			pending = append(pending, l)
			if len(pending) > maxBuffered {
				pending = pending[1:]
				dropped++
				s.d.metrics.LogLinesDropped(1)
			}
			if len(pending) >= flushLines {
				flush(false)
			}
		case <-tick.C():
			if s.d.keepAlive != nil {
				s.d.keepAlive(ctx)
			}
			flush(false)
		case e := <-ends:
			flush(true)
			if !s.d.tagged {
				return s.finish(ctx, e.err)
			}
			if ctx.Err() != nil {
				return ""
			}
			s.sourceEnded(s.ups[e.src].target, e.err)
			if alive--; alive == 0 && s.follow {
				return "container_terminated"
			}
		}
	}
}

// finish reports how the stream ended and returns the reason to close the channel ("" keeps it open).
func (s *stream) finish(ctx context.Context, err error) string {
	if ctx.Err() != nil {
		return ""
	}
	if err != nil && !errors.Is(err, io.EOF) {
		_ = s.sink.Send(s.ch, ws.TypeError, ws.ErrorBody(kube.MapError(err)))
		return "error"
	}
	_ = s.sink.Send(s.ch, "eof", map[string]any{})
	if s.follow {
		return "container_terminated"
	}
	return ""
}

// sourceEnded tells the browser that one container's log ended, or failed.
func (s *stream) sourceEnded(t Target, err error) {
	if err != nil && !errors.Is(err, io.EOF) {
		s.sourceFailed(t, err)
		return
	}
	_ = s.sink.Send(s.ch, "eof", map[string]any{"pod": t.Pod, "container": t.Container})
}

func (s *stream) sourceFailed(t Target, err error) {
	body := ws.ErrorBody(kube.MapError(err))
	_ = s.sink.Send(s.ch, "source_error", map[string]any{"pod": t.Pod, "container": t.Container, "code": body["code"], "message": body["message"]})
}

// readLines splits the stream into lines and hands them over, tagged with the container when tag is set.
// It reports the end on ends.
func readLines(ctx context.Context, r io.Reader, out chan<- Line, ends chan<- end, src int, tag *Target) {
	br := bufio.NewReaderSize(r, maxLineBytes)
	for {
		raw, err := br.ReadSlice('\n')
		truncated := false
		if errors.Is(err, bufio.ErrBufferFull) {
			truncated = true
			text := append([]byte(nil), raw...)
			if err = discardLine(br); err == nil || errors.Is(err, io.EOF) {
				err = nil
			}
			raw = text
		}
		if len(raw) > 0 {
			l := parse(raw, truncated)
			l.src = src
			if tag != nil {
				l.Pod, l.Container = tag.Pod, tag.Container
			}
			select {
			case out <- l:
			case <-ctx.Done():
				ends <- end{src, ctx.Err()}
				return
			}
		}
		if err != nil {
			ends <- end{src, err}
			return
		}
	}
}

// discardLine skips the rest of an over-long line.
func discardLine(br *bufio.Reader) error {
	for {
		_, err := br.ReadSlice('\n')
		if !errors.Is(err, bufio.ErrBufferFull) {
			return err
		}
	}
}

// parse splits "<timestamp> <text>" and cleans the text.
func parse(raw []byte, truncated bool) Line {
	s := strings.TrimRight(string(raw), "\r\n")
	var l Line
	if ts, text, ok := strings.Cut(s, " "); ok {
		l = Line{TS: ts, Text: text}
	} else {
		l = Line{Text: s}
	}
	l.Text = strings.ToValidUTF8(l.Text, "�")
	if truncated {
		l.Text += truncatedMark
	}
	return l
}
