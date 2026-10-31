package config

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type remoteConfigTransport func(*http.Request) (*http.Response, error)

func (f remoteConfigTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestMigrationRemoteDependencySnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "actionlint.yaml")
	remote := "https://example.test/base.yaml"
	original := "extends: [" + remote + "]\nconfig-variables: [LOCAL]\n"
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	body := "policy: {require-commit-hash: true}"
	reads := 0
	transport := http.DefaultTransport
	http.DefaultTransport = remoteConfigTransport(func(request *http.Request) (*http.Response, error) {
		reads++
		if request.URL.String() != remote {
			t.Fatalf("unexpected request %s", request.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}, Request: request}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = transport })
	plan, err := PrepareMigration(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	if reads != 1 {
		t.Fatalf("preparation fetched dependency %d times", reads)
	}
	if err := plan.CheckSources(map[string][]byte{path: []byte(original), remote: []byte(body)}); err != nil {
		t.Fatalf("matching captured graph: %v", err)
	}
	if err := plan.CheckSources(map[string][]byte{path: []byte(original), remote: []byte("plugins: [different/plugin]")}); err == nil || !strings.Contains(err.Error(), remote) {
		t.Fatalf("mismatched remote snapshot = %v", err)
	}
	if err := plan.CheckSources(map[string][]byte{path: []byte(original)}); err == nil {
		t.Fatal("accepted an incomplete captured graph")
	}
	if reads != 1 {
		t.Fatal("comparing captured graphs performed network IO")
	}
	if err := plan.CheckUnchanged(t.Context()); err != nil {
		t.Fatal(err)
	}
	body = "policy: {require-commit-hash: false}"
	if err := plan.Apply(t.Context()); err == nil || !strings.Contains(err.Error(), "changed before migration") || !strings.Contains(err.Error(), remote) {
		t.Fatalf("changed remote error = %v", err)
	}
	content, err := os.ReadFile(path)
	if err != nil || string(content) != original {
		t.Fatalf("leaf after rejection = %q, %v", content, err)
	}
}

func TestMigrationRemoteLeafReadOnly(t *testing.T) {
	_, err := PrepareMigration(t.Context(), "https://example.test/actionlint.yaml")
	if err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("remote leaf error = %v", err)
	}
}
