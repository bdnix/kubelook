package egress_test

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bdnix/kubelook/internal/egress"
)

// connect sends a CONNECT request to the proxy and returns the connection and the status line's reason phrase.
func connect(t *testing.T, p *egress.Proxy, target, credential string) (net.Conn, string, int) {
	t.Helper()
	u, err := p.ProxyFunc()(&http.Request{URL: &url.URL{Scheme: "https", Host: target}})
	require.NoError(t, err)
	conn, err := (&net.Dialer{}).DialContext(context.Background(), "tcp", u.Host)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	auth := ""
	if credential == "" {
		pw, _ := u.User.Password()
		credential = base64.StdEncoding.EncodeToString([]byte(u.User.Username() + ":" + pw))
	}
	if credential != "none" {
		auth = "Proxy-Authorization: Basic " + credential + "\r\n"
	}
	_, err = fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n%s\r\n", target, target, auth)
	require.NoError(t, err)
	resp, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: http.MethodConnect})
	require.NoError(t, err)
	_ = resp.Body.Close()
	reason := resp.Status[len("000 "):]
	return conn, reason, resp.StatusCode
}

func newProxy(t *testing.T, blocked bool) *egress.Proxy {
	t.Helper()
	p, err := egress.NewProxy(blocked)
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })
	return p
}

func TestProxy_Blocked_RefusesATunnelToALoopbackAddress(t *testing.T) {
	p := newProxy(t, true)
	for _, target := range []string{"127.0.0.1:6443", "localhost:6443", "169.254.169.254:80"} {
		_, reason, code := connect(t, p, target, "")
		assert.Equal(t, 403, code, target)
		assert.Equal(t, egress.RefusalReason, reason, target)
	}
}

func TestProxy_NotBlocking_TunnelsBytesBothWays(t *testing.T) {
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = c.Close() }()
		_, _ = io.Copy(c, c) // echo
	}()
	p := newProxy(t, false)

	conn, reason, code := connect(t, p, ln.Addr().String(), "")
	require.Equal(t, 200, code, reason)
	_, err = conn.Write([]byte("hello"))
	require.NoError(t, err)
	buf := make([]byte, 5)
	_, err = io.ReadFull(conn, buf)
	require.NoError(t, err)
	assert.Equal(t, "hello", string(buf))
}

func TestProxy_WithoutTheRightCredential_AnswersProxyAuthRequired(t *testing.T) {
	p := newProxy(t, false)
	_, _, code := connect(t, p, "10.0.0.1:443", "none")
	assert.Equal(t, 407, code)
	_, _, code = connect(t, p, "10.0.0.1:443", base64.StdEncoding.EncodeToString([]byte("x:y")))
	assert.Equal(t, 407, code)
}

func TestProxy_OnlyConnect(t *testing.T) {
	p := newProxy(t, false)
	u, err := p.ProxyFunc()(&http.Request{URL: &url.URL{Scheme: "https", Host: "x:1"}})
	require.NoError(t, err)
	pw, _ := u.User.Password()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+u.Host+"/", nil)
	require.NoError(t, err)
	req.SetBasicAuth(u.User.Username(), pw)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
}

func TestProxy_AnUnreachableTarget_AnswersBadGateway(t *testing.T) {
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	require.NoError(t, ln.Close()) // nothing listens there now
	p := newProxy(t, false)
	_, _, code := connect(t, p, addr, "")
	assert.Equal(t, 502, code)
}

func TestProxy_Close_ClosesOpenTunnelsAndStopsListening(t *testing.T) {
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		c, err := ln.Accept()
		if err == nil {
			defer func() { _ = c.Close() }()
			_, _ = io.Copy(io.Discard, c)
		}
	}()
	p, err := egress.NewProxy(false)
	require.NoError(t, err)
	conn, _, code := connect(t, p, ln.Addr().String(), "")
	require.Equal(t, 200, code)

	require.NoError(t, p.Close())

	_, err = conn.Read(make([]byte, 1))
	assert.Error(t, err, "the tunnel was closed")
	_, err = (&net.Dialer{}).DialContext(t.Context(), "tcp", p.Addr())
	assert.Error(t, err, "nothing listens any more")
}
