//go:build unix

package pluginregistry

import (
	"context"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"actionlint.kjanat.dev/v2/internal/plugins/artifact"
	"golang.org/x/sys/unix"
)

// A candidate FIFO must fail before reading. A subprocess bounds the regression
// even when an open syscall cannot observe context cancellation.
func TestCandidateRejectsFIFO(t *testing.T) {
	if directory := os.Getenv("ACTIONLINT_TEST_CANDIDATE_FIFO"); directory != "" {
		registry := (&url.URL{Scheme: "file", Path: directory + "/"}).String()
		var err error
		if os.Getenv("ACTIONLINT_TEST_CANDIDATE_KIND") == "metadata" {
			var client *Client
			client, err = New(registry)
			if err == nil {
				_, err = client.Search(t.Context(), "")
			}
		} else {
			pin := Pin{Origin: "candidate", Registry: registry, URL: "plugin.wasm", SHA256: strings.Repeat("a", 64)}
			err = pin.importCandidate(t.Context(), artifact.Options{CacheDir: t.TempDir()})
		}
		if err == nil || !strings.Contains(err.Error(), "regular file") {
			t.Fatalf("FIFO did not produce a regular-file error: %v", err)
		}
		return
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"metadata", "artifact"} {
		t.Run(kind, func(t *testing.T) {
			directory := t.TempDir()
			name := "plugin.wasm"
			if kind == "metadata" {
				name = "index.json"
			}
			if err := unix.Mkfifo(filepath.Join(directory, name), 0o600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, executable, "-test.run=^TestCandidateRejectsFIFO$")
			cmd.Env = append(os.Environ(), "ACTIONLINT_TEST_CANDIDATE_FIFO="+directory, "ACTIONLINT_TEST_CANDIDATE_KIND="+kind)
			output, err := cmd.CombinedOutput()
			if ctx.Err() != nil {
				t.Fatalf("candidate %s blocked opening FIFO: %v", kind, ctx.Err())
			}
			if err != nil {
				t.Fatalf("candidate %s: %v\n%s", kind, err, output)
			}
		})
	}
}
