// Package egress keeps the server from connecting to itself or to a cloud metadata service on behalf of a user:
// the addresses a user supplies (a kubeconfig's server and identity provider, a Helm repository, an OCI registry)
// may not point at loopback, link-local or unspecified addresses. It is not an egress allowlist; the deployer's
// firewall is still responsible for everything else.
package egress

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"syscall"
	"time"

	"github.com/bdnix/kubelook/internal/apperr"
)

// ErrBlocked is the answer for an address the guard refuses. It is an API error, so every layer that maps errors
// answers 422 with this message.
var ErrBlocked = &apperr.APIError{
	Status: 422, Code: "unprocessable",
	Message: "This address isn't allowed: Kubelook doesn't connect to local or link-local addresses from a kubeconfig.",
}

const (
	dialTimeout   = 30 * time.Second
	dialKeepAlive = 30 * time.Second
)

// blockedAddr reports whether ip is loopback, link-local or unspecified, in either IP version and in the
// IPv4-mapped form of an IPv4 address.
func blockedAddr(ip netip.Addr) bool {
	ip = ip.Unmap()
	return ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified()
}

// Control is a net.Dialer.Control function: it sees the address a connection attempt resolved to, after any DNS
// lookup and for every redirect, and refuses a blocked one with ErrBlocked. With blocked false it allows everything.
func Control(blocked bool) func(network, address string, c syscall.RawConn) error {
	return func(_, address string, _ syscall.RawConn) error {
		if !blocked {
			return nil
		}
		ap, err := netip.ParseAddrPort(address)
		if err != nil || blockedAddr(ap.Addr()) {
			return ErrBlocked
		}
		return nil
	}
}

// DialContext returns a dial function that applies Control.
func DialContext(blocked bool) func(ctx context.Context, network, address string) (net.Conn, error) {
	d := &net.Dialer{Timeout: dialTimeout, KeepAlive: dialKeepAlive, Control: Control(blocked)}
	return d.DialContext
}

// Transport returns a clone of base that dials through Control. With blocked it also ignores the proxy of the
// environment, which would move the connection to the proxy and defeat the check. With blocked false it is just
// a clone of base.
func Transport(base *http.Transport, blocked bool) *http.Transport {
	t := base.Clone()
	if blocked {
		t.Proxy = nil
		t.DialContext = DialContext(true)
	}
	return t
}

// CheckURL refuses an address whose host is a blocked IP literal. A host name passes: it is checked, when it is
// resolved, by Control. An address that does not parse passes too; whoever uses it reports that.
func CheckURL(raw string, blocked bool) error {
	if !blocked {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		// A bare "host:port", as a kubeconfig may give a server.
		if u, err = url.Parse("//" + raw); err != nil {
			return nil //nolint:nilerr // not an address this check can judge
		}
	}
	if ip, err := netip.ParseAddr(u.Hostname()); err == nil && blockedAddr(ip) {
		return ErrBlocked
	}
	return nil
}
