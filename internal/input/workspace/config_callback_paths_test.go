package workspace_test

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"

	checking "actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
)

func TestConfigCallbacksUseAbsolutePaths(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	for _, initial := range []string{"config.yml", "./config.yml"} {
		for _, inherited := range []bool{false, true} {
			t.Run(initial+strconv.FormatBool(inherited), func(t *testing.T) {
				t.Parallel()
				base := filepath.Join(root, "base.yml")
				content := "lint: {rules: {correctness: {if-cond: warn}}}\n"
				if inherited {
					content = "extends: [base.yml]\n"
				}
				var reads []string
				session, err := workspace.New(t.Context(), workspace.Options{ConfigFile: initial, ReadFile: func(path string) ([]byte, error) {
					reads = append(reads, path)
					switch path {
					case filepath.Join(root, "config.yml"):
						return []byte(content), nil
					case base:
						return []byte("lint: {rules: {correctness: {if-cond: warn}}}\n"), nil
					default:
						return nil, os.ErrNotExist
					}
				}, Rules: checking.ConfigurationRules()})
				if err != nil {
					t.Fatal(err)
				}
				want := []string{filepath.Join(root, "config.yml")}
				if inherited {
					want = append(want, base)
				}
				selection, err := session.Content("workflow.yml", nil, nil, false)
				if err != nil {
					t.Fatal(err)
				}
				config := selection.Sources[0].Config
				if !slices.Equal(reads, want) || checking.ConfigurationRules().DiagnosticLevel(config, "if-cond") != "warn" {
					t.Fatalf("callback paths=%v, want=%v", reads, want)
				}
				if !slices.Contains(selection.Sources[0].ConfigProvenance.Sources(), filepath.Join(root, "config.yml")) {
					t.Fatalf("config origin did not remain absolute: %v", selection.Sources[0].ConfigProvenance.Sources())
				}
			})
		}
	}
}
