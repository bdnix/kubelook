//go:build integration

package integration

import (
	"archive/tar"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdnix/kubelook/test/integration/it"
)

type fileSession struct {
	t   *testing.T
	ch  *it.Channel
	rid int
}

func openFiles(t *testing.T, sub string) *fileSession {
	t.Helper()
	c := devopsClient(t, sub)
	pod := podOf(t, "alpha", "shell")
	ch := c.WS().Open("file.open", map[string]any{"cluster": "v:alpha", "namespace": "demo", "pod": pod})
	return &fileSession{t: t, ch: ch}
}

// call sends an op with a fresh request id and returns the type and data of the reply for it.
func (f *fileSession) call(op string, d map[string]any) (string, map[string]any) {
	f.t.Helper()
	f.rid++
	rid := "r" + strconv.Itoa(f.rid)
	d["rid"] = rid
	f.ch.Send(op, d)
	for {
		kind, raw := f.ch.NextOf(60*time.Second, "list", "content", "saved", "deleted", "done", "uploaded", "error")
		if kind == "" {
			f.t.Fatalf("no answer to %s", op)
		}
		var out map[string]any
		require.NoError(f.t, json.Unmarshal(raw, &out))
		if out["rid"] == rid {
			return kind, out
		}
	}
}

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

func TestFiles_ListSaveReadAndAStaleSaveConflicts(t *testing.T) {
	f := openFiles(t, "files-basic")

	kind, out := f.call("file.list", map[string]any{"path": "/etc"})
	require.Equal(t, "list", kind, "%v", out)
	assert.Contains(t, jsonText(t, out["entries"]), "passwd")

	kind, out = f.call("file.save", map[string]any{"path": "/tmp/a.txt", "data": b64("hello")})
	require.Equal(t, "saved", kind, "%v", out)
	kind, out = f.call("file.read", map[string]any{"path": "/tmp/a.txt"})
	require.Equal(t, "content", kind, "%v", out)
	raw, err := base64.StdEncoding.DecodeString(out["data"].(string))
	require.NoError(t, err)
	assert.Equal(t, "hello", string(raw))

	// the file is changed behind the editor's back, then saved with the old stat
	stale := out["stat"]
	_, _ = f.call("file.save", map[string]any{"path": "/tmp/a.txt", "data": b64("changed elsewhere!")})
	kind, out = f.call("file.save", map[string]any{"path": "/tmp/a.txt", "data": b64("mine"), "expect": stale})
	assert.Equal(t, "error", kind)
	assert.Equal(t, "conflict", out["code"])
}

func TestFiles_ANameWithShellSyntaxIsCreatedLiterally(t *testing.T) {
	f := openFiles(t, "files-literal")
	kind, out := f.call("file.save", map[string]any{"path": "/tmp/$(touch pwned)", "data": b64("x")})
	require.Equal(t, "saved", kind, "%v", out)
	_, out = f.call("file.list", map[string]any{"path": "/tmp"})
	names := jsonText(t, out["entries"])
	assert.Contains(t, names, "$(touch pwned)")
	assert.NotContains(t, names, `"name":"pwned"`)
}

func tarOf(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for name, body := range files {
		require.NoError(t, tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}))
		_, err := tw.Write([]byte(body))
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	return buf.Bytes()
}

func TestFiles_UploadATarAndDownloadTheDirectoryAsATar(t *testing.T) {
	f := openFiles(t, "files-transfer")
	_, _ = f.call("file.save", map[string]any{"path": "/tmp/up/.keep", "data": b64("")}) // makes /tmp/up

	f.ch.Send("file.upload.begin", map[string]any{"dir": "/tmp/up", "tar": true})
	f.ch.Send("file.upload.chunk", map[string]any{"seq": 0, "data": base64.StdEncoding.EncodeToString(tarOf(t, map[string]string{"one.txt": "1", "two.txt": "2"}))})
	kind, out := f.call("file.upload.end", map[string]any{})
	require.Equal(t, "uploaded", kind, "%v", out)

	f.rid++
	rid := "r" + strconv.Itoa(f.rid)
	f.ch.Send("file.download", map[string]any{"rid": rid, "path": "/tmp/up"})
	var archive bytes.Buffer
	for {
		kind, raw := f.ch.NextOf(60*time.Second, "chunk", "done", "error")
		require.NotEmpty(t, kind, "the download ended without done")
		var d map[string]any
		require.NoError(t, json.Unmarshal(raw, &d))
		if kind == "error" {
			t.Fatalf("download failed: %v", d)
		}
		if kind == "chunk" {
			b, err := base64.StdEncoding.DecodeString(d["data"].(string))
			require.NoError(t, err)
			archive.Write(b)
			continue
		}
		break
	}
	found := map[string]bool{}
	tr := tar.NewReader(&archive)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		found[strings.TrimPrefix(h.Name, "./")] = true
	}
	assert.True(t, found["one.txt"] || found["up/one.txt"], "one.txt is in the archive: %v", found)
	assert.True(t, found["two.txt"] || found["up/two.txt"], "two.txt is in the archive: %v", found)
}

func TestFiles_AnUploadOverTheLimitIsTooLarge(t *testing.T) {
	f := openFiles(t, "files-large")
	f.ch.Send("file.upload.begin", map[string]any{"dir": "/tmp", "name": "big.bin"})
	chunk := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte("x"), 256<<10))
	for seq := 0; seq < 8; seq++ { // 2 MiB; the configured limit is 1 MiB
		f.ch.Send("file.upload.chunk", map[string]any{"seq": seq, "data": chunk})
	}
	f.ch.Send("file.upload.end", map[string]any{"rid": "big"})
	kind, raw := f.ch.NextOf(30*time.Second, "error", "uploaded")
	require.Equal(t, "error", kind, "an oversized upload must be refused")
	assert.Contains(t, string(raw), "too_large")
}

func jsonText(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return string(b)
}
