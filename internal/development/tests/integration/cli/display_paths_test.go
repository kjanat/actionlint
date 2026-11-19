package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/results/report"
)

func TestDisplayPathsPreferShorterSpelling(t *testing.T) {
	dir := t.TempDir()
	working := filepath.Join(dir, "work")
	deep := filepath.Join(append([]string{working}, strings.Split(strings.Repeat("nested/", 40), "/")...)...)
	if err := os.MkdirAll(deep, 0o700); err != nil {
		t.Fatal(err)
	}
	sibling := filepath.Join(dir, "other", "ci.yml")
	far := filepath.Join(dir, "far.yml")
	for _, path := range []string{sibling, far} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(commandBadWorkflow), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct{ name, cwd, file, want string }{
		{"sibling stays relative", working, sibling, filepath.Join("..", "other", "ci.yml")},
		{"far becomes absolute", deep, far, far},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(tc.cwd)
			got := testRunCommandDefaults("", "--no-config", "-o", "pretty", "--diagnostic-width=400", "--summary", tc.file)
			if got.Status != 1 || !strings.Contains(got.Stdout, "--> "+tc.want+":6:24\n") || !strings.Contains(got.Stderr, "\n  1  "+tc.want+"\n") {
				t.Fatalf("pretty output does not show %q: %+v", tc.want, got)
			}
			got = testRunCommand("", "--no-config", "--json", tc.file)
			var result report.CheckResult
			if err := json.Unmarshal([]byte(got.Stdout), &result); err != nil {
				t.Fatalf("invalid JSON: %v: %+v", err, got)
			}
			if len(result.Diagnostics) != 1 || result.Diagnostics[0].Path != tc.want || len(result.Documents) != 1 || result.Documents[0].DocumentPath() != tc.want {
				t.Fatalf("JSON paths = %+v, documents = %+v, want %q", result.Diagnostics, result.Documents, tc.want)
			}
			got = testRunCommand(commandBadWorkflow, "check", "--no-config", "-o", "pretty", "--diagnostic-width=400", "--stdin-filename="+tc.file, "-")
			if got.Status != 1 || !strings.Contains(got.Stdout, "--> "+tc.want+":6:24\n") {
				t.Fatalf("stdin output does not show %q: %+v", tc.want, got)
			}
		})
	}
}
