package egress_test

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	"github.com/bdnix/kubelook/internal/egress"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

func TestControl_RefusesLocalAndLinkLocalAddresses(t *testing.T) {
	control := egress.Control(true)
	for _, addr := range []string{
		"127.0.0.1:443", "127.255.255.254:80", "[::1]:443", "169.254.169.254:80", "169.254.0.1:443", "[fe80::1]:443",
		"0.0.0.0:80", "[::]:80", "[::ffff:127.0.0.1]:443", "[::ffff:169.254.169.254]:80", "[::ffff:0.0.0.0]:80",
	} {
		err := control("tcp", addr, nil)
		assert.ErrorIs(t, err, egress.ErrBlocked, addr)
	}
}

func TestControl_AllowsPrivateAndPublicAddresses(t *testing.T) {
	control := egress.Control(true)
	for _, addr := range []string{"10.0.0.1:443", "192.168.1.1:443", "172.16.0.1:443", "8.8.8.8:53", "[2001:4860:4860::8888]:443", "[fd00::1]:443"} {
		assert.NoError(t, control("tcp", addr, nil), addr)
	}
}

func TestControl_NotBlocking_AllowsEverything(t *testing.T) {
	control := egress.Control(false)
	assert.NoError(t, control("tcp", "127.0.0.1:443", nil))
	assert.NoError(t, control("tcp", "169.254.169.254:80", nil))
}

func TestControl_AnAddressItCannotParse_IsRefused(t *testing.T) {
	assert.ErrorIs(t, egress.Control(true)("tcp", "not-an-address", nil), egress.ErrBlocked)
}

func TestErrBlocked_SaysWhyAndIsUnprocessable(t *testing.T) {
	assert.Equal(t, "unprocessable: This address isn't allowed: Kubelook doesn't connect to local or link-local addresses from a kubeconfig.", egress.ErrBlocked.Error())
	assert.Equal(t, 422, egress.ErrBlocked.Status)
	assert.Equal(t, "unprocessable", egress.ErrBlocked.Code)
	assert.Equal(t, "This address isn't allowed: Kubelook doesn't connect to local or link-local addresses from a kubeconfig.", egress.ErrBlocked.Message)
}

func get(t *testing.T, tr *http.Transport, url string) error {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	require.NoError(t, err)
	client := &http.Client{Transport: tr}
	resp, err := client.Do(req)
	if err == nil {
		_ = resp.Body.Close()
	}
	return err
}

func TestTransport_Blocked_RefusesALoopbackServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	t.Cleanup(srv.Close)
	tr := egress.Transport(http.DefaultTransport.(*http.Transport), true)
	t.Cleanup(tr.CloseIdleConnections)

	err := get(t, tr, srv.URL)

	require.Error(t, err)
	assert.ErrorIs(t, err, egress.ErrBlocked)
}

func TestTransport_NotBlocking_ReachesALoopbackServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	t.Cleanup(srv.Close)
	tr := egress.Transport(http.DefaultTransport.(*http.Transport), false)
	t.Cleanup(tr.CloseIdleConnections)

	assert.NoError(t, get(t, tr, srv.URL))
}

func TestTransport_ANameThatResolvesToALoopbackAddress_IsRefused(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	t.Cleanup(srv.Close)
	_, port, err := net.SplitHostPort(srv.Listener.Addr().String())
	require.NoError(t, err)
	tr := egress.Transport(http.DefaultTransport.(*http.Transport), true)
	t.Cleanup(tr.CloseIdleConnections)

	err = get(t, tr, "http://localhost:"+port)

	assert.ErrorIs(t, err, egress.ErrBlocked, "the check is made on the address the name resolved to")
}

func TestTransport_IgnoresTheProxyOfTheEnvironment(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://proxy.example.org:3128")
	base := http.DefaultTransport.(*http.Transport).Clone()
	require.NotNil(t, base.Proxy)

	assert.Nil(t, egress.Transport(base, true).Proxy)
	assert.NotNil(t, egress.Transport(base, false).Proxy, "without the guard the transport is the base")
	assert.NotNil(t, base.Proxy, "the base is not changed")
}

func TestDial_Blocked_RefusesALoopbackAddress(t *testing.T) {
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })

	_, err = egress.DialContext(true)(context.Background(), "tcp", ln.Addr().String())
	assert.ErrorIs(t, err, egress.ErrBlocked)

	conn, err := egress.DialContext(false)(context.Background(), "tcp", ln.Addr().String())
	require.NoError(t, err)
	_ = conn.Close()
}

func TestCheckURL(t *testing.T) {
	for _, tc := range []struct {
		url     string
		blocked bool
		err     bool
	}{
		{"https://127.0.0.1:6443", true, true},
		{"https://[::1]:6443", true, true},
		{"http://169.254.169.254/latest", true, true},
		{"https://[::ffff:127.0.0.1]/", true, true},
		{"https://0.0.0.0", true, true},
		{"https://10.0.0.1:6443", true, false},
		{"https://kubernetes.example.org", true, false},
		{"https://127.0.0.1:6443", false, false},
		{"://bad", true, false},
	} {
		err := egress.CheckURL(tc.url, tc.blocked)
		if tc.err {
			assert.True(t, errors.Is(err, egress.ErrBlocked), tc.url)
		} else {
			assert.NoError(t, err, tc.url)
		}
	}
}
