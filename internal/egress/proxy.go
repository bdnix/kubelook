package egress

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// RefusalReason is the reason phrase of the answer to a tunnel the guard refused. The WebSocket client of
// client-go reports only that phrase, so whoever uses the proxy recognises a refusal by it.
const RefusalReason = "kubelook-egress-blocked"

const proxyHeaderTimeout = 10 * time.Second

// Proxy is an HTTP CONNECT proxy on a loopback port of this process that dials through the guard. client-go's
// WebSocket exec does not dial through rest.Config.Dial, but it does honour rest.Config.Proxy; routing it through
// this proxy puts the check on the resolved address of every exec connection, whatever name it was reached by.
// The proxy accepts only requests with its own random credential, so nothing else in the pod can use it.
type Proxy struct {
	ln       net.Listener
	srv      *http.Server
	dial     func(ctx context.Context, network, address string) (net.Conn, error)
	user     string
	password string

	mu     sync.Mutex
	conns  map[net.Conn]struct{}
	closed bool
	wg     sync.WaitGroup
}

// NewProxy starts the proxy. With blocked false it tunnels anywhere (tests); the server passes true.
func NewProxy(blocked bool) (*Proxy, error) {
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("starting the egress proxy: %w", err)
	}
	p := &Proxy{ln: ln, dial: DialContext(blocked), user: "kubelook", password: secret(), conns: map[net.Conn]struct{}{}}
	p.srv = &http.Server{Handler: http.HandlerFunc(p.serve), ReadHeaderTimeout: proxyHeaderTimeout}
	p.wg.Go(func() { _ = p.srv.Serve(ln) })
	return p, nil
}

func secret() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b) // crypto/rand.Read never fails
	return hex.EncodeToString(b)
}

// Addr is the host:port the proxy listens on.
func (p *Proxy) Addr() string { return p.ln.Addr().String() }

// ProxyFunc is a rest.Config.Proxy function: every request goes through this proxy.
func (p *Proxy) ProxyFunc() func(*http.Request) (*url.URL, error) {
	u := &url.URL{Scheme: "http", Host: p.Addr(), User: url.UserPassword(p.user, p.password)}
	return func(*http.Request) (*url.URL, error) { return u, nil }
}

// Close stops listening, closes every open tunnel and waits for them.
func (p *Proxy) Close() error {
	p.mu.Lock()
	p.closed = true
	for c := range p.conns {
		_ = c.Close()
	}
	p.mu.Unlock()
	err := p.srv.Close()
	p.wg.Wait()
	return err
}

func (p *Proxy) authorized(r *http.Request) bool {
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte(p.user+":"+p.password))
	return subtle.ConstantTimeCompare([]byte(r.Header.Get("Proxy-Authorization")), []byte(want)) == 1
}

// reply writes a status line with the given reason phrase on the hijacked connection.
func reply(c net.Conn, code int, reason string) {
	_, _ = fmt.Fprintf(c, "HTTP/1.1 %d %s\r\n\r\n", code, reason)
}

func (p *Proxy) track(c net.Conn) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return false
	}
	p.conns[c] = struct{}{}
	p.wg.Add(1)
	return true
}

func (p *Proxy) untrack(c net.Conn) {
	p.mu.Lock()
	delete(p.conns, c)
	p.mu.Unlock()
	p.wg.Done()
}

func (p *Proxy) serve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodConnect {
		http.Error(w, "only CONNECT", http.StatusMethodNotAllowed)
		return
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "cannot hijack", http.StatusInternalServerError)
		return
	}
	client, _, err := hj.Hijack()
	if err != nil {
		return
	}
	if !p.track(client) {
		_ = client.Close()
		return
	}
	defer p.untrack(client)
	defer func() { _ = client.Close() }()

	if !p.authorized(r) {
		reply(client, http.StatusProxyAuthRequired, "Proxy Authentication Required")
		return
	}
	target, err := p.dial(r.Context(), "tcp", r.Host)
	switch {
	case errors.Is(err, ErrBlocked):
		reply(client, http.StatusForbidden, RefusalReason)
		return
	case err != nil:
		reply(client, http.StatusBadGateway, "Bad Gateway")
		return
	}
	defer func() { _ = target.Close() }()
	if !p.track(target) {
		return
	}
	defer p.untrack(target)
	reply(client, http.StatusOK, "Connection established")

	done := make(chan struct{}, 2)
	pipe := func(dst, src net.Conn) {
		_, _ = io.Copy(dst, src)
		_ = dst.Close()
		_ = src.Close()
		done <- struct{}{}
	}
	go pipe(target, client)
	go pipe(client, target)
	<-done
	<-done
}
