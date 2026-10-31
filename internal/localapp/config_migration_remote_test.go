package localapp

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type migrationConfigTransport func(*http.Request) (*http.Response, error)

func (f migrationConfigTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestConfigMigrationRejectsMixedRemoteSnapshots(t *testing.T) {
	root := pluginRepository(t)
	path := filepath.Join(root, "actionlint.yaml")
	original := "extends: [https://example.test/base.yaml]\nconfig-variables: [LOCAL]\n"
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	reads := 0
	transport := http.DefaultTransport
	http.DefaultTransport = migrationConfigTransport(func(request *http.Request) (*http.Response, error) {
		if request.URL.String() != "https://example.test/base.yaml" {
			t.Fatalf("unexpected acquisition: %s", request.URL)
		}
		reads++
		body := "plugins: []"
		if reads == 2 {
			body = "plugins: [github.com/example/plugin@v1.0.0]"
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = transport })
	if _, err := PrepareConfigMigration(t.Context(), path, DefaultPluginOptions{}); err == nil || !strings.Contains(err.Error(), "changed during migration preparation") {
		t.Fatalf("mixed remote snapshots accepted: %v", err)
	}
	if reads != 2 {
		t.Fatalf("snapshot comparison fetched remote again: %d reads", reads)
	}
	content, err := os.ReadFile(path)
	if err != nil || string(content) != original {
		t.Fatalf("failed preview changed config: %q, %v", content, err)
	}
}
