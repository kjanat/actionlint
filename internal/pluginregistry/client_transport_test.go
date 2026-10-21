package pluginregistry

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type responseTransport func(*http.Request) (*http.Response, error)

func (f responseTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestCatalogueTransportLimits(t *testing.T) {
	for _, tc := range []struct {
		name, body, redirect string
		status               int
	}{
		{name: "size", body: strings.Repeat(" ", maxJSONBytes+1), status: 200},
		{name: "duplicate", body: `{"schema_version":1,"schema_version":1,"plugins":[]}`, status: 200},
		{name: "trailing", body: `{"schema_version":1,"plugins":[]} {}`, status: 200},
		{name: "status", status: 404},
		{name: "origin", redirect: "https://other.example/catalogue/index.json", status: 302},
		{name: "path", redirect: "https://example.org/outside/index.json", status: 302},
		{name: "escaped path", redirect: "https://example.org/catalogue/%2e%2e/outside/index.json", status: 302},
		{name: "downgrade", redirect: "http://example.org/catalogue/index.json", status: 302},
		{name: "loop", redirect: "https://example.org/catalogue/index.json", status: 302},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := New("https://example.org/catalogue/")
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			c.http.Transport = responseTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if calls > 1 && tc.name != "loop" {
					t.Fatal("followed rejected redirect")
				}
				return &http.Response{StatusCode: tc.status, Header: http.Header{"Location": []string{tc.redirect}}, Body: io.NopCloser(strings.NewReader(tc.body)), Request: r}, nil
			})
			if _, err := c.Search(t.Context(), ""); err == nil {
				t.Fatal("accepted invalid catalogue")
			}
		})
	}
	c, err := New(DefaultURL)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := c.Search(ctx, ""); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
}

func TestCatalogueRedirectHostCasing(t *testing.T) {
	for _, tc := range []struct {
		name, base, target string
		allowed            bool
	}{
		{name: "lowercase", target: "https://example.org:8443/catalogue/index.json", allowed: true},
		{name: "mixed case", target: "https://eXaMpLe.OrG:8443/catalogue/index.json", allowed: true},
		{name: "other port", target: "https://example.org:9443/catalogue/index.json"},
		{name: "omitted port", target: "https://example.org/catalogue/index.json"},
		{name: "credentials", target: "https://user@example.org:8443/catalogue/index.json"},
		{name: "other host", target: "https://other.example:8443/catalogue/index.json"},
		{name: "other path", target: "https://example.org:8443/outside/index.json"},
		{name: "path prefix", target: "https://example.org:8443/catalogue-other/index.json"},
		{name: "escaped path", target: "https://example.org:8443/catalogue/%2e%2e/outside/index.json"},
		{name: "address case", base: "https://[FE80::1%25Ethernet]:8443/catalogue/", target: "https://[fe80::1%25Ethernet]:8443/catalogue/index.json", allowed: true},
		{name: "address scope", base: "https://[fe80::1%25Ethernet]:8443/catalogue/", target: "https://[fe80::1%25ethernet]:8443/catalogue/index.json"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := tc.base
			if base == "" {
				base = "https://EXAMPLE.ORG:8443/catalogue/"
			}
			client, err := New(base)
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			client.http.Transport = responseTransport(func(request *http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": []string{tc.target}}, Body: io.NopCloser(strings.NewReader("")), Request: request}, nil
				}
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"schema_version":1,"plugins":[]}`)), Request: request}, nil
			})
			_, err = client.Search(t.Context(), "")
			if tc.allowed {
				if err != nil || calls != 2 {
					t.Fatalf("same-origin redirect: %d requests, %v", calls, err)
				}
			} else if err == nil || calls != 1 {
				t.Fatalf("redirect escaped catalogue: %d requests, %v", calls, err)
			}
		})
	}
}
