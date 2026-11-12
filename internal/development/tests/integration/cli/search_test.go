package cli_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSearchDirectoriesAndGlobsAcrossRepositories(t *testing.T) {
	parent := t.TempDir()
	t.Chdir(parent)
	for _, name := range []string{"first", "second"} {
		root := filepath.Join(parent, name)
		if output, err := exec.CommandContext(t.Context(), "git", "init", "--quiet", root).CombinedOutput(); err != nil {
			t.Fatalf("git init: %v: %s", err, output)
		}
		if err := os.MkdirAll(filepath.Join(root, ".github", "workflows"), 0o700); err != nil {
			t.Fatal(err)
		}
		config := "files: {excludes: ['.github/workflows/excluded.yml']}\ntools: {shellcheck: false, ruff: false}\n"
		if name == "first" {
			config += "lint: {rules: {correctness: {expression-reference: off}}}\n"
		}
		for path, content := range map[string]string{
			".github/actionlint.yaml":        config,
			".gitignore":                     ".github/workflows/ignored.yml\n",
			".github/workflows/ci.yml":       commandBadWorkflow,
			".github/workflows/ignored.yml":  commandBadWorkflow,
			".github/workflows/excluded.yml": "[must not be read",
			"compose.yml":                    "[must not be read",
		} {
			if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(path)), []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, paths := range [][]string{{parent}, {filepath.Join(parent, "*")}, {parent, filepath.Join(parent, "second")}} {
		got := testRunCommandDefaults("", append([]string{"-o", "json"}, paths...)...)
		var result struct {
			Files       int `json:"file_count"`
			Diagnostics []struct {
				Path string `json:"path"`
			} `json:"diagnostics"`
		}
		if err := json.Unmarshal([]byte(got.Stdout), &result); err != nil || got.Status != 1 || result.Files != 2 || len(result.Diagnostics) != 1 {
			t.Fatalf("search %v: %+v, %v", paths, got, err)
		}
		if !strings.HasPrefix(result.Diagnostics[0].Path, "second"+string(filepath.Separator)) {
			t.Fatalf("repository config leaked: %+v", result)
		}
	}
	got := testRunCommandDefaults("", "-C", parent, ".", "--no-ignore", "-o", "pretty", "-o", "github")
	if got.Status != 1 || !strings.Contains(got.Stderr, "Checked 6 files") ||
		!strings.Contains(got.Stdout, "::error file=") || !strings.Contains(got.Stdout, "error[expression-reference]") ||
		!strings.Contains(got.Stdout, filepath.Join("first", ".github", "workflows", "excluded.yml")) ||
		!strings.Contains(got.Stdout, filepath.Join("second", ".github", "workflows", "excluded.yml")) ||
		strings.Count(got.Stdout, "error[syntax-check]") != 2 {
		t.Fatalf("directory, no-ignore and repeated output: %+v", got)
	}
}
