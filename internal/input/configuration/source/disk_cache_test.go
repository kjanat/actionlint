package source

import (
	"errors"
	"os"
	"strings"
	"testing"

	acquisitionhttp "actionlint.kjanat.dev/v2/internal/integrations/http"
	"actionlint.kjanat.dev/v2/internal/integrations/network"
)

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
