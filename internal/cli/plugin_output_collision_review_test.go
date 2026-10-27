package cli

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckRefusesFileURIPluginAsOutput(t *testing.T) {
	fixture, err := filepath.Abs(filepath.Join("internal", "plugins", "core", "testdata", "engines", "echo.wasm"))
	if err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	plugin := filepath.Join(root, "plugin with space.wasm")
	uriPath := filepath.ToSlash(plugin)
	if !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath
	}
	base := (&url.URL{Scheme: "file", Path: uriPath}).String()
	workflow := filepath.Join(root, "ci.yml")
	if err := os.WriteFile(workflow, []byte("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps: [{run: echo}]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{base, base + fmt.Sprintf("#sha256=%x", sha256.Sum256(original))} {
		if err := os.WriteFile(plugin, original, 0o600); err != nil {
			t.Fatal(err)
		}
		var out, logs bytes.Buffer
		command := Command{Stdout: &out, Stderr: &logs}
		status := command.MainContext(t.Context(), []string{"actionlint", "check", "--no-config", "--summary=false", "--plugin", source, "--output-file", plugin, "--yes", workflow})
		actual, err := os.ReadFile(plugin)
		if err != nil {
			t.Fatal(err)
		}
		if status != 3 || !strings.Contains(logs.String(), "also an input") || !bytes.Equal(actual, original) {
			t.Fatalf("plugin overwritten through file URI: exit=%d preserved=%v stdout=%s stderr=%s", status, bytes.Equal(actual, original), &out, &logs)
		}
	}
}
