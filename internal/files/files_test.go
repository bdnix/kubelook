package files_test

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdnix/kubelook/internal/apperr"
	"github.com/bdnix/kubelook/internal/audit"
	"github.com/bdnix/kubelook/internal/clock"
	"github.com/bdnix/kubelook/internal/execs"
	"github.com/bdnix/kubelook/internal/files"
	"github.com/bdnix/kubelook/internal/session"
	"github.com/bdnix/kubelook/internal/store/storetest"
	"github.com/bdnix/kubelook/internal/testutil"
)

var (
	t0   = time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	sess = session.Session{Hash: "sess", UserID: "u"}
)

type frame struct {
	Type string
	D    map[string]any
}

type sink struct{ frames chan frame }

func (s *sink) Send(_ int, typ string, d any) error {
	b, _ := json.Marshal(d)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	s.frames <- frame{typ, m}
	return nil
}

// reply is what a fake command does: write to stdout/stderr, return an exit code.
type reply struct {
	stdout, stderr string
	code           int
	err            error
	write          func(w io.Writer) error // streams stdout instead
}

type call struct {
	argv  []string
	stdin []byte
}

type fakeExec struct {
	mu    sync.Mutex
	rules []func(argv []string) *reply
	calls []call
}

func (f *fakeExec) on(prefix string, r reply) {
	f.rules = append(f.rules, func(argv []string) *reply {
		if strings.HasPrefix(strings.Join(argv, " "), prefix) {
			return &r
		}
		return nil
	})
}

func (f *fakeExec) Stream(ctx context.Context, _ session.Session, _ string, r execs.ExecRequest, sio execs.StreamIO) error {
	var in []byte
	if sio.Stdin != nil {
		in, _ = io.ReadAll(sio.Stdin)
	}
	f.mu.Lock()
	f.calls = append(f.calls, call{append([]string(nil), r.Command...), in})
	rules := f.rules
	f.mu.Unlock()
	for i := len(rules) - 1; i >= 0; i-- {
		rep := rules[i](r.Command)
		if rep == nil {
			continue
		}
		if rep.err != nil {
			return rep.err
		}
		if rep.write != nil {
			if err := rep.write(sio.Stdout); err != nil {
				return err
			}
		}
		_, _ = io.WriteString(sio.Stdout, rep.stdout)
		_, _ = io.WriteString(sio.Stderr, rep.stderr)
		if rep.code != 0 {
			return &execs.ExitError{Code: rep.code, Message: fmt.Sprintf("command terminated with exit code %d", rep.code)}
		}
		return ctx.Err()
	}
	return &execs.ExitError{Code: 127, Message: "command terminated with exit code 127"}
}

func (f *fakeExec) argvs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, c := range f.calls {
		out = append(out, strings.Join(c.argv, "\x1f"))
	}
	return out
}

type env struct {
	t     *testing.T
	exec  *fakeExec
	sink  *sink
	out   *bytes.Buffer
	deps  files.Deps
	chn   *chanHandle
	clock *clock.Fake
}

type chanHandle struct {
	handle func(op string, d any) error
	close  func()
}

func newEnv(t *testing.T) *env {
	t.Helper()
	e := &env{t: t, exec: &fakeExec{}, sink: &sink{frames: make(chan frame, 512)}, out: &bytes.Buffer{}, clock: clock.NewFake(t0)}
	db := storetest.New(t)
	n := 0
	rec := audit.NewRecorder(db, e.clock, e.out, func() string { n++; return fmt.Sprintf("a%d", n) })
	e.deps = files.Deps{Executor: e.exec, Clock: e.clock, Audit: rec, Enabled: true, MaxTransferBytes: 1 << 20}
	return e
}

func (e *env) open() {
	e.t.Helper()
	raw := json.RawMessage(`{"cluster":"c","namespace":"ns","pod":"web-1","container":"app"}`)
	ch, err := files.Open(e.t.Context(), e.deps, e.sink, sess, 1, raw)
	require.NoError(e.t, err)
	e.t.Cleanup(ch.Close)
	e.chn = &chanHandle{
		handle: func(op string, d any) error {
			b, _ := json.Marshal(d)
			return ch.Handle(e.t.Context(), op, b)
		},
		close: ch.Close,
	}
}

func (e *env) do(op string, d map[string]any) frame {
	e.t.Helper()
	if e.chn == nil {
		e.open()
	}
	require.NoError(e.t, e.chn.handle(op, d))
	return e.next()
}

func (e *env) next() frame { return testutil.Recv(e.t, e.sink.frames) }

func (e *env) auditVerbs() []string {
	var out []string
	for _, l := range strings.Split(strings.TrimSpace(e.out.String()), "\n") {
		if l == "" {
			continue
		}
		var m map[string]any
		_ = json.Unmarshal([]byte(l), &m)
		out = append(out, m["verb"].(string)+":"+m["outcome"].(string))
	}
	return out
}

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

func testdata(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	require.NoError(t, err)
	return string(b)
}

const statLine = "-rw-r--r-- 1 root root 11 Nov 14 10:00 hosts\n"

func TestOpen_DisabledAnswersNotFound(t *testing.T) {
	e := newEnv(t)
	e.deps.Enabled = false
	_, err := files.Open(t.Context(), e.deps, e.sink, sess, 1, json.RawMessage(`{"cluster":"c","namespace":"ns","pod":"p"}`))
	assert.ErrorIs(t, err, apperr.ErrNotFound)
	e.deps.Enabled = true
	_, err = files.Open(t.Context(), e.deps, e.sink, sess, 1, json.RawMessage(`{"cluster":"c"}`))
	assert.ErrorIs(t, err, apperr.ErrInvalid)
	_, err = files.Open(t.Context(), e.deps, e.sink, sess, 1, json.RawMessage(`"x"`))
	assert.ErrorIs(t, err, apperr.ErrInvalid)
}

func TestList_ParsesGNUAndBusyBoxOutputAndCountsWhatItCannotRead(t *testing.T) {
	for _, name := range []string{"ls_gnu.txt", "ls_busybox.txt"} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			e.exec.on("ls -lA", reply{stdout: testdata(t, name)})
			f := e.do("file.list", map[string]any{"rid": "r1", "path": "/etc/"})
			require.Equal(t, "list", f.Type, f.D)
			assert.Equal(t, "r1", f.D["rid"])
			entries := f.D["entries"].([]any)
			byName := map[string]map[string]any{}
			for _, x := range entries {
				m := x.(map[string]any)
				byName[m["name"].(string)] = m
			}
			assert.Equal(t, "file", byName["app.conf"]["type"])
			assert.EqualValues(t, 1234, byName["app.conf"]["size"])
			assert.Equal(t, "2026-01-02T15:04:00Z", byName["app.conf"]["modified"])
			assert.Equal(t, "-rw-r--r--", byName["app.conf"]["mode"])
			assert.Equal(t, "dir", byName["data dir"]["type"])
			assert.Equal(t, "2025-01-02T00:00:00Z", byName["data dir"]["modified"])
			assert.Equal(t, "link", byName["current"]["type"])
			assert.Equal(t, "releases/42", byName["current"]["target"])
			assert.Equal(t, "other", byName["null"]["type"])
			assert.EqualValues(t, 0, byName["null"]["size"])
			if name == "ls_gnu.txt" {
				assert.EqualValues(t, 1, f.D["skipped"], "the nonsense line is counted")
				assert.Equal(t, "2025-12-31T23:59:00Z", byName["future-looking"]["modified"], "a date in the future belongs to last year")
			} else {
				assert.EqualValues(t, 0, f.D["skipped"])
				assert.Equal(t, "other", byName["sock"]["type"])
			}
			assert.Equal(t, []string{"ls\x1f-lA\x1f--\x1f/etc"}, e.exec.argvs(), "cleaned and after --")
		})
	}
}

func TestPaths_AreValidatedAndAlwaysASeparateArgumentAfterDashes(t *testing.T) {
	e := newEnv(t)
	e.exec.on("ls", reply{stdout: ""})
	for _, bad := range []string{"relative/path", "", "/etc/\x00x"} {
		f := e.do("file.list", map[string]any{"rid": "r", "path": bad})
		assert.Equal(t, "error", f.Type)
		assert.Equal(t, "bad_request", f.D["code"], bad)
	}
	assert.Empty(t, e.exec.argvs())
	for _, name := range []string{"/tmp/-rf", "/tmp/; rm -rf /x", "/tmp/$(id)", "/tmp/with space", "/tmp/new\nline"} {
		f := e.do("file.list", map[string]any{"rid": "r", "path": name})
		assert.Equal(t, "list", f.Type)
	}
	for i, name := range []string{"/tmp/-rf", "/tmp/; rm -rf /x", "/tmp/$(id)", "/tmp/with space", "/tmp/new\nline"} {
		assert.Equal(t, []string{"ls", "-lA", "--", name}, strings.Split(e.exec.argvs()[i], "\x1f"))
	}
}

func TestRead_StatsThenReadsAndFlagsBinaryAndTruncated(t *testing.T) {
	e := newEnv(t)
	e.exec.on("ls -ld", reply{stdout: statLine})
	e.exec.on("head -c 1048577 -- /etc/hosts", reply{stdout: "127.0.0.1 x\n"})
	f := e.do("file.read", map[string]any{"rid": "r", "path": "/etc/hosts"})
	require.Equal(t, "content", f.Type, f.D)
	assert.Equal(t, b64("127.0.0.1 x\n"), f.D["data"])
	assert.Equal(t, false, f.D["binary"])
	assert.Equal(t, false, f.D["truncated"])
	assert.Equal(t, map[string]any{"size": float64(11), "modified": "2026-11-14T10:00:00Z"}, f.D["stat"])
	assert.Equal(t, []string{"file-view:requested", "file-view:success"}, e.auditVerbs())
	assert.Contains(t, e.out.String(), `"size":11`)
	assert.Contains(t, e.out.String(), `"path":"/etc/hosts"`)
	assert.NotContains(t, e.out.String(), "127.0.0.1")

	e2 := newEnv(t)
	e2.exec.on("ls -ld", reply{stdout: statLine})
	e2.exec.on("head", reply{stdout: "abc\x00def"})
	f = e2.do("file.read", map[string]any{"rid": "r", "path": "/bin/x"})
	assert.Equal(t, true, f.D["binary"])
	assert.Equal(t, "", f.D["data"])

	e3 := newEnv(t)
	e3.exec.on("ls -ld", reply{stdout: statLine})
	e3.exec.on("head", reply{stdout: strings.Repeat("a", files.ReadLimit+1)})
	f = e3.do("file.read", map[string]any{"rid": "r", "path": "/big"})
	assert.Equal(t, true, f.D["truncated"])
	assert.Len(t, mustDecode(t, f.D["data"].(string)), files.ReadLimit)
}

func mustDecode(t *testing.T, s string) []byte {
	t.Helper()
	b, err := base64.StdEncoding.DecodeString(s)
	require.NoError(t, err)
	return b
}

func TestRead_ErrorsMapToCodes(t *testing.T) {
	e := newEnv(t)
	e.exec.on("ls -ld -- /nope", reply{stderr: "ls: cannot access '/nope': No such file or directory", code: 2})
	e.exec.on("ls -ld -- /secret", reply{stderr: "ls: cannot access: Permission denied", code: 2})
	e.exec.on("ls -ld -- /etc", reply{stdout: "drwxr-xr-x 2 root root 4096 Nov 14 10:00 etc\n"})
	e.exec.on("ls -ld -- /odd", reply{stdout: "garbage\n"})
	assert.Equal(t, "not_found", e.do("file.read", map[string]any{"rid": "r", "path": "/nope"}).D["code"])
	assert.Equal(t, "forbidden", e.do("file.read", map[string]any{"rid": "r", "path": "/secret"}).D["code"])
	assert.Equal(t, "bad_request", e.do("file.read", map[string]any{"rid": "r", "path": "/etc"}).D["code"])
	assert.Equal(t, "bad_request", e.do("file.read", map[string]any{"rid": "r", "path": "/odd"}).D["code"])
}

func TestMissingUtilities_SaySo(t *testing.T) {
	e := newEnv(t)
	e.exec.rules = append(e.exec.rules, func([]string) *reply {
		return &reply{err: errors.New(`exec: "ls": executable file not found in $PATH`)}
	})
	f := e.do("file.list", map[string]any{"rid": "r", "path": "/"})
	assert.Equal(t, "no_utilities", f.D["code"])
	assert.Equal(t, "This container has no shell utilities; the file browser can't work here.", f.D["message"])

	e2 := newEnv(t)
	f = e2.do("file.list", map[string]any{"rid": "r", "path": "/"}) // no rule: exit code 127
	assert.Equal(t, "no_utilities", f.D["code"])
}

func TestCommandFailureShowsStderrAndUnknownTransportErrorsAreGeneric(t *testing.T) {
	e := newEnv(t)
	e.exec.on("ls", reply{stderr: "ls: weird failure\nsecond line", code: 1})
	f := e.do("file.list", map[string]any{"rid": "r", "path": "/"})
	assert.Equal(t, "command_failed", f.D["code"])
	assert.Equal(t, "ls: weird failure\nsecond line", f.D["message"])
	e.exec.on("ls", reply{code: 1})
	assert.Contains(t, e.do("file.list", map[string]any{"rid": "r", "path": "/"}).D["message"], "exit code 1")
	e.exec.on("ls", reply{err: errors.New("connection reset by peer")})
	f = e.do("file.list", map[string]any{"rid": "r", "path": "/"})
	assert.Equal(t, "internal", f.D["code"])
	assert.NotContains(t, fmt.Sprint(f.D), "connection reset")
}

func TestSave_ComparesWithExpectBeforeWritingAndWritesThroughTee(t *testing.T) {
	e := newEnv(t)
	e.exec.on("ls -ld -- /etc/hosts", reply{stdout: statLine})
	e.exec.on("tee", reply{})
	good := map[string]any{"size": 11, "modified": "2026-11-14T10:00:00Z"}
	f := e.do("file.save", map[string]any{"rid": "r", "path": "/etc/hosts", "data": b64("new"), "expect": good})
	require.Equal(t, "saved", f.Type, f.D)
	assert.Contains(t, f.D, "stat")
	e.exec.mu.Lock()
	var teeIn string
	for _, c := range e.exec.calls {
		if c.argv[0] == "tee" {
			teeIn = string(c.stdin)
			assert.Equal(t, []string{"tee", "--", "/etc/hosts"}, c.argv)
		}
	}
	e.exec.mu.Unlock()
	assert.Equal(t, "new", teeIn)
	assert.Equal(t, []string{"file-save:requested", "file-save:success"}, e.auditVerbs())
	assert.NotContains(t, e.out.String(), "new\"")
}

func TestSave_ConflictsDoNotRunTee(t *testing.T) {
	e := newEnv(t)
	e.exec.on("ls -ld -- /etc/hosts", reply{stdout: statLine})
	e.exec.on("ls -ld -- /gone", reply{stderr: "No such file or directory", code: 2})
	e.exec.on("tee", reply{})
	stale := map[string]any{"size": 5, "modified": "2026-11-14T10:00:00Z"}
	for _, r := range []map[string]any{
		{"rid": "r", "path": "/etc/hosts", "data": b64("x"), "expect": stale},
		{"rid": "r", "path": "/etc/hosts", "data": b64("x")},
		{"rid": "r", "path": "/gone", "data": b64("x"), "expect": stale},
	} {
		f := e.do("file.save", r)
		assert.Equal(t, "conflict", f.D["code"], r)
		assert.Equal(t, "The file changed in the container after you opened it.", f.D["message"])
	}
	for _, a := range e.exec.argvs() {
		assert.False(t, strings.HasPrefix(a, "tee"), a)
	}
}

func TestSave_CreatesANewFileWithoutExpectAndValidatesInput(t *testing.T) {
	e := newEnv(t)
	calls := 0
	e.exec.rules = append(e.exec.rules, func(argv []string) *reply {
		if argv[0] == "ls" {
			calls++
			if calls == 1 {
				return &reply{stderr: "No such file or directory", code: 2}
			}
			return &reply{stdout: statLine}
		}
		return &reply{}
	})
	assert.Equal(t, "saved", e.do("file.save", map[string]any{"rid": "r", "path": "/tmp/new", "data": b64("x")}).Type)
	assert.Equal(t, "bad_request", e.do("file.save", map[string]any{"rid": "r", "path": "/tmp/new", "data": "!!"}).D["code"])
	e.deps.MaxTransferBytes = 2
	e2 := newEnv(t)
	e2.deps.MaxTransferBytes = 2
	assert.Equal(t, "too_large", e2.do("file.save", map[string]any{"rid": "r", "path": "/tmp/new", "data": b64("abc")}).D["code"])
}

func TestDelete_UsesRmWithDashesAndRefusesTheRoot(t *testing.T) {
	e := newEnv(t)
	e.exec.on("rm", reply{})
	assert.Equal(t, "deleted", e.do("file.delete", map[string]any{"rid": "r", "path": "/tmp/-rf"}).Type)
	assert.Equal(t, "deleted", e.do("file.delete", map[string]any{"rid": "r", "path": "/tmp/dir", "recursive": true}).Type)
	assert.Equal(t, []string{"rm\x1f-f\x1f--\x1f/tmp/-rf", "rm\x1f-rf\x1f--\x1f/tmp/dir"}, e.exec.argvs())
	assert.Equal(t, "bad_request", e.do("file.delete", map[string]any{"rid": "r", "path": "/", "recursive": true}).D["code"])
	assert.Equal(t, "bad_request", e.do("file.delete", map[string]any{"rid": "r", "path": "//", "recursive": true}).D["code"])
	assert.Len(t, e.exec.argvs(), 2)
	assert.Equal(t, []string{"file-delete:requested", "file-delete:success", "file-delete:requested", "file-delete:success"}, e.auditVerbs())
}

// collect reads chunk frames until done or error.
func (e *env) collect() (data []byte, last frame) {
	e.t.Helper()
	for {
		f := e.next()
		if f.Type != "chunk" {
			return data, f
		}
		data = append(data, mustDecode(e.t, f.D["data"].(string))...)
	}
}

func TestDownload_SingleFileStreamsInChunks(t *testing.T) {
	e := newEnv(t)
	payload := strings.Repeat("0123456789", 12000) // 120 KB: three chunks
	e.exec.on("ls -ld -- /var/log/app.log", reply{stdout: "-rw-r--r-- 1 root root 120000 Nov 14 10:00 app.log\n"})
	e.exec.on("cat -- /var/log/app.log", reply{stdout: payload})
	e.open()
	require.NoError(t, e.chn.handle("file.download", map[string]any{"rid": "r", "path": "/var/log/app.log"}))
	var sizes []int
	var data []byte
	var done frame
	for {
		f := e.next()
		if f.Type != "chunk" {
			done = f
			break
		}
		assert.EqualValues(t, len(sizes), f.D["seq"])
		b := mustDecode(t, f.D["data"].(string))
		sizes = append(sizes, len(b))
		data = append(data, b...)
	}
	assert.Equal(t, []int{files.ChunkBytes, files.ChunkBytes, 120000 - 2*files.ChunkBytes}, sizes)
	assert.Equal(t, payload, string(data))
	assert.Equal(t, "done", done.Type)
	assert.EqualValues(t, 120000, done.D["bytes"])
	assert.Equal(t, false, done.D["tar"])
	assert.Equal(t, []string{"file-download:requested", "file-download:success"}, e.auditVerbs())
}

func tarOf(t *testing.T, entries ...tar.Header) string {
	t.Helper()
	var b bytes.Buffer
	w := tar.NewWriter(&b)
	for _, h := range entries {
		h := h
		if h.Typeflag == 0 {
			h.Typeflag = tar.TypeReg
		}
		require.NoError(t, w.WriteHeader(&h))
		if h.Typeflag == tar.TypeReg {
			_, err := w.Write(make([]byte, h.Size))
			require.NoError(t, err)
		}
	}
	require.NoError(t, w.Close())
	return b.String()
}

func TestDownload_DirectoryIsATarThatIsValidatedWhileItStreams(t *testing.T) {
	e := newEnv(t)
	e.exec.on("ls -ld -- /srv/site", reply{stdout: "drwxr-xr-x 2 root root 4096 Nov 14 10:00 site\n"})
	e.exec.on("tar --version", reply{})
	archive := tarOf(t, tar.Header{Name: "site/", Typeflag: tar.TypeDir, Mode: 0o755}, tar.Header{Name: "site/index.html", Size: 10, Mode: 0o644},
		tar.Header{Name: "site/current", Typeflag: tar.TypeSymlink, Linkname: "index.html"})
	e.exec.on("tar cf - -C /srv -- site", reply{stdout: archive})
	e.open()
	require.NoError(t, e.chn.handle("file.download", map[string]any{"rid": "r", "path": "/srv/site"}))
	data, last := e.collect()
	assert.Equal(t, "done", last.Type, last.D)
	assert.Equal(t, true, last.D["tar"])
	assert.Equal(t, archive, string(data), "forwarded unchanged")
}

func TestDownload_UnsafeArchiveEntriesAbortTheTransfer(t *testing.T) {
	for name, h := range map[string]tar.Header{
		"absolute name":     {Name: "/etc/passwd", Size: 1, Mode: 0o644},
		"dot dot":           {Name: "site/../../etc/passwd", Size: 1, Mode: 0o644},
		"outside the root":  {Name: "other/file", Size: 1, Mode: 0o644},
		"absolute symlink":  {Name: "site/link", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"},
		"escaping symlink":  {Name: "site/link", Typeflag: tar.TypeSymlink, Linkname: "../../etc/passwd"},
		"escaping hardlink": {Name: "site/hl", Typeflag: tar.TypeLink, Linkname: "../x"},
		"absolute hardlink": {Name: "site/hl", Typeflag: tar.TypeLink, Linkname: "/etc/shadow"},
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			e.exec.on("ls -ld -- /srv/site", reply{stdout: "drwxr-xr-x 2 root root 4096 Nov 14 10:00 site\n"})
			e.exec.on("tar --version", reply{})
			e.exec.on("tar cf", reply{stdout: tarOf(t, h)})
			e.open()
			require.NoError(t, e.chn.handle("file.download", map[string]any{"rid": "r", "path": "/srv/site"}))
			_, last := e.collect()
			assert.Equal(t, "error", last.Type)
			assert.Equal(t, "unsafe_archive", last.D["code"])
			assert.Equal(t, []string{"file-download:requested", "file-download:failure"}, e.auditVerbs())
		})
	}
}

func TestDownload_NeedsTarForDirectories_AndProbesOncePerChannel(t *testing.T) {
	e := newEnv(t)
	e.exec.on("ls -ld -- /srv/a", reply{stdout: "drwxr-xr-x 2 root root 4096 Nov 14 10:00 a\n"})
	e.exec.on("ls -ld -- /srv/b", reply{stdout: "drwxr-xr-x 2 root root 4096 Nov 14 10:00 b\n"})
	e.exec.on("tar --version", reply{code: 127, err: nil})
	f := e.do("file.download", map[string]any{"rid": "r", "path": "/srv/a"})
	assert.Equal(t, "no_tar", f.D["code"])
	assert.Equal(t, "This container has no tar; only single files can be transferred.", f.D["message"])
	f = e.do("file.download", map[string]any{"rid": "r", "path": "/srv/b"})
	assert.Equal(t, "no_tar", f.D["code"])
	probes := 0
	for _, a := range e.exec.argvs() {
		if strings.HasPrefix(a, "tar") {
			probes++
		}
	}
	assert.Equal(t, 1, probes)
}

func TestDownload_TooLargeCountsActualBytes(t *testing.T) {
	e := newEnv(t)
	e.deps.MaxTransferBytes = 100_000
	e.exec.on("ls -ld -- /big", reply{stdout: "-rw-r--r-- 1 root root 5 Nov 14 10:00 big\n"}) // the reported size lies
	e.exec.on("cat", reply{stdout: strings.Repeat("x", 150_000)})
	e.open()
	require.NoError(t, e.chn.handle("file.download", map[string]any{"rid": "r", "path": "/big"}))
	_, last := e.collect()
	assert.Equal(t, "too_large", last.D["code"])
}

func TestUpload_SingleFileThroughTarWhenAvailable(t *testing.T) {
	e := newEnv(t)
	e.exec.on("tar --version", reply{})
	e.exec.on("tar xf", reply{})
	e.open()
	require.NoError(t, e.chn.handle("file.upload.begin", map[string]any{"dir": "/tmp", "name": "a.txt"}))
	require.NoError(t, e.chn.handle("file.upload.chunk", map[string]any{"seq": 0, "data": b64("hello ")}))
	require.NoError(t, e.chn.handle("file.upload.chunk", map[string]any{"seq": 1, "data": b64("world")}))
	require.NoError(t, e.chn.handle("file.upload.end", map[string]any{"rid": "r"}))
	f := e.next()
	require.Equal(t, "uploaded", f.Type, f.D)
	assert.EqualValues(t, 11, f.D["bytes"])
	e.exec.mu.Lock()
	defer e.exec.mu.Unlock()
	last := e.exec.calls[len(e.exec.calls)-1]
	assert.Equal(t, []string{"tar", "xf", "-", "-C", "/tmp"}, last.argv)
	tr := tar.NewReader(bytes.NewReader(last.stdin))
	h, err := tr.Next()
	require.NoError(t, err)
	assert.Equal(t, "a.txt", h.Name)
	assert.EqualValues(t, 11, h.Size)
	body, _ := io.ReadAll(tr)
	assert.Equal(t, "hello world", string(body))
	assert.Equal(t, []string{"file-upload:requested", "file-upload:success"}, e.auditVerbs())
}

func TestUpload_WithoutTarUsesTee(t *testing.T) {
	e := newEnv(t)
	e.exec.on("tar --version", reply{code: 127})
	e.exec.on("tee", reply{})
	e.open()
	require.NoError(t, e.chn.handle("file.upload.begin", map[string]any{"dir": "/tmp", "name": "a b.txt"}))
	require.NoError(t, e.chn.handle("file.upload.chunk", map[string]any{"seq": 0, "data": b64("x")}))
	require.NoError(t, e.chn.handle("file.upload.end", map[string]any{"rid": "r"}))
	assert.Equal(t, "uploaded", e.next().Type)
	e.exec.mu.Lock()
	defer e.exec.mu.Unlock()
	last := e.exec.calls[len(e.exec.calls)-1]
	assert.Equal(t, []string{"tee", "--", "/tmp/a b.txt"}, last.argv)
	assert.Equal(t, "x", string(last.stdin))
}

func TestUpload_ClientBuiltTarIsValidatedBeforeAnythingRuns(t *testing.T) {
	e := newEnv(t)
	e.exec.on("tar --version", reply{})
	e.exec.on("tar xf", reply{})
	e.open()
	good := tarOf(t, tar.Header{Name: "dir/", Typeflag: tar.TypeDir, Mode: 0o755}, tar.Header{Name: "dir/a", Size: 3, Mode: 0o644})
	require.NoError(t, e.chn.handle("file.upload.begin", map[string]any{"dir": "/tmp", "tar": true}))
	require.NoError(t, e.chn.handle("file.upload.chunk", map[string]any{"seq": 0, "data": b64(good)}))
	require.NoError(t, e.chn.handle("file.upload.end", map[string]any{"rid": "r"}))
	assert.Equal(t, "uploaded", e.next().Type)

	evil := tarOf(t, tar.Header{Name: "../../etc/cron.d/x", Size: 3, Mode: 0o644})
	require.NoError(t, e.chn.handle("file.upload.begin", map[string]any{"dir": "/tmp", "tar": true}))
	require.NoError(t, e.chn.handle("file.upload.chunk", map[string]any{"seq": 0, "data": b64(evil)}))
	require.NoError(t, e.chn.handle("file.upload.end", map[string]any{"rid": "r2"}))
	f := e.next()
	assert.Equal(t, "unsafe_archive", f.D["code"])
	extracts := 0
	for _, a := range e.exec.argvs() {
		if strings.HasPrefix(a, "tar\x1fxf") {
			extracts++
		}
	}
	assert.Equal(t, 1, extracts, "the unsafe archive was never extracted")

	require.NoError(t, e.chn.handle("file.upload.begin", map[string]any{"dir": "/tmp", "tar": true}))
	require.NoError(t, e.chn.handle("file.upload.chunk", map[string]any{"seq": 0, "data": b64("not a tar archive at all, really not")}))
	require.NoError(t, e.chn.handle("file.upload.end", map[string]any{"rid": "r3"}))
	assert.Equal(t, "bad_request", e.next().D["code"])
}

func TestUpload_Limits_Ordering_AndNames(t *testing.T) {
	e := newEnv(t)
	e.deps.MaxTransferBytes = 10
	e.open()
	assert.ErrorIs(t, e.chn.handle("file.upload.chunk", map[string]any{"seq": 0, "data": b64("x")}), apperr.ErrInvalid, "no begin")
	assert.ErrorIs(t, e.chn.handle("file.upload.begin", map[string]any{"dir": "tmp", "name": "a"}), apperr.ErrInvalid)
	for _, bad := range []string{"", ".", "..", "a/b", "a\x00b"} {
		assert.ErrorIs(t, e.chn.handle("file.upload.begin", map[string]any{"dir": "/tmp", "name": bad}), apperr.ErrInvalid, bad)
	}
	require.NoError(t, e.chn.handle("file.upload.begin", map[string]any{"dir": "/tmp", "name": "a"}))
	assert.ErrorIs(t, e.chn.handle("file.upload.chunk", map[string]any{"seq": 1, "data": b64("x")}), apperr.ErrInvalid, "out of order")
	require.NoError(t, e.chn.handle("file.upload.begin", map[string]any{"dir": "/tmp", "name": "a"}))
	assert.ErrorIs(t, e.chn.handle("file.upload.chunk", map[string]any{"seq": 0, "data": b64("this is far too long")}), apperr.ErrTooLarge)
	assert.ErrorIs(t, e.chn.handle("file.upload.chunk", map[string]any{"seq": 0, "data": "!!"}), apperr.ErrInvalid)
	require.NoError(t, e.chn.handle("file.upload.end", map[string]any{"rid": "r"}))
	assert.Equal(t, "bad_request", e.next().D["code"], "the upload was dropped")
}

func TestAuditFailureRefusesEveryAuditedAction(t *testing.T) {
	e := newEnv(t)
	db := storetest.New(t)
	e.deps.Audit = audit.NewRecorder(db, e.clock, e.out, func() string { return "x" })
	require.NoError(t, db.Close())
	e.exec.on("ls", reply{stdout: statLine})
	for _, op := range []string{"file.read", "file.delete", "file.download", "file.save"} {
		f := e.do(op, map[string]any{"rid": "r", "path": "/etc/hosts", "data": b64("x"), "expect": map[string]any{"size": 1, "modified": "x"}})
		assert.Equal(t, "internal", f.D["code"], op)
		assert.Equal(t, "The action was refused because it could not be recorded.", f.D["message"])
	}
	assert.Empty(t, e.exec.argvs())
}

func TestBadOpsAndCloseCancelsRunningCommands(t *testing.T) {
	e := newEnv(t)
	e.open()
	assert.ErrorIs(t, e.chn.handle("file.nope", map[string]any{}), apperr.ErrInvalid)
	assert.ErrorIs(t, e.chn.handle("file.list", "x"), apperr.ErrInvalid)

	started, release := make(chan struct{}), make(chan struct{})
	e.exec.rules = append(e.exec.rules, func(argv []string) *reply {
		if argv[0] != "ls" {
			return nil
		}
		return &reply{write: func(io.Writer) error {
			close(started)
			<-release
			return context.Canceled
		}}
	})
	require.NoError(t, e.chn.handle("file.list", map[string]any{"rid": "r", "path": "/"}))
	testutil.Recv(t, started)
	go close(release)
	e.chn.close()
	select {
	case f := <-e.sink.frames:
		t.Fatalf("a closed channel sends nothing more: %v", f)
	default:
	}
	assert.NoError(t, e.chn.handle("file.list", map[string]any{"rid": "r", "path": "/"}), "ops after close are ignored")
}
