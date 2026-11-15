package acquisitionhttp

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/http/httpproxy"
)

func requestFor(t *testing.T, raw string) *http.Request {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, raw, nil)
	if err != nil {
		t.Fatal(err)
	}
	return request
}

func grantFor(t *testing.T, raw string) Grant {
	t.Helper()
	grant, err := ExplicitEndpoint(requestFor(t, raw).URL)
	if err != nil {
		t.Fatal(err)
	}
	return grant
}

func fixedLookup(addresses ...string) resolver {
	return func(context.Context, string, string) ([]netip.Addr, error) {
		var result []netip.Addr
		for _, address := range addresses {
			result = append(result, netip.MustParseAddr(address))
		}
		return result, nil
	}
}

func transportFor(t *testing.T, initial string, grant Grant, base http.RoundTripper, lookup resolver) *transport {
	t.Helper()
	checked, err := newTransport(requestFor(t, initial).URL, grant, base, lookup)
	if err != nil {
		t.Fatal(err)
	}
	return checked
}

type responseTransport func(*http.Request) (*http.Response, error)

func (f responseTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func roundTripError(t *testing.T, transport http.RoundTripper, request *http.Request) error {
	t.Helper()
	response, err := transport.RoundTrip(request)
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}
	return err
}

func TestGrantAuthority(t *testing.T) {
	t.Parallel()
	grant := grantFor(t, "https://bücher.example:443/private?token=secret")
	for _, test := range []struct {
		endpoint string
		allowed  bool
	}{
		{"https://xn--bcher-kva.example/other", true},
		{"https://BÜCHER.example:443/", true},
		{"http://xn--bcher-kva.example:443/", false},
		{"https://xn--bcher-kva.example:8443/", false},
		{"https://xn--bcher-kva.example./", false},
		{"https://other.example/", false},
	} {
		if allowed := grant.Allows(requestFor(t, test.endpoint).URL); allowed != test.allowed {
			t.Errorf("Allows(%s) = %v", test.endpoint, allowed)
		}
	}
	if (Grant{}).Allows(requestFor(t, "https://example.org/").URL) {
		t.Fatal("zero grant authorized an endpoint")
	}
	ipv6 := grantFor(t, "https://[FE80::1%25Ethernet]:443/")
	if !ipv6.Allows(requestFor(t, "https://[fe80::1%25Ethernet]/").URL) || ipv6.Allows(requestFor(t, "https://[fe80::1%25ethernet]/").URL) {
		t.Fatal("IPv6 identity or zone changed")
	}
	for _, endpoint := range []*url.URL{nil, {Scheme: "file", Path: "/tmp/plugin"}, {Scheme: "https"}, {Scheme: "https", Host: "example.org", User: url.User("secret")}, {Scheme: "https", Host: "example.org:invalid"}, {Scheme: "https", Host: "example.org:65536"}} {
		if _, err := ExplicitEndpoint(endpoint); !errors.Is(err, ErrInvalidEndpoint) {
			t.Fatalf("invalid grant endpoint: %v", err)
		}
		if _, err := Transport(endpoint, Grant{}, Connections{}); !errors.Is(err, ErrInvalidEndpoint) {
			t.Fatalf("invalid transport endpoint: %v", err)
		}
	}
}

func TestAutomaticPrivateDestinationsNeverDial(t *testing.T) {
	t.Parallel()
	base := &http.Transport{DialContext: func(context.Context, string, string) (net.Conn, error) {
		t.Error("private destination reached dialer")
		return nil, errors.New("unexpected dial")
	}}
	for _, address := range []string{"127.0.0.1", "10.0.0.1", "169.254.169.254", "100.64.0.1", "192.0.2.1", "198.18.0.1", "240.0.0.1", "::1", "fc00::1", "fe80::1", "::ffff:127.0.0.1", "64:ff9b::a00:1", "2001:db8::1", "2002:7f00:1::", "3fff::1"} {
		t.Run(address, func(t *testing.T) {
			t.Parallel()
			for _, target := range []string{"https://" + net.JoinHostPort(address, "443") + "/plugin", "https://configured.example/plugin"} {
				checked := transportFor(t, target, Grant{}, base, fixedLookup(address))
				if err := roundTripError(t, checked, requestFor(t, target)); !errors.Is(err, ErrPrivateDestination) {
					t.Fatalf("private initial destination accepted: %v", err)
				}
			}
		})
	}
	checked := transportFor(t, "https://configured.example/", Grant{}, base, fixedLookup("8.8.8.8", "10.0.0.1"))
	if err := roundTripError(t, checked, requestFor(t, "https://configured.example/plugin")); !errors.Is(err, ErrPrivateDestination) {
		t.Fatalf("mixed DNS answers accepted: %v", err)
	}
	for _, address := range []string{"8.8.8.8", "1.1.1.1", "2606:4700:4700::1111", "2001:4860:4860::8888", "::ffff:8.8.8.8"} {
		if !publicAddress(netip.MustParseAddr(address)) {
			t.Errorf("public address refused: %s", address)
		}
	}
}

func TestExplicitInitialPreservesOpaqueTransport(t *testing.T) {
	t.Parallel()
	request := requestFor(t, "https://[::1]:8443/private?signature=secret")
	called := false
	base := responseTransport(func(actual *http.Request) (*http.Response, error) {
		called = true
		if actual != request {
			t.Error("explicit request was changed")
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("")), Request: actual}, nil
	})
	checked := transportFor(t, request.URL.String(), grantFor(t, request.URL.String()), base, fixedLookup("127.0.0.1"))
	response, err := checked.RoundTrip(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if !called {
		t.Fatal("explicit endpoint never reached host transport")
	}
	for _, target := range []string{"https://other.example/plugin", "https://[::1]:9443/plugin", "http://[::1]:8443/plugin"} {
		if err := roundTripError(t, checked, requestFor(t, target)); !errors.Is(err, ErrUnsupportedTransport) {
			t.Fatalf("opaque transport accepted different authority: %v", err)
		}
	}
	checked = transportFor(t, request.URL.String(), Grant{}, base, fixedLookup("8.8.8.8"))
	if err := roundTripError(t, checked, request); !errors.Is(err, ErrUnsupportedTransport) {
		t.Fatalf("automatic opaque transport bypassed pinning: %v", err)
	}
}

func TestExplicitGrantNeverAuthorizesOtherInitialOrRedirectAuthority(t *testing.T) {
	t.Parallel()
	initial := "https://origin.example:8443/"
	base := &http.Transport{DialContext: func(context.Context, string, string) (net.Conn, error) {
		t.Error("ungranted private endpoint reached dialer")
		return nil, errors.New("unexpected dial")
	}}
	grant := grantFor(t, initial)
	checked := transportFor(t, initial, grant, base, fixedLookup("10.0.0.1"))
	for _, target := range []string{"https://other.example/plugin", "https://origin.example:9443/plugin", "http://origin.example:8443/plugin", "https://origin.example.:8443/plugin"} {
		if err := roundTripError(t, checked, requestFor(t, target)); !errors.Is(err, ErrPrivateDestination) {
			t.Fatalf("private redirect accepted: %s: %v", target, err)
		}
	}
	checked = transportFor(t, "https://other.example/", grant, base, fixedLookup("10.0.0.1"))
	if err := roundTripError(t, checked, requestFor(t, initial)); !errors.Is(err, ErrPrivateDestination) {
		t.Fatalf("grant for different initial acquisition leaked: %v", err)
	}
}

// A scripted connection exercises HTTP/CONNECT/TLS serialization without a
// listener, server, DNS lookup, certificate generation or external connection.
type scriptedConnection struct {
	mu     sync.Mutex
	input  *bytes.Reader
	output bytes.Buffer
}

func (c *scriptedConnection) Read(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.input.Read(p)
}

func (c *scriptedConnection) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.output.Write(p)
}

func (*scriptedConnection) Close() error { return nil }
func (*scriptedConnection) LocalAddr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1}
}

func (*scriptedConnection) RemoteAddr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 2}
}
func (*scriptedConnection) SetDeadline(time.Time) error      { return nil }
func (*scriptedConnection) SetReadDeadline(time.Time) error  { return nil }
func (*scriptedConnection) SetWriteDeadline(time.Time) error { return nil }
func (c *scriptedConnection) written() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.output.String()
}

func TestAutomaticPinsDialAndRechecksSameAuthority(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "xn--bcher-kva.example" {
			t.Errorf("HTTP host identity changed: %s", r.Host)
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)
	var dialed []string
	base := &http.Transport{DialContext: func(ctx context.Context, network string, address string) (net.Conn, error) {
		dialed = append(dialed, address)
		if len(dialed) == 1 {
			return nil, errors.New("first public address unavailable")
		}
		return new(net.Dialer).DialContext(ctx, network, server.Listener.Addr().String())
	}}
	lookups := 0
	checked := transportFor(t, "http://bücher.example/plugin", Grant{}, base, func(_ context.Context, _ string, host string) ([]netip.Addr, error) {
		if host != "xn--bcher-kva.example" {
			t.Errorf("IDN lookup = %q", host)
		}
		lookups++
		if lookups > 1 {
			return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
		}
		return []netip.Addr{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("1.1.1.1")}, nil
	})
	request := requestFor(t, "http://bücher.example/artifacts/plugin?signature=secret")
	response, err := checked.RoundTrip(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if lookups != 1 || strings.Join(dialed, ",") != "8.8.8.8:80,1.1.1.1:80" {
		t.Fatalf("DNS/dial pinning failed: lookups=%d dialed=%v", lookups, dialed)
	}
	if request.URL.Host != "bücher.example" || response.Request != request {
		t.Fatal("HTTP identity or original response request changed")
	}
	next, err := response.Request.URL.Parse("next")
	if err != nil || next.Host != "bücher.example" {
		t.Fatalf("relative redirect base changed: %v %v", next, err)
	}
	if err := roundTripError(t, checked, requestFor(t, next.String())); !errors.Is(err, ErrPrivateDestination) || len(dialed) != 2 || lookups != 2 {
		t.Fatalf("same-authority DNS rebinding accepted: %v, dials=%v lookups=%d", err, dialed, lookups)
	}
}

func TestHTTPProxyPinsCONNECTAndTLSIdentity(t *testing.T) {
	t.Parallel()
	connection := &scriptedConnection{input: bytes.NewReader([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))}
	proxyURL, err := url.Parse("http://operator:credential@proxy.example:8080")
	if err != nil {
		t.Fatal(err)
	}
	base := &http.Transport{
		Proxy: func(request *http.Request) (*url.URL, error) {
			if request.URL.Host != "cdn.example" {
				t.Errorf("proxy chosen against numeric URL: %s", request.URL.Host)
			}
			return proxyURL, nil
		},
		TLSClientConfig: &tls.Config{ServerName: "caller-config"},
		DialContext: func(_ context.Context, _ string, address string) (net.Conn, error) {
			if address != "proxy.example:8080" {
				t.Errorf("proxy dial changed: %s", address)
			}
			return connection, nil
		},
	}
	checked := transportFor(t, "https://cdn.example/plugin", Grant{}, base, fixedLookup("8.8.8.8"))
	request := requestFor(t, "https://cdn.example/plugin")
	if err := roundTripError(t, checked, request); err == nil {
		t.Fatal("scripted peer unexpectedly completed TLS")
	}
	written := connection.written()
	if !strings.Contains(written, "CONNECT 8.8.8.8:443 HTTP/1.1\r\n") || !strings.Contains(written, "cdn.example") {
		t.Fatalf("CONNECT target or TLS SNI changed: %q", written)
	}
	if base.TLSClientConfig.ServerName != "caller-config" || proxyURL.User.String() != "operator:credential" || request.URL.Host != "cdn.example" {
		t.Fatal("caller TLS/proxy/request mutated")
	}
}

func TestProxyPolicyAndOriginalURLSelection(t *testing.T) {
	t.Parallel()
	proxyFunc := (&httpproxy.Config{HTTPSProxy: "http://proxy.example:8080", NoProxy: "cdn.example"}).ProxyFunc()
	base := &http.Transport{Proxy: func(request *http.Request) (*url.URL, error) { return proxyFunc(request.URL) }}
	request := requestFor(t, "https://cdn.example/plugin")
	checked := transportFor(t, request.URL.String(), Grant{}, base, fixedLookup("8.8.8.8"))
	selected, err := authorityOf(request.URL)
	if err != nil {
		t.Fatal(err)
	}
	prepared, pinned, err := checked.prepare(request, selected, base)
	if err != nil {
		t.Fatal(err)
	}
	defer pinned.CloseIdleConnections()
	proxy, err := pinned.Proxy(prepared)
	if err != nil || proxy != nil || prepared.URL.Host != "8.8.8.8:443" || prepared.Host != "cdn.example" || pinned.TLSClientConfig.ServerName != "cdn.example" {
		t.Fatalf("NO_PROXY or pinned identity lost: %v %v", proxy, err)
	}
	for _, test := range []struct{ target, proxy string }{
		{"http://cdn.example/plugin", "http://proxy.example"},
		{"https://cdn.example/plugin", "https://proxy.example"},
		{"https://cdn.example/plugin", "socks5://proxy.example"},
	} {
		proxyURL, err := url.Parse(test.proxy)
		if err != nil {
			t.Fatal(err)
		}
		checked := transportFor(t, test.target, Grant{}, &http.Transport{Proxy: http.ProxyURL(proxyURL)}, fixedLookup("8.8.8.8"))
		if err := roundTripError(t, checked, requestFor(t, test.target)); !errors.Is(err, ErrUnsupportedProxy) {
			t.Fatalf("unsupported proxy accepted: %v", err)
		}
	}
}

func TestFailuresAndCancellation(t *testing.T) {
	t.Parallel()
	checked := transportFor(t, "https://configured.example/plugin", Grant{}, &http.Transport{}, func(context.Context, string, string) ([]netip.Addr, error) {
		return nil, errors.New("lookup https://configured.example/?signed=secret")
	})
	err := roundTripError(t, checked, requestFor(t, "https://configured.example/plugin"))
	failure, found := Classify(&url.Error{Op: "Get", URL: "https://configured.example/?signed=secret", Err: err})
	if !found || failure != ErrDNSFailed || strings.Contains(err.Error(), "secret") {
		t.Fatalf("DNS failure classification leaked details: %v %v", failure, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	checked.lookup = func(ctx context.Context, _, _ string) ([]netip.Addr, error) {
		cancel()
		return nil, ctx.Err()
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://configured.example/plugin", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := roundTripError(t, checked, request); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
	if _, found := Classify(context.Canceled); found {
		t.Fatal("context cancellation classified as policy failure")
	}
}

func TestOwnedTransportRejectsCustomTLSAndProtocolHandlersBeforeDial(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"TLS dialer", "protocol handler"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			base := &http.Transport{DialContext: func(context.Context, string, string) (net.Conn, error) {
				t.Error("unsupported transport reached dialer")
				return nil, ErrConnectionFailed
			}}
			if kind == "TLS dialer" {
				base.DialTLSContext = func(context.Context, string, string) (net.Conn, error) {
					t.Error("custom TLS dialer executed")
					return nil, ErrConnectionFailed
				}
			} else {
				base.TLSNextProto = map[string]func(string, *tls.Conn) http.RoundTripper{
					"h2": func(string, *tls.Conn) http.RoundTripper {
						t.Error("custom protocol handler executed")
						return responseTransport(func(*http.Request) (*http.Response, error) { return nil, ErrConnectionFailed })
					},
				}
			}
			checked := transportFor(t, "https://cdn.example/plugin", Grant{}, base, func(context.Context, string, string) ([]netip.Addr, error) {
				t.Error("unsupported transport reached DNS")
				return nil, ErrDNSFailed
			})
			if err := roundTripError(t, checked, requestFor(t, "https://cdn.example/plugin")); !errors.Is(err, ErrUnsupportedTransport) {
				t.Fatalf("unsupported configuration accepted: %v", err)
			}
		})
	}
}

func TestOwnedTransportPreservesDetachedSupportedSettings(t *testing.T) {
	t.Parallel()
	proxyURL, err := url.Parse("http://operator:credential@proxy.example:8080")
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	sessions := tls.NewLRUClientSessionCache(4)
	callbacks := 0
	protocols := &http.Protocols{}
	protocols.SetHTTP1(true)
	protocols.SetHTTP2(true)
	base := &http.Transport{
		Proxy:                  func(*http.Request) (*url.URL, error) { callbacks++; return proxyURL, nil },
		OnProxyConnectResponse: func(context.Context, *url.URL, *http.Request, *http.Response) error { callbacks++; return nil },
		TLSClientConfig: &tls.Config{
			ServerName: "caller-config", RootCAs: roots, ClientSessionCache: sessions,
			Certificates:     []tls.Certificate{{Certificate: [][]byte{{1, 2, 3}}}},
			VerifyConnection: func(tls.ConnectionState) error { callbacks++; return nil },
		},
		TLSHandshakeTimeout:   3 * time.Second,
		ResponseHeaderTimeout: 4 * time.Second,
		ExpectContinueTimeout: 5 * time.Second,
		IdleConnTimeout:       6 * time.Second,
		DisableCompression:    true,
		MaxIdleConns:          7, MaxIdleConnsPerHost: 8, MaxConnsPerHost: 9,
		MaxResponseHeaderBytes: 1024, WriteBufferSize: 2048, ReadBufferSize: 4096,
		ProxyConnectHeader: http.Header{"X-Fixture": []string{"original"}},
		GetProxyConnectHeader: func(context.Context, *url.URL, string) (http.Header, error) {
			callbacks++
			return http.Header{"X-Dynamic": []string{"preserved"}}, nil
		},
		ForceAttemptHTTP2: true,
		HTTP2:             &http.HTTP2Config{MaxReadFrameSize: 65536},
		Protocols:         protocols,
	}
	owned, err := ownedTransport(base)
	if err != nil {
		t.Fatal(err)
	}
	if callbacks != 0 {
		t.Fatal("configuration snapshot executed caller callbacks")
	}
	if owned == base || owned.TLSClientConfig == base.TLSClientConfig || owned.HTTP2 == base.HTTP2 || owned.Protocols == base.Protocols {
		t.Fatal("owned transport retained mutable configuration containers")
	}
	if owned.TLSClientConfig.RootCAs != roots || owned.TLSClientConfig.ClientSessionCache != sessions || !bytes.Equal(owned.TLSClientConfig.Certificates[0].Certificate[0], []byte{1, 2, 3}) {
		t.Fatal("TLS trust, client certificate or session settings changed")
	}
	request := requestFor(t, "https://cdn.example/plugin")
	selectedProxy, err := owned.Proxy(request)
	if err != nil || selectedProxy != proxyURL {
		t.Fatalf("proxy policy changed: %v %v", selectedProxy, err)
	}
	if err := owned.OnProxyConnectResponse(t.Context(), proxyURL, request, &http.Response{}); err != nil {
		t.Fatal(err)
	}
	if err := owned.TLSClientConfig.VerifyConnection(tls.ConnectionState{}); err != nil {
		t.Fatal(err)
	}
	headers, err := owned.GetProxyConnectHeader(t.Context(), proxyURL, "8.8.8.8:443")
	if err != nil || headers.Get("X-Dynamic") != "preserved" || callbacks != 4 {
		t.Fatalf("supported proxy/TLS callbacks changed: %v %v callbacks=%d", headers, err, callbacks)
	}
	if owned.TLSHandshakeTimeout != 3*time.Second || owned.ResponseHeaderTimeout != 4*time.Second || owned.ExpectContinueTimeout != 5*time.Second || owned.IdleConnTimeout != 6*time.Second || !owned.DisableCompression {
		t.Fatal("timeouts or compression policy changed")
	}
	if owned.MaxIdleConns != 7 || owned.MaxIdleConnsPerHost != 8 || owned.MaxConnsPerHost != 9 || owned.MaxResponseHeaderBytes != 1024 || owned.WriteBufferSize != 2048 || owned.ReadBufferSize != 4096 {
		t.Fatal("connection, header or buffer limits changed")
	}
	owned.TLSClientConfig.ServerName = "owned-config"
	owned.ProxyConnectHeader.Set("X-Fixture", "owned")
	owned.HTTP2.MaxReadFrameSize = 32768
	owned.Protocols.SetHTTP2(false)
	if base.TLSClientConfig.ServerName != "caller-config" || base.ProxyConnectHeader.Get("X-Fixture") != "original" || base.HTTP2.MaxReadFrameSize != 65536 || !base.Protocols.HTTP2() {
		t.Fatal("owned settings mutated caller configuration")
	}
}

func TestOwnedTransportPreservesNativeHTTP2AndExplicitDisable(t *testing.T) {
	debug := os.Getenv("GODEBUG")
	if debug != "" {
		debug += ","
	}
	t.Setenv("GODEBUG", debug+"http2client=1")
	native := &http.Transport{ForceAttemptHTTP2: true}
	// Clone initializes Go's native HTTP/2 callbacks on the source transport.
	// Its snapshot omits those callbacks when the caller originally left the
	// map nil, so a previously used native transport remains supported.
	_ = native.Clone()
	if len(native.TLSNextProto) == 0 {
		t.Fatal("native HTTP/2 initialization did not install its callbacks")
	}
	owned, err := ownedTransport(native)
	if err != nil || owned.TLSNextProto != nil || !owned.ForceAttemptHTTP2 {
		t.Fatalf("native HTTP/2 configuration rejected or disabled: %v", err)
	}
	disabled := &http.Transport{ForceAttemptHTTP2: true, TLSNextProto: map[string]func(string, *tls.Conn) http.RoundTripper{}}
	owned, err = ownedTransport(disabled)
	if err != nil || owned.TLSNextProto == nil || len(owned.TLSNextProto) != 0 {
		t.Fatalf("explicit HTTP/2 disable was lost: %v", err)
	}
}

func TestOwnedTransportDoesNotInheritRegisteredProtocolExecutor(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "cdn.example" {
			t.Errorf("HTTP host identity changed: %s", r.Host)
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)
	base := &http.Transport{DialContext: func(ctx context.Context, network string, address string) (net.Conn, error) {
		if address != "8.8.8.8:80" {
			t.Errorf("unpinned dial: %s", address)
		}
		return new(net.Dialer).DialContext(ctx, network, server.Listener.Addr().String())
	}}
	base.RegisterProtocol("http", responseTransport(func(*http.Request) (*http.Response, error) {
		t.Error("registered protocol executor inherited")
		return nil, ErrConnectionFailed
	}))
	checked := transportFor(t, "http://cdn.example/plugin", Grant{}, base, fixedLookup("8.8.8.8"))
	response, err := checked.RoundTrip(requestFor(t, "http://cdn.example/plugin"))
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}
	if err != nil || response == nil || response.StatusCode != http.StatusOK {
		t.Fatalf("owned pinned executor failed: %v", err)
	}
}
