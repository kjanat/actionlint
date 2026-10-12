package actionlint

import (
	"io"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v4"
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

func TestProgrammaticToolOverridesPreserveOmissions(t *testing.T) {
	for _, tc := range []struct{ name, base, override string }{
		{"disabled", "{enabled: true, config: .shellcheckrc}", "{enabled: false}"},
		{"enabled", "{enabled: false, config: {disable: [SC2086]}}", "{enabled: true}"},
		{"config only", "{enabled: false, config: .shellcheckrc}", "{config: replacement.rc}"},
		{"empty", "{enabled: false, config: .shellcheckrc}", "{}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config, err := ParseConfig([]byte("tools: {shellcheck: " + tc.base + "}\noverrides: [{includes: [ci.yml], tools: {shellcheck: " + tc.override + "}}]\n"))
			if err != nil {
				t.Fatal(err)
			}
			want, err := configForFile(config, "ci.yml", "")
			if err != nil {
				t.Fatal(err)
			}
			config.Overrides[0].toolsNode = nil
			for range 2 {
				got, err := configForFile(config, "ci.yml", "")
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(got.Tools, want.Tools) {
					t.Fatalf("programmatic tools differ: got %#v, want %#v", got.Tools.Shellcheck, want.Tools.Shellcheck)
				}
				encoded, err := yaml.Marshal(config)
				if err != nil {
					t.Fatal(err)
				}
				config, err = ParseConfig(encoded)
				if err != nil {
					t.Fatal(err)
				}
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
				_, want := ReadConfigFile(path)
				got, err := InspectConfig(ConfigSelection{Path: path}, true)
				if want == nil || err == nil || err.Error() != want.Error() || !strings.Contains(err.Error(), "could not parse config file") || strings.Contains(err.Error(), "could not read config file") || got.Path != path {
					t.Fatalf("inspection=%+v error=%v, want %v", got, err, want)
				}
			})
		}
	}
	path := filepath.Join(t.TempDir(), "missing.yml")
	_, want := ReadConfigFile(path)
	_, err := InspectConfig(ConfigSelection{Path: path}, true)
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
			config, err := ParseConfig([]byte("tools: {shellcheck: {enabled: false, config: .shellcheckrc}}\noverrides: [{includes: [ci.yml], tools: " + overlay + "}]\n"))
			if err != nil {
				t.Fatal(err)
			}
			for range 2 {
				got, err := configForFile(config, "ci.yml", "")
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
				config, err = ParseConfig(encoded)
				if err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
