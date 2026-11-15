package acquisitionhttp

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestTransportUsesSuppliedConnections(t *testing.T) {
	t.Parallel()
	served := 0
	base := responseTransport(func(request *http.Request) (*http.Response, error) {
		served++
		return &http.Response{StatusCode: http.StatusTeapot, Body: io.NopCloser(strings.NewReader("")), Request: request}, nil
	})
	initial := "https://registry.example/index.json"
	checked, err := Transport(requestFor(t, initial).URL, grantFor(t, initial), Connections{Transport: base})
	if err != nil {
		t.Fatal(err)
	}
	response, err := checked.RoundTrip(requestFor(t, initial))
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusTeapot || served != 1 {
		t.Fatalf("supplied transport status=%d served=%d", response.StatusCode, served)
	}

	var queries atomic.Int32
	resolver := &net.Resolver{PreferGo: true, Dial: func(context.Context, string, string) (net.Conn, error) {
		queries.Add(1)
		return nil, errors.New("fixture resolver refuses queries")
	}}
	dialer := &http.Transport{DialContext: func(context.Context, string, string) (net.Conn, error) {
		t.Error("unresolved destination reached dialer")
		return nil, errors.New("unexpected dial")
	}}
	automatic := "https://registry.invalid/index.json"
	checked, err = Transport(requestFor(t, automatic).URL, Grant{}, Connections{Transport: dialer, Resolver: resolver})
	if err != nil {
		t.Fatal(err)
	}
	if err := roundTripError(t, checked, requestFor(t, automatic)); !errors.Is(err, ErrDNSFailed) || queries.Load() == 0 {
		t.Fatalf("supplied resolver error=%v queries=%d", err, queries.Load())
	}
}

func TestTransportDefaultsToHostConnections(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(server.Close)
	checked, err := Transport(requestFor(t, server.URL).URL, grantFor(t, server.URL), Connections{})
	if err != nil {
		t.Fatal(err)
	}
	response, err := checked.RoundTrip(requestFor(t, server.URL+"/index.json"))
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("host transport status = %d", response.StatusCode)
	}

	automatic := "https://localhost/index.json"
	checked, err = Transport(requestFor(t, automatic).URL, Grant{}, Connections{})
	if err != nil {
		t.Fatal(err)
	}
	if err := roundTripError(t, checked, requestFor(t, automatic)); !errors.Is(err, ErrPrivateDestination) {
		t.Fatalf("host resolver answer for localhost: %v", err)
	}
}
