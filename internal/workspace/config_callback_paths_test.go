package workspace_test

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"

	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/workspace"
)

func TestRelativeConfigCallbackPath(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	for _, initial := range []string{"config.yml", "./config.yml"} {
		for _, inherited := range []bool{false, true} {
			t.Run(initial+strconv.FormatBool(inherited), func(t *testing.T) {
				base := filepath.Join(root, "base.yml")
				content := "lint: {rules: {correctness: {if-cond: warn}}}\n"
				if inherited {
					content = "extends: [base.yml]\n"
				}
				var reads []string
				session, err := workspace.New(workspace.Options{ConfigFile: initial, ReadFile: func(path string) ([]byte, error) {
					reads = append(reads, path)
					switch path {
					case initial:
						return []byte(content), nil
					case base:
						return []byte("lint: {rules: {correctness: {if-cond: warn}}}\n"), nil
					default:
						return nil, os.ErrNotExist
					}
				}})
				if err != nil {
					t.Fatal(err)
				}
				want := []string{initial}
				if inherited {
					want = append(want, base)
				}
				if !slices.Equal(reads, want) || repoconfig.DiagnosticLevel(session.DefaultConfig, "if-cond") != "warn" {
					t.Fatalf("callback paths=%v, want=%v", reads, want)
				}
				if !slices.Contains(session.DefaultConfig.Sources(), filepath.Join(root, "config.yml")) {
					t.Fatalf("config origin did not remain absolute: %v", session.DefaultConfig.Sources())
				}
			})
		}
	}
}
