package actionlint

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"go.yaml.in/yaml/v4"
)

func TestConfigExtends(t *testing.T) {
	root := t.TempDir()
	writeShellcheckFixture(t, root, "base/first.yml", `lint: {rules: {correctness: {if-cond: warn}, policy: {require-job-timeout: {level: error, options: {min-minutes: 5, max-minutes: 30}}}}}
config-variables: [first]
tools: {shellcheck: {config: .shellcheckrc}}
`)
	writeShellcheckFixture(t, root, "base/second.yml", `extends: [first.yml]
config-variables: [second]
`)
	path := writeShellcheckFixture(t, root, "project/actionlint.yml", `extends: [../base/first.yml, ../base/second.yml]
lint: {rules: {policy: {require-job-timeout: {level: warn, options: {max-minutes: 60}}}}}
files: {includes: ['**/*.yml'], excludes: ['**/skip.yml']}
`)
	cfg, err := ReadConfigFile(path)
	if err != nil {
		t.Fatal(err)
	}
	resolved := (RulePresets{}).apply(cfg)
	minimum, _ := resolved.RequiresJobTimeout().MinMinutes()
	maximum, _ := resolved.RequiresJobTimeout().MaxMinutes()
	if minimum != 5 || maximum != 60 || resolved.diagnosticLevel("if-cond") != "warn" || !slices.Equal(cfg.ConfigVariables, []string{"second"}) {
		t.Fatalf("%+v", resolved)
	}
	if len(cfg.configFiles) != 3 {
		t.Fatal(cfg.configFiles)
	}
	inspection, err := InspectConfig(ConfigSelection{Path: path}, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(inspection.Origins["/lint/rules/correctness/if-cond/level"].File, "first.yml") {
		t.Fatal(inspection.Origins)
	}
	if origin := inspection.Origins["/lint/rules/correctness/if-cond"]; !strings.HasSuffix(origin.File, "first.yml") || origin.Line != 1 {
		t.Fatalf("lost shorthand origin during merging: %+v", origin)
	}
	if cfg.includesFile(".github/workflows/skip.yml", root) || !cfg.includesFile(".github/workflows/ci.yml", root) {
		t.Fatal(cfg.Files)
	}
	encoded, err := yaml.Marshal(cfg.Tools.Shellcheck.Config)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), filepath.Join(root, "base", ".shellcheckrc")) {
		t.Fatal(string(encoded))
	}
}

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
				session, err := NewAnalysisSession(AnalysisOptions{ConfigFile: initial, ReadFile: func(path string) ([]byte, error) {
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
				if !slices.Equal(reads, want) || session.defaultConfig.diagnosticLevel("if-cond") != "warn" {
					t.Fatalf("callback paths=%v, want=%v", reads, want)
				}
				if !slices.Contains(session.defaultConfig.configFiles, filepath.Join(root, "config.yml")) {
					t.Fatalf("config origin did not remain absolute: %v", session.defaultConfig.configFiles)
				}
			})
		}
	}
}

func TestConfigExtendsFailures(t *testing.T) {
	for _, tc := range []struct{ name, content, want string }{
		{"missing", "extends: [missing.yml]", "missing.yml"},
		{"cycle", "extends: [actionlint.yml]", "cyclic"},
		{"remote", "extends: ['https://example.com/config.yml']", "local file"},
		{"empty", "extends: ['']", "nonempty"},
		{"nonstring", "extends: [123]", "strings"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeShellcheckFixture(t, t.TempDir(), "actionlint.yml", tc.content)
			_, err := ReadConfigFile(path)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatal(err)
			}
			if tc.name == "missing" && errors.Is(err, os.ErrNotExist) {
				t.Fatal("missing inherited config would be mistaken for absent root config")
			}
		})
	}
	if _, err := ParseConfig([]byte("extends: [base.yml]")); err == nil {
		t.Fatal("in-memory config silently ignored extends")
	}
}

func TestFilesSelection(t *testing.T) {
	for _, tc := range []struct {
		setting string
		want    bool
	}{
		{"{}", true}, {"{includes: []}", false}, {"{includes: null}", true},
		{"{includes: ['**/*.yml']}", true}, {"{includes: ['**/*.yaml']}", false},
		{"{includes: ['**/*.yml'], excludes: ['**/ci.yml']}", false},
		{"{includes: ['!**/ci.yml', '**/*.yml']}", false},
	} {
		t.Run(tc.setting, func(t *testing.T) {
			cfg, err := ParseConfig([]byte("files: " + tc.setting))
			if err != nil {
				t.Fatal(err)
			}
			for range 2 {
				result, err := Analyze(t.Context(), AnalysisRequest{Sources: []SourceUnit{{Path: ".github/workflows/ci.yml", Content: []byte("bad yaml: ["), Config: cfg}}})
				if err != nil {
					t.Fatal(err)
				}
				if (len(result.Diagnostics) > 0) != tc.want || (len(result.files) > 0) != tc.want {
					t.Fatal(result)
				}
				b, err := yaml.Marshal(cfg)
				if err != nil {
					t.Fatal(err)
				}
				cfg, err = ParseConfig(b)
				if err != nil {
					t.Fatal(err)
				}
			}
		})
	}
	for _, setting := range []string{"{includes: ['../*']}", "{includes: [1]}", "{excludes: ['[']}", "{exclude: []}"} {
		if _, err := ParseConfig([]byte("files: " + setting)); err == nil {
			t.Fatalf("accepted %s", setting)
		}
	}
}
