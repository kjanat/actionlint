package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/integrations/buildinfo"

	"actionlint.kjanat.dev/v2/internal/cli"
	"github.com/fatih/color"
	"github.com/google/go-cmp/cmp"
)

func TestCommandCompatibility(t *testing.T) {
	oldColor := color.NoColor
	t.Cleanup(func() { color.NoColor = oldColor })
	color.NoColor = true
	repoDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	t.Chdir(root)
	gitRepository(t, root)
	if err := os.MkdirAll(filepath.Join(root, ".github", "workflows"), 0o700); err != nil {
		t.Fatal(err)
	}
	good := `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo ok
permissions: {}
`
	bad := strings.Replace(good, "echo ok", `echo "${{ missing.value }}"`, 1)
	for name, src := range map[string]string{"bad.yml": bad, "good.yml": good, ".github/workflows/ci.yml": good} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	tests := []struct {
		name string
		args []string
		in   string
	}{
		{"stdin-good", []string{"-"}, good},
		{"stdin-empty", []string{"--output-format=text", "-"}, ""},
		{"stdin-pretty", []string{"--output-format=text", "-"}, bad},
		{"stdin-oneline", []string{"-oneline", "-"}, bad},
		{"stdin-long-flags", []string{"--oneline", "--stdin-filename=from-stdin.yml", "-"}, bad},
		{"stdin-label", []string{"--output-format=text", "-stdin-filename", "source with spaces.yml", "-"}, bad},
		{"json-template", []string{"-format", "{{json .}}", "-"}, bad},
		{"json-template-empty", []string{"-format", "{{json .}}", "-"}, good},
		{"jsonl-template", []string{"-format", "{{range .}}{{json .}}{{end}}", "-"}, bad},
		{"custom-template", []string{"-format", `{{range .}}{{.Kind}}:{{.Line}}:{{.Column}}\n{{end}}`, "-"}, bad},
		{"template-over-oneline", []string{"-oneline", "-format", "{{json .}}", "-"}, bad},
		{"bad-template", []string{"-format", "plain", "-"}, good},
		{"bad-regexp", []string{"--ignore-regex", "[", "-"}, good},
		{"ignored-diagnostic", []string{"--ignore-regex", "undefined variable", "-"}, bad},
		{"repeat-ignore", []string{"--ignore-regex", "undefined variable", "--ignore-regex", "other", "-"}, bad},
		{"ignore-with-comma", []string{"--output-format=text", "--ignore-regex", "undefined variable,other", "-"}, bad},
		{"boolean-false", []string{"--output-format=text", "-oneline=false", "-"}, bad},
		{"boolean-repeat", []string{"--output-format=text", "-oneline", "-oneline=false", "-"}, bad},
		{"boolean-numeric", []string{"-oneline=1", "-"}, bad},
		{"color", []string{"--output-format=text", "-color", "-no-color=false", "-"}, bad},
		{"no-color-wins", []string{"--output-format=text", "-no-color", "-color", "-"}, bad},
		{"files", []string{"--output-format=text", "bad.yml", "good.yml"}, ""},
		{"files-json-template", []string{"-format", "{{json .}}", "bad.yml", "good.yml"}, ""},
		{"repository", nil, ""},
		{"end-of-flags", []string{"--", "good.yml"}, ""},
		{"version", []string{"-version"}, ""},
		{"long-version", []string{"--version"}, ""},
		{"version-precedence", []string{"-version", "-completion", "bash", "-init-config", "-format", "bad", "--ignore-regex", "["}, ""},
		{"version-positionals", []string{"-version", "does-not-exist"}, ""},
	}
	goldenPath := filepath.Join(repoDir, "internal", "development", "tests", "integration", "cli", "testdata", "command", "compatibility.json")
	// Captured from the CLI before the Cobra rewrite. Do not regenerate these
	// transcripts with the implementation under test.
	want := map[string]commandTranscript{}
	data, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &want); err != nil {
		t.Fatal(err)
	}
	if len(want) != len(tests) {
		t.Fatalf("golden has %d cases, suite has %d", len(want), len(tests))
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer
			cmd := cli.Command{Stdin: strings.NewReader(tc.in), Stdout: &stdout, Stderr: &stderr}
			args := append([]string{"actionlint", "--summary=false", "-no-tool=shellcheck", "-no-tool=ruff", "-no-color"}, tc.args...)
			status := cmd.Main(args)
			normalize := strings.NewReplacer(runtime.Version(), "GO_VERSION", runtime.GOOS+"/"+runtime.GOARCH, "GO_PLATFORM", root, "REPO_ROOT")
			got := commandTranscript{status, normalize.Replace(stdout.String()), normalize.Replace(stderr.String())}
			if strings.Contains(tc.name, "version") {
				// The v1 transcript predates the v2 Go module identity.
				got.Stdout = strings.Replace(got.Stdout, "actionlint.kjanat.dev/v2 ", "actionlint.kjanat.dev ", 1)
				got.Stdout = strings.Replace(got.Stdout, " "+buildinfo.Version()+"\n", " 1.16.0\n", 1)
				got.Stdout = strings.Replace(got.Stdout, "\n"+buildinfo.InstalledFrom()+"\n", "\ncompatibility-test\n", 1)
			}
			if tc.name == "color" {
				// Windows' colorable writer removes ANSI escapes from buffer output.
				got.Stdout = regexp.MustCompile("\x1b\\[[0-9;]*m").ReplaceAllString(got.Stdout, "")
			}
			expected, ok := want[tc.name]
			if !ok {
				t.Fatalf("missing baseline for %s", tc.name)
			}
			// v2 intentionally exposes native codes in text and template JSON.
			// Retain the captured v1 transcript for every other field and byte.
			expected.Stdout = strings.Replace(expected.Stdout, ":6:24: undefined variable", ":6:24: unknown-variable: undefined variable", 1)
			expected.Stdout = strings.Replace(expected.Stdout, `"kind":"expression-reference","snippet"`, `"kind":"expression-reference","code":"unknown-variable","snippet"`, 1)
			if diff := cmp.Diff(expected, got); diff != "" {
				t.Fatalf("CLI compatibility changed (-want +have):\n%s", diff)
			}
		})
	}
}
