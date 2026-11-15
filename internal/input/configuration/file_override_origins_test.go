package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileOverridesSelectToolConfigOrigin(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.yml")
	content := "tools: {shellcheck: {config: file.conf}, ruff: {config: {}}}\noverrides: [{includes: [selected.yml], tools: {shellcheck: {config: selected.conf}}}]\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	rules := RuleSet{}
	loaded, err := rules.Read(path, os.ReadFile)
	if err != nil {
		t.Fatal(err)
	}
	overlay, err := rules.ParseOverlay("tools", []byte("shellcheck: {config: input.conf}\nruff: {config: {}}"))
	if err != nil {
		t.Fatal(err)
	}
	cfg, _, err := rules.Overlaid(loaded.Config, loaded, []Overlay{overlay})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		file, shellPath string
		shellInput      bool
	}{
		{"other.yml", "input.conf", true},
		{"selected.yml", "selected.conf", false},
	} {
		t.Run(tc.file, func(t *testing.T) {
			t.Parallel()
			selected, err := rules.ForFile(cfg, tc.file, "")
			if err != nil {
				t.Fatal(err)
			}
			shell := selected.Tools["shellcheck"].Config
			if shell == nil || shell.Path() != tc.shellPath || shell.FromInput() != tc.shellInput {
				t.Errorf("shellcheck settings = %#v, want path %q and input origin %v", shell, tc.shellPath, tc.shellInput)
			}
			ruff := selected.Tools["ruff"].Config
			if ruff == nil || ruff.Inline() == nil || !ruff.FromInput() {
				t.Errorf("ruff settings = %#v, want inline settings from input", ruff)
			}
		})
	}
}
