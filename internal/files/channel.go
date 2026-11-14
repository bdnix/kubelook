package files

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"sync"

	"github.com/bdnix/kubelook/internal/apperr"
	"github.com/bdnix/kubelook/internal/audit"
	"github.com/bdnix/kubelook/internal/clock"
	"github.com/bdnix/kubelook/internal/execs"
	"github.com/bdnix/kubelook/internal/kube"
	"github.com/bdnix/kubelook/internal/session"
	"github.com/bdnix/kubelook/internal/ws"
)

// Sink is where replies go; *ws.Conn implements it.
type Sink interface {
	Send(ch int, typ string, d any) error
}

// Deps are the dependencies of the file browser channel.
type Deps struct {
	Executor         execs.Executor
	Clock            clock.Clock
	Audit            *audit.Recorder
	Enabled          bool
	MaxTransferBytes int64
	KeepAlive        func(ctx context.Context, sessionHash string)
}

type openRequest struct {
	Cluster   string `json:"cluster"`
	Namespace string `json:"namespace"`
	Pod       string `json:"pod"`
	Container string `json:"container"`
}

// request is the "d" of every op after the open.
type request struct {
	Rid       string `json:"rid"`
	Path      string `json:"path"`
	Data      string `json:"data"`
	Recursive bool   `json:"recursive"`
	Expect    *Stat  `json:"expect"`
	Dir       string `json:"dir"`
	Name      string `json:"name"`
	Tar       bool   `json:"tar"`
	Seq       int    `json:"seq"`
}

// NewFactory returns the "file.open" factory for the WebSocket mux.
func NewFactory(d Deps) ws.Factory {
	return func(ctx context.Context, c *ws.Conn, ch int, raw json.RawMessage) (ws.Channel, error) {
		return open(ctx, d, c, c.Session(), ch, raw)
	}
}

func open(ctx context.Context, d Deps, sink Sink, sess session.Session, ch int, raw json.RawMessage) (ws.Channel, error) {
	if !d.Enabled {
		return nil, fmt.Errorf("the file browser is not enabled: %w", apperr.ErrNotFound)
	}
	var req openRequest
	if err := json.Unmarshal(raw, &req); err != nil || req.Cluster == "" || req.Namespace == "" || req.Pod == "" {
		return nil, fmt.Errorf("file.open needs {cluster, namespace, pod, container}: %w", apperr.ErrInvalid)
	}
	runCtx, cancel := context.WithCancel(ctx)
	return &channel{d: d, sink: sink, sess: sess, ch: ch, req: req, ctx: runCtx, cancel: cancel}, nil
}

type upload struct {
	dir, name string
	tar       bool
	buf       bytes.Buffer
	next      int
}

// channel is one file browser session on one container.
type channel struct {
	d      Deps
	sink   Sink
	sess   session.Session
	ch     int
	req    openRequest
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	opMu   sync.Mutex // one command at a time per channel

	mu     sync.Mutex
	up     *upload
	tarOK  *bool
	closed bool
}

// Close cancels running commands and waits for them.
func (c *channel) Close() {
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	c.cancel()
	c.wg.Wait()
}

func (c *channel) reply(rid, typ string, d map[string]any) {
	if d == nil {
		d = map[string]any{}
	}
	d["rid"] = rid
	_ = c.sink.Send(c.ch, typ, d)
}

func (c *channel) fail(rid string, err error) {
	code, msg := errorFrame(err)
	c.reply(rid, "error", map[string]any{"code": code, "message": msg})
}

func errorFrame(err error) (code, message string) {
	var cmd *commandError
	switch {
	case errors.Is(err, apperr.ErrNotRecorded):
		return "internal", apperr.ErrNotRecorded.Message
	case errors.Is(err, ErrUnsafeArchive):
		return "unsafe_archive", err.Error()
	case errors.Is(err, ErrNoUtilities):
		return "no_utilities", ErrNoUtilities.Error()
	case errors.Is(err, ErrNoTar):
		return "no_tar", ErrNoTar.Error()
	case errors.Is(err, apperr.ErrConflict):
		return "conflict", err.Error()
	case errors.Is(err, apperr.ErrNotFound):
		return "not_found", err.Error()
	case errors.Is(err, apperr.ErrInvalid):
		return "bad_request", err.Error()
	case errors.Is(err, apperr.ErrTooLarge):
		return "too_large", err.Error()
	case errors.Is(err, apperr.ErrForbidden):
		return "forbidden", err.Error()
	case errors.As(err, &cmd):
		return "command_failed", cmd.msg
	}
	return "internal", "The file operation failed."
}

var errConflict = kube.Mapped("The file changed in the container after you opened it.", apperr.ErrConflict)

// Handle starts the op. Commands run one after the other in the background; upload chunks are taken at once.
func (c *channel) Handle(_ context.Context, op string, raw json.RawMessage) error {
	var r request
	if err := json.Unmarshal(raw, &r); err != nil {
		return fmt.Errorf("%s needs a JSON object: %w", op, apperr.ErrInvalid)
	}
	switch op {
	case "file.upload.begin":
		return c.uploadBegin(r)
	case "file.upload.chunk":
		return c.uploadChunk(r)
	case "file.list", "file.read", "file.save", "file.delete", "file.download", "file.upload.end":
	default:
		return fmt.Errorf("unknown op %q for the file browser: %w", op, apperr.ErrInvalid)
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.wg.Add(1)
	c.mu.Unlock()
	go func() {
		defer c.wg.Done()
		c.opMu.Lock()
		defer c.opMu.Unlock()
		if c.d.KeepAlive != nil {
			c.d.KeepAlive(c.ctx, c.sess.Hash)
		}
		var err error
		switch op {
		case "file.list":
			err = c.list(r)
		case "file.read":
			err = c.read(r)
		case "file.save":
			err = c.save(r)
		case "file.delete":
			err = c.remove(r)
		case "file.download":
			err = c.download(r)
		case "file.upload.end":
			err = c.uploadEnd(r)
		}
		if err != nil && c.ctx.Err() == nil {
			c.fail(r.Rid, err)
		}
	}()
	return nil
}

// audited records the action as requested, runs it and records the outcome. No record, no action.
// The reply is sent only after the outcome is recorded, so a client that has the answer can rely on the trail.
func (c *channel) audited(verb, p string, fn func(*audit.Pending) (reply func(), err error)) error {
	pending, err := c.d.Audit.Begin(c.ctx, audit.Event{
		Cluster: c.req.Cluster, Namespace: c.req.Namespace, Verb: verb, ResourceKind: "pods", ResourceName: c.req.Pod,
		Details: map[string]any{"container": c.req.Container, "path": p},
	})
	if err != nil {
		return apperr.ErrNotRecorded
	}
	reply, err := fn(pending)
	pending.Done(context.WithoutCancel(c.ctx), err)
	if err == nil && reply != nil {
		reply()
	}
	return err
}

func (c *channel) list(r request) error {
	p, err := cleanPath(r.Path)
	if err != nil {
		return err
	}
	out, err := c.runCapture(c.ctx, listArgv(p), listLimit, nil)
	if err != nil {
		return err
	}
	entries, skipped := parseListing(out, c.d.Clock.Now())
	c.reply(r.Rid, "list", map[string]any{"entries": entries, "skipped": skipped})
	return nil
}

func (c *channel) stat(p string) (Stat, Entry, error) {
	out, err := c.runCapture(c.ctx, statArgv(p), 64<<10, nil)
	if err != nil {
		return Stat{}, Entry{}, err
	}
	e, ok := parseLine(firstLine(out), c.d.Clock.Now())
	if !ok {
		return Stat{}, Entry{}, fmt.Errorf("the file's details could not be read: %w", apperr.ErrInvalid)
	}
	return Stat{Size: e.Size, Modified: e.Modified}, e, nil
}

func (c *channel) read(r request) error {
	p, err := cleanPath(r.Path)
	if err != nil {
		return err
	}
	return c.audited(audit.VerbFileView, p, func(pending *audit.Pending) (func(), error) {
		st, entry, err := c.stat(p)
		if err != nil {
			return nil, err
		}
		if entry.Type == "dir" {
			return nil, fmt.Errorf("%s is a directory: %w", p, apperr.ErrInvalid)
		}
		pending.Annotate("size", st.Size)
		out := &capWriter{limit: ReadLimit + 1}
		stderr, code, err := c.run(c.ctx, readArgv(p), nil, out)
		if err != nil {
			return nil, err
		}
		if err := check(stderr, code); err != nil {
			return nil, err
		}
		data := out.buf.Bytes()
		truncated := len(data) > ReadLimit
		if truncated {
			data = data[:ReadLimit]
		}
		binary := bytes.IndexByte(data[:min(len(data), binaryProbe)], 0) >= 0
		if binary {
			data = nil
		}
		return func() {
			c.reply(r.Rid, "content", map[string]any{
				"data": base64.StdEncoding.EncodeToString(data), "binary": binary, "truncated": truncated, "stat": st,
			})
		}, nil
	})
}

func (c *channel) save(r request) error {
	p, err := cleanPath(r.Path)
	if err != nil {
		return err
	}
	data, err := base64.StdEncoding.DecodeString(r.Data)
	if err != nil {
		return fmt.Errorf("the data must be base64: %w", apperr.ErrInvalid)
	}
	if int64(len(data)) > c.d.MaxTransferBytes {
		return fmt.Errorf("the file is larger than the limit of %d bytes: %w", c.d.MaxTransferBytes, apperr.ErrTooLarge)
	}
	return c.audited(audit.VerbFileSave, p, func(pending *audit.Pending) (func(), error) {
		pending.Annotate("size", len(data))
		st, _, statErr := c.stat(p)
		switch {
		case statErr == nil && (r.Expect == nil || *r.Expect != st):
			return nil, errConflict
		case statErr != nil && !errors.Is(statErr, apperr.ErrNotFound):
			return nil, statErr
		case statErr != nil && r.Expect != nil:
			return nil, errConflict // it was there when the editor opened it
		}
		stderr, code, err := c.run(c.ctx, saveArgv(p), bytes.NewReader(data), io.Discard)
		if err != nil {
			return nil, err
		}
		if err := check(stderr, code); err != nil {
			return nil, err
		}
		fresh, _, err := c.stat(p)
		if err != nil {
			return nil, err
		}
		return func() { c.reply(r.Rid, "saved", map[string]any{"stat": fresh}) }, nil
	})
}

func (c *channel) remove(r request) error {
	p, err := cleanPath(r.Path)
	if err != nil {
		return err
	}
	argv, err := deleteArgv(p, r.Recursive)
	if err != nil {
		return err
	}
	return c.audited(audit.VerbFileDelete, p, func(*audit.Pending) (func(), error) {
		stderr, code, err := c.run(c.ctx, argv, nil, io.Discard)
		if err != nil {
			return nil, err
		}
		if err := check(stderr, code); err != nil {
			return nil, err
		}
		return func() { c.reply(r.Rid, "deleted", nil) }, nil
	})
}

// hasTar probes once per channel whether tar exists in the container.
func (c *channel) hasTar() bool {
	c.mu.Lock()
	known := c.tarOK
	c.mu.Unlock()
	if known != nil {
		return *known
	}
	_, code, err := c.run(c.ctx, tarProbeArgv, nil, io.Discard)
	ok := err == nil && code == 0
	c.mu.Lock()
	c.tarOK = &ok
	c.mu.Unlock()
	return ok
}

func (c *channel) download(r request) error {
	p, err := cleanPath(r.Path)
	if err != nil {
		return err
	}
	return c.audited(audit.VerbFileDownload, p, func(pending *audit.Pending) (func(), error) {
		_, entry, err := c.stat(p)
		if err != nil {
			return nil, err
		}
		argv, isTar := catArgv(p), false
		if entry.Type == "dir" {
			if p == "/" {
				return nil, fmt.Errorf("the root directory cannot be downloaded: %w", apperr.ErrInvalid)
			}
			if !c.hasTar() {
				return nil, ErrNoTar
			}
			argv, isTar = tarCreateArgv(path.Dir(p), path.Base(p)), true
		}
		out := &chunker{c: c, rid: r.Rid, limit: c.d.MaxTransferBytes}
		var dst io.Writer = out
		var guard *tarGuard
		runCtx, cancel := context.WithCancel(c.ctx)
		defer cancel()
		if isTar {
			guard = newTarGuard(path.Base(p), cancel)
			dst = io.MultiWriter(out, guard)
		}
		stderr, code, runErr := c.run(runCtx, argv, nil, dst)
		if guard != nil {
			if verdict := guard.Wait(); verdict != nil {
				return nil, verdict
			}
		}
		if out.over {
			return nil, fmt.Errorf("the transfer is larger than the limit of %d bytes: %w", out.limit, apperr.ErrTooLarge)
		}
		if runErr != nil {
			return nil, runErr
		}
		if err := check(stderr, code); err != nil {
			return nil, err
		}
		if err := out.finish(); err != nil {
			return nil, err
		}
		pending.Annotate("size", out.total)
		return func() { c.reply(r.Rid, "done", map[string]any{"bytes": out.total, "tar": isTar}) }, nil
	})
}

func (c *channel) uploadBegin(r request) error {
	dir, err := cleanPath(r.Dir)
	if err != nil {
		return err
	}
	if !r.Tar {
		if err := leafName(r.Name); err != nil {
			return err
		}
	}
	c.mu.Lock()
	c.up = &upload{dir: dir, name: r.Name, tar: r.Tar}
	c.mu.Unlock()
	return nil
}

func (c *channel) uploadChunk(r request) error {
	data, err := base64.StdEncoding.DecodeString(r.Data)
	if err != nil {
		return fmt.Errorf("the data must be base64: %w", apperr.ErrInvalid)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	u := c.up
	if u == nil {
		return fmt.Errorf("no upload was begun: %w", apperr.ErrInvalid)
	}
	if r.Seq != u.next {
		c.up = nil
		return fmt.Errorf("chunk %d arrived, %d was expected: %w", r.Seq, u.next, apperr.ErrInvalid)
	}
	if int64(u.buf.Len()+len(data)) > c.d.MaxTransferBytes {
		c.up = nil
		return fmt.Errorf("the upload is larger than the limit of %d bytes: %w", c.d.MaxTransferBytes, apperr.ErrTooLarge)
	}
	u.buf.Write(data)
	u.next++
	return nil
}

func (c *channel) uploadEnd(r request) error {
	c.mu.Lock()
	u := c.up
	c.up = nil
	c.mu.Unlock()
	if u == nil {
		return fmt.Errorf("no upload was begun: %w", apperr.ErrInvalid)
	}
	target := u.dir
	if !u.tar {
		target = path.Join(u.dir, u.name)
	}
	return c.audited(audit.VerbFileUpload, target, func(pending *audit.Pending) (func(), error) {
		pending.Annotate("size", u.buf.Len())
		argv, stdin, err := c.uploadCommand(u)
		if err != nil {
			return nil, err
		}
		stderr, code, err := c.run(c.ctx, argv, bytes.NewReader(stdin), io.Discard)
		if err != nil {
			return nil, err
		}
		if err := check(stderr, code); err != nil {
			return nil, err
		}
		return func() { c.reply(r.Rid, "uploaded", map[string]any{"bytes": u.buf.Len()}) }, nil
	})
}

// uploadCommand decides how the bytes reach the container: a client-built tar is validated and extracted; a
// single file goes in a tar made here when tar exists, else straight through tee.
func (c *channel) uploadCommand(u *upload) (argv []string, stdin []byte, err error) {
	data := u.buf.Bytes()
	switch {
	case u.tar:
		if !c.hasTar() {
			return nil, nil, ErrNoTar
		}
		if err := checkTar(bytes.NewReader(data), ""); err != nil {
			return nil, nil, err
		}
		return tarExtractArgv(u.dir), data, nil
	case c.hasTar():
		archive, err := singleFileTar(u.name, data)
		if err != nil {
			return nil, nil, err
		}
		return tarExtractArgv(u.dir), archive, nil
	}
	return saveArgv(path.Join(u.dir, u.name)), data, nil
}
