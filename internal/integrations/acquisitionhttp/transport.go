// Package acquisitionhttp confines automatic HTTP acquisition to public addresses.
package acquisitionhttp

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/netip"
	"net/url"

	"actionlint.kjanat.dev/v2/internal/base/endpoint"
	"actionlint.kjanat.dev/v2/internal/integrations/acquisitionhttp/accounting"
)

// Grant authorizes private addresses for one explicitly selected authority, and its zero value grants nothing.
type Grant struct{ endpoint endpoint.Authority }

// ExplicitEndpoint must not be called from repository declarations.
func ExplicitEndpoint(endpoint *url.URL) (Grant, error) {
	selected, err := authorityOf(endpoint)
	if err != nil {
		return Grant{}, err
	}
	return Grant{endpoint: selected}, nil
}

// Allows rejects a different endpoint reached through a redirect.
func (g Grant) Allows(endpoint *url.URL) bool {
	selected, err := authorityOf(endpoint)
	return err == nil && selected == g.endpoint
}

func authorityOf(u *url.URL) (endpoint.Authority, error) {
	selected, err := endpoint.Of(u)
	if err != nil {
		return endpoint.Authority{}, ErrInvalidEndpoint
	}
	return selected, nil
}

type resolver func(context.Context, string, string) ([]netip.Addr, error)

type transport struct {
	initial endpoint.Authority
	grant   Grant
	base    http.RoundTripper
	lookup  resolver
}

type Connections struct {
	Transport http.RoundTripper
	Resolver  *net.Resolver
}

// Transport requires public DNS answers and pinned connections for every request without an exact initial-endpoint grant.
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
	defer checked.CloseIdleConnections()
	response, err := checked.RoundTrip(prepared)
	if response != nil {
		response.Request = request
	}
	return response, err
}

func (t *transport) prepare(request *http.Request, selected endpoint.Authority, original *http.Transport) (*http.Request, *http.Transport, error) {
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
	addresses, err := t.publicAddresses(request.Context(), selected.Host)
	if err != nil {
		return nil, nil, err
	}
	checked.DisableKeepAlives = true
	checked.Proxy = func(*http.Request) (*url.URL, error) { return proxy, nil }
	if checked.TLSClientConfig == nil {
		checked.TLSClientConfig = &tls.Config{}
	}
	checked.TLSClientConfig.ServerName = selected.Host
	baseDial := checked.DialContext
	if baseDial == nil {
		dialer := &net.Dialer{}
		baseDial = dialer.DialContext
	}
	checked.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		if proxy != nil {
			return baseDial(ctx, network, address)
		}
		// Reuse this lookup's numeric answers. Do not resolve again at dial.
		for _, ip := range addresses {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			connection, err := baseDial(ctx, network, net.JoinHostPort(ip.String(), selected.Port))
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
	cloned.URL.Host = net.JoinHostPort(addresses[0].String(), selected.Port)
	if cloned.Host == "" {
		cloned.Host = request.URL.Host
	}
	return cloned, checked, nil
}

// net/http HTTP/2 setup treats a nil TLSNextProto differently from an explicitly empty map.
// ownedTransport never inherits deprecated dial hooks or registered protocol handlers.
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
