package localapp

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/application"
)

// TestLocalManifestToolHelper is the tool a local manifest runs: the test
// binary, reporting what it read on stdin.
func TestLocalManifestToolHelper(t *testing.T) {
	if os.Getenv("ACTIONLINT_LOCAL_MANIFEST_HELPER") != "1" {
		return
	}
	stdin, err := io.ReadAll(os.Stdin)
	if err != nil {
		os.Exit(2)
	}
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"items": []map[string]any{{"line": 1, "col": 6, "text": "saw " + strings.TrimSpace(string(stdin))}}})
	os.Exit(0)
}

// A user runs any binary through a manifest and a jq transform beside it.
func TestLocalManifestRunsBinaryWithJQ(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	const program = `[.items[] | {filename: $vars.filename, code: "LOCAL1", message: .text, location: {row: .line, column: .col}, end_location: {row: .line, column: (.col + 4)}}]`
	sum := sha256.Sum256([]byte(program))
	manifest, err := json.Marshal(map[string]any{
		"schema_version": 1, "id": "example.com/local-tool", "version": "1.0.0", "role": "script", "scope": "source", "transport": "process",
		"rule":         map[string]any{"name": "local-tool", "description": "A local tool", "category": "correctness", "subcategory": "shell", "maturity": "stable", "provider": "local-tool"},
		"preparation":  "kjanat:actionlint/expression-mask@0.1.0",
		"selectors":    map[string]any{"shells": []string{"bash"}, "filename": "actionlint.sh"},
		"capabilities": map[string]any{"fixes": false, "reported_sources": false, "message_template": "local: {code}: {message}"},
		"process": map[string]any{
			"entry": executable, "args": []string{"-test.run=^TestLocalManifestToolHelper$", "--", "{var:filename}"}, "stdin": "source",
			"env": map[string]string{"ACTIONLINT_LOCAL_MANIFEST_HELPER": "1"}, "env_passthrough": []string{"TMPDIR", "TEMP", "TMP"},
			"config": map[string]any{"render": "none"},
			"output": map[string]any{"format": "json", "transform": map[string]any{"language": "jq", "digest": "sha256:" + hex.EncodeToString(sum[:])}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	path := filepath.Join(directory, "manifest.json")
	for name, content := range map[string]string{"manifest.json": string(manifest), "output.jq": program} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	input := "on: push\njobs:\n  check:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ${{ github.ref }}\n        shell: bash\n      - run: print(1)\n        shell: python\n"
	result, err := Check(t.Context(), application.Options{}, application.Input{Kind: application.Stdin, Reader: strings.NewReader(input)}, []string{path})
	if err != nil {
		t.Fatal(err)
	}
	findings := slices.DeleteFunc(slices.Clone(result.Diagnostics), func(d diagnostic.Diagnostic) bool { return d.Rule != "local-tool" })
	if len(findings) != 1 {
		t.Fatalf("findings: %+v", result.Diagnostics)
	}
	finding := findings[0]
	want := "local: LOCAL1: saw echo " + strings.Repeat("_", len("${{ github.ref }}"))
	if finding.Rule != "local-tool" || finding.Message != want || finding.Start.Line != 6 || finding.Start.Column != 19 || finding.End.Column != 23 {
		t.Fatalf("finding: %+v, want message %q", finding, want)
	}
}
