package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	cachemanagement "actionlint.kjanat.dev/v2/internal/integrations/cache"
	plugininstall "actionlint.kjanat.dev/v2/internal/plugins/installation"
)

func TestCacheManagementCommands(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	directory := filepath.Join(base, "actionlint")
	acquisition := plugininstall.Acquisition{UserCacheDir: base}
	code, output, log := runOfflineCommand(t, acquisition, "", "cache", "dir")
	if code != 0 || strings.TrimSpace(output) != directory || log != "" {
		t.Fatalf("cache dir: status=%d output=%q log=%s", code, output, log)
	}
	if _, err := os.Stat(directory); !os.IsNotExist(err) {
		t.Fatalf("cache dir created a directory: %v", err)
	}
	code, output, log = runOfflineCommand(t, acquisition, "", "cache", "list", "--json")
	var inventory cachemanagement.Inventory
	if err := json.Unmarshal([]byte(output), &inventory); err != nil || code != 0 || inventory.Exists || inventory.Files != 0 {
		t.Fatalf("empty list: status=%d output=%s log=%s error=%v", code, output, log, err)
	}
	file := filepath.Join(directory, "configs", "example")
	if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("cached config"), 0600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(file, old, old); err != nil {
		t.Fatal(err)
	}
	code, output, log = runOfflineCommand(t, acquisition, "", "cache", "prune", "--older-than", "24h", "--dry-run", "--json", "--offline")
	var preview cachemanagement.Cleanup
	if err := json.Unmarshal([]byte(output), &preview); err != nil || code != 0 || preview.Files != 1 || !preview.DryRun || preview.Entries[0].Path != "configs/example" {
		t.Fatalf("prune preview: status=%d output=%s log=%s error=%v", code, output, log, err)
	}
	if _, err := os.Stat(file); err != nil {
		t.Fatalf("preview removed file: %v", err)
	}
	code, output, log = runOfflineCommand(t, acquisition, "", "cache", "prune", "--older-than", "24h", "--json")
	var removed cachemanagement.Cleanup
	if err := json.Unmarshal([]byte(output), &removed); err != nil || code != 0 || removed.Files != 1 || removed.Bytes != 13 || removed.DryRun {
		t.Fatalf("prune: status=%d output=%s log=%s error=%v", code, output, log, err)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatalf("prune retained file: %v", err)
	}
	code, output, log = runOfflineCommand(t, acquisition, "", "cache", "clean")
	if code != 0 || !strings.Contains(output, "Removed 0 files") {
		t.Fatalf("clean: status=%d output=%s log=%s", code, output, log)
	}
	code, output, log = runOfflineCommand(t, acquisition, "", "cache", "prune", "--older-than=-1h")
	if code != 2 || !strings.Contains(log, "must be positive") {
		t.Fatalf("invalid age: status=%d output=%s log=%s", code, output, log)
	}
}
