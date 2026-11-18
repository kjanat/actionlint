package plugins_test

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/fixtureregistry"
	"actionlint.kjanat.dev/v2/internal/integrations/acquisitionhttp"
	"actionlint.kjanat.dev/v2/internal/integrations/dependencycache"
	"actionlint.kjanat.dev/v2/internal/model/pluginreference"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginregistry"
)

type registryTestTransport func(*http.Request) (*http.Response, error)

func (transport registryTestTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func TestRegistryAutomaticPrivateInitialNeverDials(t *testing.T) {
	t.Parallel()
	dials := 0
	transport := &http.Transport{DialContext: func(context.Context, string, string) (net.Conn, error) {
		dials++
		return nil, errors.New("unexpected private connection")
	}}
	t.Cleanup(transport.CloseIdleConnections)
	acquisition := dependencycache.Acquisition{UserCacheDir: t.TempDir(), Connections: acquisitionhttp.Connections{Transport: transport}}
	declaration, err := pluginreference.New(fixtureregistry.FixturePluginName, "latest", "https://127.0.0.1:8443/catalogue/")
	if err != nil {
		t.Fatal(err)
	}
	client, err := pluginregistry.NewWithGrant(declaration, pluginregistry.StatusOptions{Acquisition: acquisition})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Search(t.Context(), ""); !errors.Is(err, acquisitionhttp.ErrPrivateDestination) || dials != 0 {
		t.Fatalf("automatic private registry: %v, dials=%d", err, dials)
	}
}

func TestRegistryExplicitNewPreservesSelectedPrivateAuthority(t *testing.T) {
	t.Parallel()
	calls := 0
	transport := registryTestTransport(func(request *http.Request) (*http.Response, error) {
		calls++
		if request.URL.String() != "https://127.0.0.1:8443/catalogue/index.json" {
			t.Fatalf("registry request escaped selected authority: %s", request.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"schema_version":1,"plugins":[]}`)), Request: request}, nil
	})
	acquisition := dependencycache.Acquisition{UserCacheDir: t.TempDir(), Connections: acquisitionhttp.Connections{Transport: transport}}
	client, err := pluginregistry.New("https://127.0.0.1:8443/catalogue/", acquisition)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Search(t.Context(), ""); err != nil || calls != 1 {
		t.Fatalf("explicit registry lost caller transport: %v calls=%d", err, calls)
	}
}

func TestRegistryAutomaticPublicFixtureRetainsOriginalAuthority(t *testing.T) {
	t.Parallel()
	registry := fixtureregistry.NewPluginRegistry(t)
	registry.SetFile(fixtureregistry.FixtureRegistryURL+"index.json", []byte(`{"schema_version":1,"plugins":[]}`))
	declaration, err := pluginreference.New(fixtureregistry.FixturePluginName, "latest", fixtureregistry.FixtureRegistryURL)
	if err != nil {
		t.Fatal(err)
	}
	client, err := pluginregistry.NewWithGrant(declaration, pluginregistry.StatusOptions{Acquisition: registryAcquisition(registry)})
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

func registryAcquisition(registry *fixtureregistry.PluginRegistry) dependencycache.Acquisition {
	return dependencycache.Acquisition{UserCacheDir: registry.UserCacheDir(), Connections: registry.Connections()}
}
