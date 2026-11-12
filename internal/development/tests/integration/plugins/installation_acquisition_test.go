package plugins_test

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"

	fixtureregistry "actionlint.kjanat.dev/v2/internal/development/tests/support/registry"
	acquisitionhttp "actionlint.kjanat.dev/v2/internal/integrations/http"
	plugininstall "actionlint.kjanat.dev/v2/internal/plugins/installation"
)

type registryTestTransport func(*http.Request) (*http.Response, error)

func (transport registryTestTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func TestRegistryAutomaticPrivateInitialNeverDials(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	previous := http.DefaultTransport
	dials := 0
	http.DefaultTransport = &http.Transport{DialContext: func(context.Context, string, string) (net.Conn, error) {
		dials++
		return nil, errors.New("unexpected private connection")
	}}
	t.Cleanup(func() { http.DefaultTransport = previous })
	client, err := plugininstall.NewWithGrant("https://127.0.0.1:8443/catalogue/", acquisitionhttp.Grant{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Search(t.Context(), ""); !errors.Is(err, acquisitionhttp.ErrPrivateDestination) || dials != 0 {
		t.Fatalf("automatic private registry: %v, dials=%d", err, dials)
	}
}

func TestRegistryExplicitNewPreservesSelectedPrivateAuthority(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	previous := http.DefaultTransport
	calls := 0
	http.DefaultTransport = registryTestTransport(func(request *http.Request) (*http.Response, error) {
		calls++
		if request.URL.String() != "https://127.0.0.1:8443/catalogue/index.json" {
			t.Fatalf("registry request escaped selected authority: %s", request.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"schema_version":1,"plugins":[]}`)), Request: request}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = previous })
	client, err := plugininstall.New("https://127.0.0.1:8443/catalogue/")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Search(t.Context(), ""); err != nil || calls != 1 {
		t.Fatalf("explicit registry lost caller transport: %v calls=%d", err, calls)
	}
}

func TestRegistryAutomaticPublicFixtureRetainsOriginalAuthority(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	registry := fixtureregistry.NewPluginRegistry(t)
	registry.SetFile(fixtureregistry.FixtureRegistryURL+"index.json", []byte(`{"schema_version":1,"plugins":[]}`))
	client, err := plugininstall.NewWithGrant(fixtureregistry.FixtureRegistryURL, acquisitionhttp.Grant{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Search(t.Context(), ""); err != nil {
		t.Fatal(err)
	}
	requests := registry.Requests()
	if len(requests) != 1 || requests[0] != fixtureregistry.FixtureRegistryURL+"index.json" {
		t.Fatalf("pinned fixture lost original URL: %v", requests)
	}
}
