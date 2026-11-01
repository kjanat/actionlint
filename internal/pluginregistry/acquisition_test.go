package pluginregistry

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/acquisitionhttp"
	"actionlint.kjanat.dev/v2/internal/testutil"
)

func TestRegistryAutomaticPrivateInitialNeverDials(t *testing.T) {
	previous := http.DefaultTransport
	dials := 0
	http.DefaultTransport = &http.Transport{DialContext: func(context.Context, string, string) (net.Conn, error) {
		dials++
		return nil, errors.New("unexpected private connection")
	}}
	t.Cleanup(func() { http.DefaultTransport = previous })
	client, err := NewWithGrant("https://127.0.0.1:8443/catalogue/", acquisitionhttp.Grant{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Search(t.Context(), ""); !errors.Is(err, acquisitionhttp.ErrPrivateDestination) || dials != 0 {
		t.Fatalf("automatic private registry: %v, dials=%d", err, dials)
	}
}

func TestRegistryExplicitNewPreservesSelectedPrivateAuthority(t *testing.T) {
	previous := http.DefaultTransport
	calls := 0
	http.DefaultTransport = responseTransport(func(request *http.Request) (*http.Response, error) {
		calls++
		if request.URL.String() != "https://127.0.0.1:8443/catalogue/index.json" {
			t.Fatalf("registry request escaped selected authority: %s", request.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"schema_version":1,"plugins":[]}`)), Request: request}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = previous })
	client, err := New("https://127.0.0.1:8443/catalogue/")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Search(t.Context(), ""); err != nil || calls != 1 {
		t.Fatalf("explicit registry lost caller transport: %v calls=%d", err, calls)
	}
}

func TestRegistryAutomaticPublicFixtureRetainsOriginalAuthority(t *testing.T) {
	registry := testutil.NewPluginRegistry(t)
	registry.SetFile(testutil.FixtureRegistryURL+"index.json", []byte(`{"schema_version":1,"plugins":[]}`))
	client, err := NewWithGrant(testutil.FixtureRegistryURL, acquisitionhttp.Grant{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Search(t.Context(), ""); err != nil {
		t.Fatal(err)
	}
	requests := registry.Requests()
	if len(requests) != 1 || requests[0] != testutil.FixtureRegistryURL+"index.json" {
		t.Fatalf("pinned fixture lost original URL: %v", requests)
	}
}
