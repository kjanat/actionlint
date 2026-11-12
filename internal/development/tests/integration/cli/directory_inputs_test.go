package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommandDirectoryInputs(t *testing.T) {
	commandTestRepo(t)
	if err := os.MkdirAll(".github/actions/build", 0o700); err != nil {
		t.Fatal(err)
	}
	action := "name: build\ndescription: build\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      run: echo ok\n"
	if err := os.WriteFile(".github/actions/build/action.yml", []byte(action), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(".github/dependabot.yml", []byte("version: 2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{".", "--json"},
		{".github/", "--json"},
		{"check", ".", "--json"},
		{"check", ".github/", ".github/workflows", ".github/actions/build/action.yml", "--json"},
	} {
		got := testRunCommand("", args...)
		var result struct {
			FileCount int `json:"file_count"`
		}
		if err := json.Unmarshal([]byte(got.Stdout), &result); err != nil || got.Status != 1 || result.FileCount != 2 || got.Stderr != "" || !strings.Contains(got.Stdout, "expression-reference") {
			t.Fatalf("directory arguments %v: %+v (count %d, %v)", args, got, result.FileCount, err)
		}
	}
	t.Run("action-only directory", func(t *testing.T) {
		got := testRunCommand("", filepath.Join(".github", "actions"), "--json")
		if got.Status != 0 || !cleanCheckJSON(got.Stdout) || got.Stderr != "" {
			t.Fatalf("action directory: %+v", got)
		}
	})
}
