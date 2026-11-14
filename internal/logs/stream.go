package logs

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/bdnix/kubelook/internal/clock"
	"github.com/bdnix/kubelook/internal/kube"
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
	TS   string `json:"ts"`
	Text string `json:"text"`
}

// Sink is where a stream sends frames; *ws.Conn implements it.
type Sink interface {
	Send(ch int, typ string, d any) error
	Backlog() int
	CloseChannel(ch int, reason string)
}

type streamDeps struct {
	clock     clock.Clock
	keepAlive func(ctx context.Context)
	maxPerSec int
	onEnd     func(ctx context.Context) // called once when the stream ends, whatever the reason
}

// stream reads one container's log and forwards it.
type stream struct {
	d      streamDeps
	sink   Sink
	ch     int
	follow bool
	rc     io.ReadCloser
	cancel context.CancelFunc
	done   chan struct{}

	closeOnce sync.Once
}

// start begins forwarding. The returned stream is a ws.Channel.
func start(ctx context.Context, d streamDeps, sink Sink, ch int, follow bool, rc io.ReadCloser) *stream {
	ctx, cancel := context.WithCancel(ctx)
	s := &stream{d: d, sink: sink, ch: ch, follow: follow, rc: rc, cancel: cancel, done: make(chan struct{})}
	go s.run(ctx)
	return s
}

// Handle refuses every op: a log channel is driven by the server.
func (s *stream) Handle(context.Context, string, json.RawMessage) error { return errNoOps }

// Close stops the stream and waits for it to finish.
func (s *stream) Close() {
	s.closeOnce.Do(func() {
		s.cancel()
		_ = s.rc.Close()
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
	lines := make(chan Line)
	readErr := make(chan error, 1)
	go readLines(ctx, s.rc, lines, readErr)

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
			if !bucket.allow() {
				dropped++
				continue
			}
			pending = append(pending, l)
			if len(pending) > maxBuffered {
				pending = pending[1:]
				dropped++
			}
			if len(pending) >= flushLines {
				flush(false)
			}
		case <-tick.C():
			if s.d.keepAlive != nil {
				s.d.keepAlive(ctx)
			}
			flush(false)
		case err := <-readErr:
			flush(true)
			return s.finish(ctx, err)
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

// readLines splits the stream into lines and hands them over. It reports the end on end.
func readLines(ctx context.Context, r io.Reader, out chan<- Line, end chan<- error) {
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
			select {
			case out <- parse(raw, truncated):
			case <-ctx.Done():
				end <- ctx.Err()
				return
			}
		}
		if err != nil {
			end <- err
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
