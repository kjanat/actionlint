package actionlint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v4"
)

func TestRuffConfiguration(t *testing.T) {
	for _, text := range []string{
		"tools: {ruff: false}",
		"tools: {ruff: {enabled: false, target-version: py312, select: [F, B], ignore: [F401]}}",
		"tools: {ruff: {select: []}}",
		"tools: {ruff: {ignore: []}}",
	} {
		cfg, err := ParseConfig([]byte(text))
		if err != nil {
			t.Fatal(err)
		}
		data, err := yaml.Marshal(cfg)
		if err != nil {
			t.Fatal(err)
		}
		again, err := ParseConfig(data)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Tools.Ruff.Select != nil && again.Tools.Ruff.Select == nil {
			t.Fatal("empty selection was lost")
		}
		if cfg.Tools.Ruff.Select == nil && again.Tools.Ruff.Select != nil {
			t.Fatal("default selection became empty selection")
		}
		if (cfg.Tools.Ruff.Ignore == nil) != (again.Tools.Ruff.Ignore == nil) {
			t.Fatal("ignore selection changed between omitted and explicit empty")
		}
	}
	for _, text := range []string{
		"tools: {ruff: {typo: true}}",
		"tools: {ruff: {enabled: wrong}}",
		"tools: {ruff: {enabled: 'false'}}",
		"tools: {ruff: {target-version: '--fix'}}",
		"tools: {ruff: {select: ['F,--fix']}}",
		"tools: {ruff: {ignore: ['']}}",
	} {
		if _, err := ParseConfig([]byte(text)); err == nil {
			t.Fatalf("accepted %s", text)
		}
	}
}

func TestRuffTargetVersionConfiguration(t *testing.T) {
	for _, version := range []string{"py37", "py38", "py39", "py310", "py311", "py312", "py313", "py314", "py315", "py27", "py30", "py36", "py316", "py399", "py0314", "3.14", "PY314"} {
		t.Run(version, func(t *testing.T) {
			valid := version == "py37" || version == "py38" || version == "py39" || version == "py310" || version == "py311" || version == "py312" || version == "py313" || version == "py314" || version == "py315"
			setting := "ruff: {target-version: '" + version + "'}"
			for _, text := range []string{"tools: {" + setting + "}", "overrides: [{includes: ['**'], tools: {" + setting + "}}]"} {
				_, err := ParseConfig([]byte(text))
				if (err == nil) != valid || (err != nil && !strings.Contains(err.Error(), "tools.ruff.target-version")) {
					t.Fatalf("ParseConfig(%q): %v; valid=%v", text, err, valid)
				}
			}
			if _, err := ParseConfigOverlay("tools", []byte(setting)); (err == nil) != valid || (err != nil && !strings.Contains(err.Error(), "tools.ruff.target-version")) {
				t.Fatalf("ParseConfigOverlay(%q): %v; valid=%v", setting, err, valid)
			}
		})
	}
}

func TestRuffSelectorConfiguration(t *testing.T) {
	for _, tc := range []struct {
		selector string
		valid    bool
	}{
		{"F", true}, {"F8", true}, {"F82", true}, {"F821", true},
		{"ALL", true}, {"C", true}, {"T", true}, {"C9", true},
		{"SIM111", true}, {"U004", true}, {"TCH", true},
		{"E111", false}, {"PLR0904", false}, {"RUF055", false}, {"CPY001", true},
		{"ANN101", false}, {"ANN102", false}, {"S320", false}, {"PGH001", true},
		{"XYZ", false}, {"F9999", false}, {"F999", false},
		{"P", false}, {"RUF940", false}, {"PREVIEW", false},
		{"undefined-name", false}, {"correctness", false}, {"", false},
	} {
		t.Run(tc.selector, func(t *testing.T) {
			for _, key := range []string{"select", "ignore"} {
				setting := "ruff: {" + key + ": ['" + tc.selector + "']}"
				for _, text := range []string{"tools: {" + setting + "}", "overrides: [{includes: ['**'], tools: {" + setting + "}}]"} {
					_, err := ParseConfig([]byte(text))
					if (err == nil) != tc.valid || (err != nil && !strings.Contains(err.Error(), "invalid rule selector")) {
						t.Fatalf("ParseConfig(%q): %v; valid=%v", text, err, tc.valid)
					}
				}
				if _, err := ParseConfigOverlay("tools", []byte(setting)); (err == nil) != tc.valid {
					t.Fatalf("ParseConfigOverlay(%q): %v; valid=%v", setting, err, tc.valid)
				}
			}
		})
	}
}

func TestRuffTargetVersionEmptyAndReset(t *testing.T) {
	for _, tc := range []struct {
		value string
		valid bool
	}{
		{"''", false},
		{`""`, false},
		{"null", true},
	} {
		t.Run(tc.value, func(t *testing.T) {
			setting := "ruff: {target-version: " + tc.value + "}"
			for _, text := range []string{"tools: {" + setting + "}", "overrides: [{includes: ['**'], tools: {" + setting + "}}]"} {
				_, err := ParseConfig([]byte(text))
				if (err == nil) != tc.valid || (err != nil && !strings.Contains(err.Error(), "tools.ruff.target-version")) {
					t.Fatalf("ParseConfig(%q): %v; valid=%v", text, err, tc.valid)
				}
			}
			if _, err := ParseConfigOverlay("tools", []byte(setting)); (err == nil) != tc.valid {
				t.Fatalf("ParseConfigOverlay(%q): %v; valid=%v", setting, err, tc.valid)
			}
		})
	}
	for _, overlayText := range []string{"ruff: {}", "ruff: {target-version: null}"} {
		path := writeShellcheckFixture(t, t.TempDir(), "actionlint.yml", "tools: {ruff: {target-version: py312}}")
		overlay, err := ParseConfigOverlay("tools", []byte(overlayText))
		if err != nil {
			t.Fatal(err)
		}
		session, err := NewAnalysisSession(AnalysisOptions{ConfigFile: path, ConfigOverlays: []ConfigOverlay{overlay}})
		if err != nil {
			t.Fatal(err)
		}
		cfg, err := session.configForProject(nil)
		if err != nil {
			t.Fatal(err)
		}
		resolved, err := effectiveConfig(cfg)
		if err != nil {
			t.Fatal(err)
		}
		want := "py312"
		if strings.Contains(overlayText, "null") {
			want = "py314"
		}
		if got := resolved["tools"].(map[string]any)["ruff"].(map[string]any)["target-version"]; got != want {
			t.Fatalf("overlay %q resolved to %v, want %s", overlayText, got, want)
		}
	}
}

func TestRuffShorthandOverlay(t *testing.T) {
	for _, tc := range []struct {
		name, base, overlay string
		enabled             bool
	}{
		{"disable retains settings", "tools: {ruff: {target-version: py312, select: [F821], ignore: [F401]}}", "ruff: false", false},
		{"enable retains settings", "tools: {ruff: {enabled: false, target-version: py312, select: [F821], ignore: [F401]}}", "ruff: true", true},
		{"settings retain switch", "tools: {ruff: false}", "ruff: {target-version: py312, select: [F821], ignore: [F401]}", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeShellcheckFixture(t, t.TempDir(), "actionlint.yml", tc.base)
			overlay, err := ParseConfigOverlay("tools", []byte(tc.overlay))
			if err != nil {
				t.Fatal(err)
			}
			session, err := NewAnalysisSession(AnalysisOptions{ConfigFile: path, ConfigOverlays: []ConfigOverlay{overlay}})
			if err != nil {
				t.Fatal(err)
			}
			cfg, err := session.configForProject(nil)
			if err != nil {
				t.Fatal(err)
			}
			ruff := cfg.Tools.Ruff
			if ruff.Enabled == nil || *ruff.Enabled != tc.enabled || ruff.TargetVersion != "py312" || len(ruff.Select) != 1 || ruff.Select[0] != "F821" || len(ruff.Ignore) != 1 || ruff.Ignore[0] != "F401" {
				t.Fatalf("lost Ruff settings: %+v", ruff)
			}
		})
	}
}

func TestRuffRequiredToolsOverrides(t *testing.T) {
	root := t.TempDir()
	path := writeShellcheckFixture(t, root, "actionlint.yml", "tools: {shellcheck: false}\nfiles: {excludes: [skip.yml]}\noverrides:\n  - includes: [disabled.yml]\n    lint: {rules: {external: {ruff: off}}}\n")
	session, err := NewAnalysisSession(AnalysisOptions{WorkingDir: root, ConfigFile: path, Ruff: "ruff", Shellcheck: "shellcheck"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		path string
		want bool
	}{{"ci.yml", true}, {"skip.yml", false}, {"disabled.yml", false}} {
		tools, err := session.RequiredTools([]string{tc.path})
		if err != nil || tools.Ruff != tc.want || tools.Shellcheck {
			t.Fatalf("%s: %+v %v", tc.path, tools, err)
		}
	}
}

func TestRuffRequiredToolsCompositeOverrides(t *testing.T) {
	for _, tc := range []struct {
		name, base, override string
		want                 bool
	}{
		{"tool reenabled", "tools: {ruff: false}", "tools: {ruff: true}", true},
		{"tool reset", "tools: {ruff: false}", "tools: null", true},
		{"rule reenabled", "lint: {rules: {external: {ruff: off}}}", "lint: {rules: {external: {ruff: on}}}", true},
		{"tool remains disabled", "tools: {ruff: false}", "tools: {ruff: {select: [F821]}}", false},
		{"tool explicitly disabled", "tools: {ruff: false}", "tools: {ruff: false}", false},
		{"rule remains disabled", "lint: {rules: {external: {ruff: off}}}", "lint: {rules: {external: {ruff: off}}}", false},
		{"rule blocked by tool", "tools: {ruff: false}\nlint: {rules: {external: {ruff: off}}}", "lint: {rules: {external: {ruff: on}}}", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			path := writeShellcheckFixture(t, root, "actionlint.yml", tc.base+"\noverrides:\n  - includes: [action.yml]\n    "+tc.override+"\n")
			for _, executable := range []string{"ruff", ""} {
				session, err := NewAnalysisSession(AnalysisOptions{WorkingDir: root, ConfigFile: path, Ruff: executable})
				if err != nil {
					t.Fatal(err)
				}
				tools, err := session.RequiredTools([]string{"ci.yml"})
				if err != nil || tools.Ruff != (tc.want && executable != "") {
					t.Fatalf("tools=%+v, error=%v", tools, err)
				}
			}
		})
	}
}

func TestRuffRequiredToolsProjectCompositeBaseline(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		root := t.TempDir()
		writeShellcheckFixture(t, root, ".git", "")
		workflow := writeShellcheckFixture(t, root, ".github/workflows/ci.yml", "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: $/python-action\n")
		writeShellcheckFixture(t, root, "python-action/action.yml", "name: Python\ndescription: test\nruns:\n  using: composite\n  steps:\n    - shell: python\n      run: print(missing)\n")
		base := ""
		if !enabled {
			base = "tools: {ruff: false}\n"
		}
		path := writeShellcheckFixture(t, root, "actionlint.yml", base+"overrides:\n  - includes: ['.github/workflows/**']\n    tools: {ruff: false}\n")
		session, err := NewAnalysisSession(AnalysisOptions{WorkingDir: root, ConfigFile: path, Ruff: "ruff"})
		if err != nil {
			t.Fatal(err)
		}
		tools, err := session.RequiredTools([]string{workflow})
		if err != nil || tools.Ruff != enabled {
			t.Fatalf("enabled=%v: tools=%+v error=%v", enabled, tools, err)
		}
	}
}

func TestRuffProjectCatchAllOverrides(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, overrides string
		enabled         bool
	}{
		{"all tools disabled", "[{includes: ['**'], tools: {ruff: false}}]", false},
		{"all rules disabled", "[{includes: ['**'], lint: {rules: {external: {ruff: off}}}}]", false},
		{"relative catch-all tools disabled", "[{includes: ['./**'], tools: {ruff: false}}]", false},
		{"file catch-all tools disabled", "[{includes: ['**/*'], tools: {ruff: false}}]", false},
		{"relative file catch-all rules disabled", "[{includes: ['./**/*'], lint: {rules: {external: {ruff: off}}}}]", false},
		{"caller lint disabled", "[{includes: ['.github/workflows/**'], lint: {enabled: false}}]", false},
		{"caller lint disabled despite composite enabled", "[{includes: ['.github/workflows/**'], lint: {enabled: false}}, {includes: ['python-action/**'], lint: {enabled: true}}]", false},
		{"later tool reenabled", "[{includes: ['**'], tools: {ruff: false}}, {includes: ['python-action/action.yml'], tools: {ruff: true}}]", true},
		{"later rule reenabled", "[{includes: ['**'], lint: {rules: {external: {ruff: off}}}}, {includes: ['python-action/action.yml'], lint: {rules: {external: {ruff: on}}}}]", true},
		{"later catch-all disables tool", "[{includes: ['python-action/action.yml'], tools: {ruff: true}}, {includes: ['**'], tools: {ruff: false}}]", false},
		{"later catch-all disables rule", "[{includes: ['python-action/action.yml'], lint: {rules: {external: {ruff: on}}}}, {includes: ['**'], lint: {rules: {external: {ruff: off}}}}]", false},
		{"workflow-only disabled", "[{includes: ['.github/workflows/**'], tools: {ruff: false}}]", true},
		{"catch-all has exception", "[{includes: ['**'], excludes: ['python-action/**'], tools: {ruff: false}}]", true},
		{"catch-all has negative include", "[{includes: ['**', '!python-action/**'], tools: {ruff: false}}]", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeShellcheckFixture(t, root, ".git", "")
			workflow := writeShellcheckFixture(t, root, ".github/workflows/ci.yml", "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: $/python-action\n")
			writeShellcheckFixture(t, root, "python-action/action.yml", "name: Python\ndescription: test\nruns:\n  using: composite\n  steps:\n    - shell: python\n      run: print(missing)\n")
			writeShellcheckFixture(t, root, ".github/actionlint.yaml", "overrides: "+tc.overrides+"\n")
			command := executable
			if !tc.enabled {
				command = filepath.Join(root, "missing-ruff")
			}
			created := false
			session, err := NewAnalysisSession(AnalysisOptions{
				WorkingDir:  root,
				RuffOptions: &ExternalCommandOptions{Executable: &command, Environment: []string{"ACTIONLINT_TEST_RUFF=1", "ACTIONLINT_TEST_RUFF_OUTPUT=[]"}},
				OnRulesCreated: func(rules []Rule) []Rule {
					for _, rule := range rules {
						if _, ok := rule.(*ruffRule); ok {
							created = true
						}
					}
					return rules
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			needed, err := session.RequiredTools([]string{workflow})
			if err != nil || needed.Ruff != tc.enabled {
				t.Fatalf("needed=%+v, error=%v", needed, err)
			}
			if _, err := session.Files([]string{workflow}, nil); err != nil || created != tc.enabled {
				t.Fatalf("checker created=%v, error=%v", created, err)
			}
		})
	}
}
