package checking_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	inputmodel "actionlint.kjanat.dev/v2/internal/input"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
)

func TestReusableIgnoreCallerRelativeFile(t *testing.T) {
	const source = "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo '${{ invalid }}'\npermissions: {}\n"
	for _, pattern := range []string{".github/workflows/**", ".github/workflows/other.yml"} {
		t.Run(pattern, func(t *testing.T) {
			parent := t.TempDir()
			t.Chdir(parent)
			root := filepath.Join(parent, "repo")
			if err := os.MkdirAll(filepath.Join(root, ".git"), 0o700); err != nil {
				t.Fatal(err)
			}
			workflow := writeFixture(t, root, ".github/workflows/ci.yml", source)
			config := "ignores: [{rule: expression-reference, file: '" + pattern + "'}]"
			writeFixture(t, root, ".github/actionlint.yaml", config)
			cfg, err := checking.ConfigurationRules().Parse([]byte(config))
			if err != nil {
				t.Fatal(err)
			}
			want := 0
			if strings.HasSuffix(pattern, "other.yml") {
				want = 1
			}
			for _, path := range []string{workflow, filepath.Join("repo", ".github", "workflows", "ci.yml")} {
				session := checking.Options{WorkingDir: parent}
				var err error

				result, err := checking.Check(t.Context(), session, inputmodel.Request{Kind: inputmodel.Files, Paths: []string{path}})
				if err != nil || result.FileCount != 1 || len(result.Diagnostics) != want {
					t.Fatalf("session %s: %+v, %v", path, result, err)
				}
				result, err = checking.Analyze(t.Context(), checking.Options{WorkingDir: parent}, []workspace.Source{{Path: path, Content: []byte(source), Project: workspace.Identity(root), Config: cfg}})
				if err != nil || result.FileCount != 1 || len(result.Diagnostics) != want {
					t.Fatalf("direct %s: %+v, %v", path, result, err)
				}
			}
		})
	}
}
