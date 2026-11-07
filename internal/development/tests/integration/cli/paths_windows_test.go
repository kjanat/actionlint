package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func windowsPathSpellings(path string) map[string]string {
	forward := filepath.ToSlash(path)
	return map[string]string{
		"native":  path,
		"forward": forward,
		"mixed":   strings.Replace(forward, "/", `\`, 1),
	}
}

func TestWindowsForwardSlashCheckPaths(t *testing.T) {
	for _, modern := range []bool{false, true} {
		for _, spelling := range []string{"native", "forward", "mixed"} {
			t.Run(fmtTestName(modern, spelling), func(t *testing.T) {
				root := filepath.Join(t.TempDir(), "repo with spaces #é")
				for _, dir := range []string{".git", ".github/workflows", "config dir", "report dir"} {
					if err := os.MkdirAll(filepath.Join(root, dir), 0700); err != nil {
						t.Fatal(err)
					}
				}
				t.Chdir(root)
				files := map[string]string{
					".github/workflows/test.yml": commandBadWorkflow,
					"base config.yml":            "lint: {rules: {correctness: {expression-reference: off}}}\n",
					"config dir/leaf.yml":        "extends: ['../base config.yml']\nfiles: {includes: ['.github/workflows/*.yml']}\n",
					"report template.tmpl":       "{{json .}}",
				}
				for name, content := range files {
					if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0600); err != nil {
						t.Fatal(err)
					}
				}
				path := func(name string) string { return windowsPathSpellings(filepath.Join(root, name))[spelling] }
				checkCount := func(args ...string) {
					t.Helper()
					got := testRunCommand(commandBadWorkflow, args...)
					var result struct {
						FileCount *int `json:"file_count"`
					}
					if err := json.Unmarshal([]byte(got.Stdout), &result); err != nil || got.Status != 0 || result.FileCount == nil || *result.FileCount != 1 {
						t.Fatalf("input/config selection: %+v, %v", got, err)
					}
				}
				checkCount("check", "--json", "--config", path("config dir/leaf.yml"), path(".github/workflows/test.yml"))
				t.Setenv("ACTIONLINT_CONFIG", path("config dir/leaf.yml"))
				checkCount("check", "--json", path(".github/workflows/test.yml"))
				checkCount("check", "--json", "--stdin-filename", path(".github/workflows/test.yml"), "-")
				args := []string{"--config", path("config dir/leaf.yml"), "--template-file", path("report template.tmpl"), "--output-file", path("report dir/report.json")}
				if modern {
					args = append([]string{"check"}, args...)
				}
				got := testRunCommand("", append(slices.Clone(args), path(".github/workflows/test.yml"))...)
				data, err := os.ReadFile(filepath.Join(root, "report dir/report.json"))
				if got.Status != 0 || got.Stderr != "" || err != nil || !json.Valid(data) {
					t.Fatalf("path handling failed: %+v; %s; %v", got, data, err)
				}
				got = testRunCommand("", append(slices.Clone(args), path(".github/workflows/test.yml"))...)
				if got.Status != 3 || !strings.Contains(got.Stderr, "already exists") {
					t.Fatalf("existing output was not recognised: %+v", got)
				}
				got = testRunCommand("", append(slices.Clone(args), "--yes", path(".github/workflows/test.yml"))...)
				if got.Status != 0 {
					t.Fatalf("explicit overwrite failed: %+v", got)
				}
				got = testRunCommand("", "check", "--yes", "--output-file", path(".github/workflows/test.yml"), filepath.Join(root, ".github/workflows/test.yml"))
				if got.Status != 3 || !strings.Contains(got.Stderr, "also an input") {
					t.Fatalf("alternate input spelling escaped protection: %+v", got)
				}
			})
		}
	}
}
