package actionlint

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/fatih/color"
	"github.com/google/go-cmp/cmp"
)

type commandTranscript struct {
	Status int    `json:"status"`
	Stdout string `json:"stdout"`
	Stderr string `json:"stderr"`
}

func TestCommandCompatibility(t *testing.T) {
	oldVersion, oldInstalled, oldColor := version, installedFrom, color.NoColor
	version, installedFrom = "1.16.0", "compatibility-test"
	t.Cleanup(func() { version, installedFrom, color.NoColor = oldVersion, oldInstalled, oldColor })
	repoDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	t.Chdir(root)
	for _, dir := range []string{".git", ".github/workflows"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	good := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n"
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
		{"stdin-empty", []string{"-"}, ""},
		{"stdin-pretty", []string{"-"}, bad},
		{"stdin-oneline", []string{"-oneline", "-"}, bad},
		{"stdin-long-flags", []string{"--oneline", "--stdin-filename=from-stdin.yml", "-"}, bad},
		{"stdin-label", []string{"-stdin-filename", "source with spaces.yml", "-"}, bad},
		{"json-template", []string{"-format", "{{json .}}", "-"}, bad},
		{"json-template-empty", []string{"-format", "{{json .}}", "-"}, good},
		{"jsonl-template", []string{"-format", "{{range .}}{{json .}}{{end}}", "-"}, bad},
		{"custom-template", []string{"-format", `{{range .}}{{.Kind}}:{{.Line}}:{{.Column}}\n{{end}}`, "-"}, bad},
		{"template-over-oneline", []string{"-oneline", "-format", "{{json .}}", "-"}, bad},
		{"bad-template", []string{"-format", "plain", "-"}, good},
		{"bad-regexp", []string{"-ignore", "[", "-"}, good},
		{"ignored-diagnostic", []string{"-ignore", "undefined variable", "-"}, bad},
		{"repeat-ignore", []string{"-ignore", "undefined variable", "-ignore", "other", "-"}, bad},
		{"ignore-with-comma", []string{"-ignore", "undefined variable,other", "-"}, bad},
		{"boolean-false", []string{"-oneline=false", "-"}, bad},
		{"boolean-repeat", []string{"-oneline", "-oneline=false", "-"}, bad},
		{"boolean-numeric", []string{"-oneline=1", "-"}, bad},
		{"color", []string{"-color", "-no-color=false", "-"}, bad},
		{"no-color-wins", []string{"-no-color", "-color", "-"}, bad},
		{"files", []string{"bad.yml", "good.yml"}, ""},
		{"files-json-template", []string{"-format", "{{json .}}", "bad.yml", "good.yml"}, ""},
		{"repository", nil, ""},
		{"end-of-flags", []string{"--", "good.yml"}, ""},
		{"version", []string{"-version"}, ""},
		{"long-version", []string{"--version"}, ""},
		{"version-precedence", []string{"-version", "-completion", "bash", "-init-config", "-format", "bad", "-ignore", "["}, ""},
		{"version-positionals", []string{"-version", "does-not-exist"}, ""},
	}
	goldenPath := filepath.Join(repoDir, "testdata", "command", "compatibility.json")
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
			color.NoColor = true
			var stdout, stderr bytes.Buffer
			cmd := Command{Stdin: strings.NewReader(tc.in), Stdout: &stdout, Stderr: &stderr}
			args := append([]string{"actionlint", "-shellcheck=", "-pyflakes=", "-no-color"}, tc.args...)
			status := cmd.Main(args)
			normalize := strings.NewReplacer(runtime.Version(), "GO_VERSION", runtime.GOOS+"/"+runtime.GOARCH, "GO_PLATFORM", root, "REPO_ROOT")
			got := commandTranscript{status, normalize.Replace(stdout.String()), normalize.Replace(stderr.String())}
			if tc.name == "color" {
				// Windows' colorable writer removes ANSI escapes from buffer output.
				got.Stdout = regexp.MustCompile("\x1b\\[[0-9;]*m").ReplaceAllString(got.Stdout, "")
			}
			expected, ok := want[tc.name]
			if !ok {
				t.Fatalf("missing baseline for %s", tc.name)
			}
			if diff := cmp.Diff(expected, got); diff != "" {
				t.Fatalf("CLI compatibility changed (-want +have):\n%s", diff)
			}
		})
	}
}
