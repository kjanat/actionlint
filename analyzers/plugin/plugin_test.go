package plugin

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSettings(t *testing.T) {
	if _, err := newPlugin(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := newPlugin(map[string]any{"contract": "old-inventory.json"}); err == nil {
		t.Fatal("accepted repository policy")
	}
}

// This runs the actual custom binary against an unrelated module, then changes
// only its source under a warm cache. No architecture manifest is present.
func TestUnrelatedModule(t *testing.T) {
	binary := os.Getenv("ACTIONLINT_CUSTOM_GCL")
	if binary == "" {
		t.Skip("ACTIONLINT_CUSTOM_GCL is required for binary integration")
	}
	root := t.TempDir()
	write := func(name, text string) {
		t.Helper()
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.org/unrelated\n\ngo 1.27.0\n")
	write(".golangci.yml", "version: '2'\nlinters:\n  default: none\n  enable: [actionlintarch]\n  settings:\n    custom:\n      actionlintarch:\n        type: module\nissues:\n  max-issues-per-linter: 0\n  max-same-issues: 0\n")
	write("internal/storage/value.go", "package storage\ntype Value struct { Number int }\n")
	write("helper/helper.go", "package helper\nfunc View(b []byte) []byte { return b }\ntype Alias[T any] = []T\n")
	write("api.go", `package unrelated
import ("example.org/unrelated/helper"; "example.org/unrelated/internal/storage"; "sync/atomic")
type CompletelyDifferent struct { private []byte; Writable int }
func (s *CompletelyDifferent) Retrieve() []byte { return helper.View(s.private) }
func Leak() helper.Alias[storage.Value] { return nil }
type Counter struct { Value int64 }
func (c *Counter) Inc() { atomic.AddInt64(&c.Value, 1) }
`)
	run := func(wantFailure bool) []string {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, binary, "run", "--allow-parallel-runners", "--show-stats=false", "--output.json.path=stdout", "--output.text.path=", "./...")
		cmd.Dir = root
		output, err := cmd.CombinedOutput()
		if ctx.Err() != nil {
			t.Fatal(ctx.Err())
		}
		if wantFailure != (err != nil) {
			t.Fatalf("exit=%v, output=%s", err, output)
		}
		var report struct{ Issues []struct{ Text string } }
		if err := json.Unmarshal(output, &report); err != nil {
			t.Fatalf("invalid output: %s: %v", output, err)
		}
		var messages []string
		for _, issue := range report.Issues {
			messages = append(messages, issue.Text)
		}
		return messages
	}
	messages := strings.Join(run(true), "\n")
	for _, code := range []string{"ARC001", "ARC002", "ARC003", "ARC004"} {
		if !strings.Contains(messages, code) {
			t.Fatalf("missing %s: %s", code, messages)
		}
	}
	write("api.go", `package unrelated
import "bytes"
type RenamedAgain struct { private []byte; writable int }
func (s *RenamedAgain) Retrieve() []byte { return bytes.Clone(s.private) }
`)
	if messages := run(false); len(messages) != 0 {
		t.Fatalf("fixed source still failed: %v", messages)
	}
}
