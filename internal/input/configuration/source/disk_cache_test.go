package source

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	acquisitionhttp "actionlint.kjanat.dev/v2/internal/integrations/http"
	"actionlint.kjanat.dev/v2/internal/integrations/network"
)

func TestRemoteConfigCacheWriteFailure(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		warm bool
	}{{"online check", false}, {"cache warm", true}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			directory := filepath.Join(t.TempDir(), "not-a-directory")
			if err := os.WriteFile(directory, []byte("blocks cache directory creation"), 0o600); err != nil {
				t.Fatal(err)
			}
			ctx, session := network.WithSession(t.Context(), network.Options{CacheDirectory: directory, WarmCache: tc.warm})
			reader := New(ctx, nil, acquisitionhttp.Connections{})
			const content = "lint = {}\n"
			const configURL = "https://example.org/config.toml?token=private-config-token"
			reader.client.Transport = transportFunc(func(request *http.Request) (*http.Response, error) {
				if request.URL.String() != configURL {
					t.Errorf("request URL = %q, want %q", request.URL.String(), configURL)
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(content)), Request: request}, nil
			})
			got, err := reader.ReadFile(configURL)
			if tc.warm {
				if pathError, ok := errors.AsType[*os.PathError](err); !ok || pathError.Op != "mkdir" || pathError.Path != directory || got != nil {
					t.Fatalf("cache warm error=%v, want cache write failure", err)
				}
				return
			}
			if err != nil || string(got) != content {
				t.Fatalf("online read=%q error=%v, want downloaded configuration", got, err)
			}
			if warnings := session.Warnings(); len(warnings) != 1 || !strings.Contains(warnings[0], "Could not cache configuration https://example.org/config.toml:") || strings.Contains(warnings[0], "token") {
				t.Fatalf("cache warning=%v", warnings)
			}
		})
	}
}

func TestOfflineConfigCacheLimits(t *testing.T) {
	t.Parallel()
	ctx, _ := network.WithSession(t.Context(), network.Options{Offline: true, CacheDirectory: t.TempDir()})
	const url = "https://example.org/config.toml"
	reader := New(ctx, nil, acquisitionhttp.Connections{})
	if _, err := reader.ReadFile(url); !errors.Is(err, network.ErrOffline) {
		t.Fatalf("missing cache: %v", err)
	}
	if err := cacheConfig(ctx, url, []byte("lint = {}\n")); err != nil {
		t.Fatal(err)
	}
	data, err := reader.ReadFile(url)
	if err != nil || string(data) != "lint = {}\n" {
		t.Fatalf("cached config=%q error=%v", data, err)
	}
	file, err := os.OpenFile(configCachePath(ctx, url), os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	err = errors.Join(file.Truncate(MaxBytes+1), file.Close())
	if err != nil {
		t.Fatal(err)
	}
	_, err = reader.ReadFile(url)
	if err == nil || errors.Is(err, network.ErrOffline) || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized cache must remain an error: %v", err)
	}
}
