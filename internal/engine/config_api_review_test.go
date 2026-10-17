package engine

import (
	"io"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	repoconfig "actionlint.kjanat.dev/internal/config"
	"actionlint.kjanat.dev/internal/yaml"
)

func TestLinterFilesSelectedPreservesInputs(t *testing.T) {
	for _, excluded := range []string{"skip.yml", "*.yml"} {
		t.Run(excluded, func(t *testing.T) {
			root := t.TempDir()
			config := writeShellcheckFixture(t, root, "actionlint.yml", "files: {excludes: ['"+excluded+"']}\n")
			paths := []string{
				writeShellcheckFixture(t, root, "skip.yml", commandGoodWorkflow),
				writeShellcheckFixture(t, root, "keep.yml", commandGoodWorkflow),
			}
			want := slices.Clone(paths)
			var selected []string
			calls := 0
			linter, err := NewLinter(io.Discard, &LinterOptions{WorkingDir: root, ConfigFile: config, OnFilesSelected: func(files []string) {
				calls++
				selected = slices.Clone(files)
				if len(files) > 0 {
					files[0] = "changed.yml"
				}
			}})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := linter.LintFiles(paths, nil); err != nil {
				t.Fatal(err)
			}
			if calls != 1 || !slices.Equal(selected, want) || !slices.Equal(paths, want) {
				t.Fatalf("callback calls=%d selected=%v inputs=%v, want %v", calls, selected, paths, want)
			}
		})
	}
}

func TestInspectConfigErrorClassification(t *testing.T) {
	for _, tc := range []struct{ name, content string }{
		{"invalid YAML", "files: ["},
		{"invalid settings", "tools: {shellcheck: {enabled: wrong}}"},
	} {
		for _, inherited := range []bool{false, true} {
			t.Run(tc.name+"/"+map[bool]string{false: "selected", true: "inherited"}[inherited], func(t *testing.T) {
				root := t.TempDir()
				path := writeShellcheckFixture(t, root, "invalid.yml", tc.content)
				if inherited {
					path = writeShellcheckFixture(t, root, "actionlint.yml", "extends: [invalid.yml]\n")
				}
				_, want := repoconfig.Load(path)
				got, err := repoconfig.Inspect(repoconfig.Selection{Path: path}, true)
				if want == nil || err == nil || err.Error() != want.Error() || !strings.Contains(err.Error(), "could not parse config file") || strings.Contains(err.Error(), "could not read config file") || got.Path != path {
					t.Fatalf("inspection=%+v error=%v, want %v", got, err, want)
				}
			})
		}
	}
	path := filepath.Join(t.TempDir(), "missing.yml")
	_, want := repoconfig.Load(path)
	_, err := repoconfig.Inspect(repoconfig.Selection{Path: path}, true)
	if err == nil || err.Error() != want.Error() || strings.Count(err.Error(), "could not read config file") != 1 {
		t.Fatalf("read error=%v, want %v", err, want)
	}
}

func TestFileOverrideExplicitToolResets(t *testing.T) {
	for _, overlay := range []string{
		"{shellcheck: {config: null}}",
		"{shellcheck: null}",
		"null",
	} {
		t.Run(overlay, func(t *testing.T) {
			config, err := repoconfig.Parse([]byte("tools: {shellcheck: {enabled: false, config: .shellcheckrc}}\noverrides: [{includes: [ci.yml], tools: " + overlay + "}]\n"))
			if err != nil {
				t.Fatal(err)
			}
			for range 2 {
				got, err := repoconfig.ForFile(config, "ci.yml", "")
				if err != nil || got.Tools.Shellcheck.Config != nil {
					t.Fatalf("explicit tool reset lost: %+v, %v", got, err)
				}
				if overlay == "{shellcheck: {config: null}}" && (got.Tools.Shellcheck.Enabled == nil || *got.Tools.Shellcheck.Enabled) {
					t.Fatal("config reset changed inherited tool enablement")
				}
				encoded, err := yaml.Marshal(config)
				if err != nil {
					t.Fatal(err)
				}
				config, err = repoconfig.Parse(encoded)
				if err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
