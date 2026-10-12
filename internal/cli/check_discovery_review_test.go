package cli

import (
	"os"
	"strings"
	"testing"
)

func TestReportCannotReplaceExcludedDiscoveredWorkflow(t *testing.T) {
	for _, modern := range []bool{false, true} {
		t.Run(fmtTestName(modern, "excluded"), func(t *testing.T) {
			t.Chdir(t.TempDir())
			for _, dir := range []string{".git", ".github/workflows"} {
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
			}
			const target = ".github/workflows/excluded.yml"
			const content = "invalid: [yaml\n"
			for path, data := range map[string]string{target: content, ".github/actionlint.yaml": "files: {includes: []}\n"} {
				if err := os.WriteFile(path, []byte(data), 0600); err != nil {
					t.Fatal(err)
				}
			}
			args := []string{"--output-file", target}
			if modern {
				args = append([]string{"check"}, args...)
			}
			got := testRunCommand("", args...)
			if got.Status != 3 || !strings.Contains(got.Stderr, "also an input") {
				t.Fatalf("excluded workflow collision accepted: %+v", got)
			}
			after, err := os.ReadFile(target)
			if err != nil || string(after) != content {
				t.Fatalf("excluded workflow changed: %q, %v", after, err)
			}
		})
	}
}
