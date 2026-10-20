package cli_test

import (
	"os"
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/internal/yaml"
)

type preCommitHook struct {
	ID            string   `yaml:"id"`
	PassFilenames *bool    `yaml:"pass_filenames"`
	AlwaysRun     bool     `yaml:"always_run"`
	Files         string   `yaml:"files,omitempty"`
	Types         []string `yaml:"types,omitempty"`
	Entry         string   `yaml:"entry"`
}

func readPreCommitHooks(t *testing.T) []preCommitHook {
	t.Helper()
	content, err := os.ReadFile(".pre-commit-hooks.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var hooks []preCommitHook
	if err := yaml.Unmarshal(content, &hooks); err != nil {
		t.Fatal(err)
	}
	return hooks
}

func TestPreCommitHookManifest(t *testing.T) {
	hooks := readPreCommitHooks(t)
	var ids []string
	for _, hook := range hooks {
		ids = append(ids, hook.ID)
		t.Run(hook.ID, func(t *testing.T) {
			if hook.PassFilenames == nil || *hook.PassFilenames {
				t.Fatal("hook must explicitly disable filename arguments to recheck all workflows")
			}
			if !hook.AlwaysRun {
				t.Fatal("hook must run even when pre-commit supplies no files after a deletion")
			}
			if hook.Files != "" || len(hook.Types) != 0 {
				t.Fatalf("hook must not filter files: files=%q types=%v", hook.Files, hook.Types)
			}
			if words := strings.Fields(hook.Entry); len(words) != 2 || words[1] != "pre-commit" || !strings.HasSuffix(words[0], "actionlint") && !strings.Contains(words[0], "actionlint:") {
				t.Fatalf("entry must run the actionlint pre-commit command: %q", hook.Entry)
			}
		})
	}
	want := []string{"actionlint", "actionlint-docker", "actionlint-system", "actionlint-bundled", "actionlint-shellcheck"}
	if !slices.Equal(ids, want) {
		t.Fatalf("hook inventory changed: %v, want %v", ids, want)
	}
}
