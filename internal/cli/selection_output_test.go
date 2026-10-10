package cli

import (
	"os"
	"strings"
	"testing"
)

func TestReportCannotReplaceExcludedSelectionConfig(t *testing.T) {
	for _, modern := range []bool{false, true} {
		for _, target := range []string{".github/actionlint.yaml", "base.yml", ".github/workflows/ci.yml"} {
			t.Run(fmtTestName(modern, target), func(t *testing.T) {
				t.Chdir(t.TempDir())
				for _, dir := range []string{".git", ".github/workflows"} {
					if err := os.MkdirAll(dir, 0700); err != nil {
						t.Fatal(err)
					}
				}
				files := map[string]string{
					"base.yml":                 "files: {includes: []}\n",
					".github/actionlint.yaml":  "extends: ['../base.yml']\n",
					".github/workflows/ci.yml": commandGoodWorkflow,
				}
				for path, content := range files {
					if err := os.WriteFile(path, []byte(content), 0600); err != nil {
						t.Fatal(err)
					}
				}
				args := []string{"--output-file", target}
				if target == ".github/workflows/ci.yml" {
					args = append(args, target)
				}
				if modern {
					args = append([]string{"check"}, args...)
				}
				got := testRunCommand("", args...)
				if got.Status != 3 || !strings.Contains(got.Stderr, "also an input") {
					t.Fatalf("excluded selection config was not protected: %+v", got)
				}
				for path, content := range files {
					after, err := os.ReadFile(path)
					if err != nil || string(after) != content {
						t.Fatalf("input %s changed: %q, %v", path, after, err)
					}
				}
			})
		}
	}
}
