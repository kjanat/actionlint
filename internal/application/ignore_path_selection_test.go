package application_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/application"
	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/workspace"
)

func TestReusableIgnoreCallerRelativeFile(t *testing.T) {
	parent := t.TempDir()
	t.Chdir(parent)
	root := filepath.Join(parent, "repo")
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	const source = "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo '${{ invalid }}'\n"
	workflow := writeFixture(t, root, ".github/workflows/ci.yml", source)
	for _, pattern := range []string{".github/workflows/**", ".github/workflows/other.yml"} {
		t.Run(pattern, func(t *testing.T) {
			config := "ignores: [{rule: expression-reference, file: '" + pattern + "'}]"
			writeFixture(t, root, ".github/actionlint.yaml", config)
			cfg, err := repoconfig.Parse([]byte(config))
			if err != nil {
				t.Fatal(err)
			}
			want := 0
			if strings.HasSuffix(pattern, "other.yml") {
				want = 1
			}
			for _, path := range []string{workflow, filepath.Join("repo", ".github", "workflows", "ci.yml")} {
				session := application.Options{WorkingDir: parent}
				var err error

				result, err := application.Check(t.Context(), session, application.Input{Kind: application.Files, Paths: []string{path}})
				if err != nil || result.FileCount != 1 || len(result.Diagnostics) != want {
					t.Fatalf("session %s: %+v, %v", path, result, err)
				}
				result, err = application.Analyze(t.Context(), application.Options{WorkingDir: parent}, []workspace.Source{{Path: path, Content: []byte(source), Project: workspace.Identity(root), Config: cfg}})
				if err != nil || result.FileCount != 1 || len(result.Diagnostics) != want {
					t.Fatalf("direct %s: %+v, %v", path, result, err)
				}
			}
		})
	}
}
