package testutil

import (
	"errors"
	"net"
	"net/http"
)

type localHTTPTransport struct{ base http.RoundTripper }

func (t localHTTPTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	host := request.URL.Hostname()
	if host == "localhost" || net.ParseIP(host).IsLoopback() {
		return t.base.RoundTrip(request)
	}
	return nil, errors.New("external HTTP is disabled in unit tests; provide a fixture transport")
}

// BlockExternalHTTP allows local test servers and rejects external requests.
func BlockExternalHTTP() { http.DefaultTransport = localHTTPTransport{base: http.DefaultTransport} }
