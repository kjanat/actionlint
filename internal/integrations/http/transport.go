// Package acquisitionhttp confines automatic HTTP acquisition to public addresses.
// Invocation-owned grants permit one explicitly selected initial authority.
package acquisitionhttp

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"

	"actionlint.kjanat.dev/v2/internal/integrations/http/accounting"

	"golang.org/x/net/idna"
)

type authority struct{ scheme, host, port string }

// Grant authorizes private addresses for one explicitly selected authority.
// Its zero value grants nothing. Grants contain no URLs, credentials or queries.
type Grant struct{ endpoint authority }

// ExplicitEndpoint records invocation-owned endpoint selection. Repository
// declarations must not call this function to grant themselves network access.
func ExplicitEndpoint(endpoint *url.URL) (Grant, error) {
	selected, err := authorityOf(endpoint)
	if err != nil {
		return Grant{}, err
	}
	return Grant{endpoint: selected}, nil
}

// Allows reports whether endpoint has exactly the granted scheme, host and port.
// It does not authorize a different endpoint reached through a redirect.
func (g Grant) Allows(endpoint *url.URL) bool {
	selected, err := authorityOf(endpoint)
	return err == nil && selected == g.endpoint
}

func authorityOf(endpoint *url.URL) (authority, error) {
	if endpoint == nil || endpoint.User != nil || endpoint.Opaque != "" || (endpoint.Scheme != "http" && endpoint.Scheme != "https") || endpoint.Hostname() == "" {
		return authority{}, ErrInvalidEndpoint
	}
	// Callers may construct url.URL directly; reject malformed authority
	// syntax before Port can mistake an invalid explicit port for no port.
	if _, err := url.Parse(endpoint.String()); err != nil {
		return authority{}, ErrInvalidEndpoint
	}
	host := endpoint.Hostname()
	if ip, err := netip.ParseAddr(host); err == nil {
		host = ip.String()
	} else {
		ascii, err := idna.Lookup.ToASCII(host)
		if err != nil {
			return authority{}, ErrInvalidEndpoint
		}
		// Preserve a trailing dot: an absolute DNS name must not grant the
		// corresponding search-relative name access to private addresses.
		host = strings.ToLower(ascii)
	}
	port := endpoint.Port()
	if port == "" {
		port = "80"
		if endpoint.Scheme == "https" {
			port = "443"
		}
	}
	number, err := strconv.ParseUint(port, 10, 16)
	if err != nil {
		return authority{}, ErrInvalidEndpoint
	}
	return authority{scheme: endpoint.Scheme, host: host, port: strconv.FormatUint(number, 10)}, nil
}

type resolver func(context.Context, string, string) ([]netip.Addr, error)

type transport struct {
	initial authority
	grant   Grant
	base    http.RoundTripper
	lookup  resolver
}

type Connections struct {
	Transport http.RoundTripper
	Resolver  *net.Resolver
}

// Transport validates the initial endpoint and captures the base transport and
// resolver from connections, or the host's current defaults for nil fields.
// Without an exact initial-endpoint grant, every request,
// including same-authority redirects, requires public DNS answers and pinned
// connections. Different redirect authorities always require public addresses.
func Transport(initial *url.URL, grant Grant, connections Connections) (http.RoundTripper, error) {
	base, dns := connections.Transport, connections.Resolver
	if base == nil {
		base = http.DefaultTransport
	}
	if dns == nil {
		dns = net.DefaultResolver
	}
	return newTransport(initial, grant, base, dns.LookupNetIP)
}

func newTransport(initial *url.URL, grant Grant, base http.RoundTripper, lookup resolver) (*transport, error) {
	selected, err := authorityOf(initial)
	if err != nil {
		return nil, err
	}
	return &transport{initial: selected, grant: grant, base: base, lookup: lookup}, nil
}

func (t *transport) RoundTrip(request *http.Request) (*http.Response, error) {
	return accounting.TrackHTTPAttempt(request, t.roundTrip)
}

func (t *transport) roundTrip(request *http.Request) (*http.Response, error) {
	if request == nil {
		return nil, ErrInvalidEndpoint
	}
	selected, err := authorityOf(request.URL)
	if err != nil {
		return nil, err
	}
	if err := request.Context().Err(); err != nil {
		return nil, err
	}
	if t.base == nil {
		return nil, ErrUnsupportedTransport
	}
	if selected == t.initial && t.grant.Allows(request.URL) {
		return t.base.RoundTrip(request)
	}
	var original *http.Transport
	switch base := t.base.(type) {
	case *http.Transport:
		original = base
	default:
		return nil, ErrUnsupportedTransport
	}
	prepared, checked, err := t.prepare(request, selected, original)
	if err != nil {
		return nil, err
	}
	// A checked request owns its transport; caller connection pools stay open.
	defer checked.CloseIdleConnections()
	response, err := checked.RoundTrip(prepared)
	if response != nil {
		response.Request = request
	}
	return response, err
}

func (t *transport) prepare(request *http.Request, selected authority, original *http.Transport) (*http.Request, *http.Transport, error) {
	if err := request.Context().Err(); err != nil {
		return nil, nil, err
	}
	checked, err := ownedTransport(original)
	if err != nil {
		return nil, nil, err
	}
	var proxy *url.URL
	if checked.Proxy != nil {
		chosen, err := checked.Proxy(request)
		if err != nil {
			return nil, nil, ErrUnsupportedProxy
		}
		if chosen != nil {
			proxyValue := *chosen
			proxy = &proxyValue
		}
	}
	// HTTP forward proxies use Request.Host as their absolute-form target;
	// HTTPS proxies share TLS configuration between proxy and target. Neither
	// supports this numeric-target pinning while preserving TLS identity.
	if proxy != nil && (proxy.Scheme != "http" || request.URL.Scheme != "https") {
		return nil, nil, ErrUnsupportedProxy
	}
	addresses, err := t.publicAddresses(request.Context(), selected.host)
	if err != nil {
		return nil, nil, err
	}
	checked.DisableKeepAlives = true
	checked.Proxy = func(*http.Request) (*url.URL, error) { return proxy, nil }
	if checked.TLSClientConfig == nil {
		checked.TLSClientConfig = &tls.Config{}
	}
	checked.TLSClientConfig.ServerName = selected.host
	baseDial := checked.DialContext
	if baseDial == nil {
		dialer := &net.Dialer{}
		baseDial = dialer.DialContext
	}
	checked.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		if proxy != nil {
			return baseDial(ctx, network, address)
		}
		// Reuse this lookup's numeric answers; never resolve again at dial.
		for _, ip := range addresses {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			connection, err := baseDial(ctx, network, net.JoinHostPort(ip.String(), selected.port))
			if err == nil {
				return connection, nil
			}
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return nil, ErrConnectionFailed
	}
	cloned := request.Clone(request.Context())
	cloned.URL.Host = net.JoinHostPort(addresses[0].String(), selected.port)
	if cloned.Host == "" {
		cloned.Host = request.URL.Host
	}
	return cloned, checked, nil
}

// ownedTransport imports only supported settings into a fresh request executor.
// Clone is a configuration snapshot, never the executor: its native HTTP/2
// bookkeeping distinguishes caller-supplied TLSNextProto from automatically
// installed handlers, while preserving nil versus an explicitly empty map.
// Deprecated dial hooks and registered protocol handlers are never inherited.
func ownedTransport(original *http.Transport) (*http.Transport, error) {
	if original == nil || original.DialTLSContext != nil {
		return nil, ErrUnsupportedTransport
	}
	configuration := original.Clone()
	if len(configuration.TLSNextProto) != 0 {
		return nil, ErrUnsupportedTransport
	}
	return &http.Transport{
		Proxy:                  configuration.Proxy,
		OnProxyConnectResponse: configuration.OnProxyConnectResponse,
		DialContext:            configuration.DialContext,
		TLSClientConfig:        configuration.TLSClientConfig,
		TLSHandshakeTimeout:    configuration.TLSHandshakeTimeout,
		DisableKeepAlives:      configuration.DisableKeepAlives,
		DisableCompression:     configuration.DisableCompression,
		MaxIdleConns:           configuration.MaxIdleConns,
		MaxIdleConnsPerHost:    configuration.MaxIdleConnsPerHost,
		MaxConnsPerHost:        configuration.MaxConnsPerHost,
		IdleConnTimeout:        configuration.IdleConnTimeout,
		ResponseHeaderTimeout:  configuration.ResponseHeaderTimeout,
		ExpectContinueTimeout:  configuration.ExpectContinueTimeout,
		TLSNextProto:           configuration.TLSNextProto,
		ProxyConnectHeader:     configuration.ProxyConnectHeader,
		GetProxyConnectHeader:  configuration.GetProxyConnectHeader,
		MaxResponseHeaderBytes: configuration.MaxResponseHeaderBytes,
		WriteBufferSize:        configuration.WriteBufferSize,
		ReadBufferSize:         configuration.ReadBufferSize,
		ForceAttemptHTTP2:      configuration.ForceAttemptHTTP2,
		HTTP2:                  configuration.HTTP2,
		Protocols:              configuration.Protocols,
	}, nil
}
