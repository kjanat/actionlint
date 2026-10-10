package actionlint

import (
	"bytes"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func TestDependencyPathsDoNotResolveWorkflowOverrides(t *testing.T) {
	for _, dependency := range []string{"local/action.yml", "scripts/lib.sh"} {
		for _, ignore := range []bool{false, true} {
			t.Run(dependency+"/"+strconv.FormatBool(ignore), func(t *testing.T) {
				root := t.TempDir()
				text := "lint:\n  rules:\n    policy:\n      require-job-timeout: {level: on, options: {min-minutes: 5}}\noverrides:\n  - includes: [local/**, scripts/**]\n    lint:\n      rules:\n        policy:\n          require-job-timeout: {level: on, options: {max-minutes: 3}}\n"
				if ignore {
					text += "paths: {'**': {ignore: ['dependency finding']}}\n"
				}
				cfg, err := ParseConfig([]byte(text))
				if err != nil {
					t.Fatal(err)
				}
				result, err := Analyze(t.Context(), AnalysisRequest{
					WorkingDir: root,
					Sources:    []SourceUnit{{Path: ".github/workflows/ci.yml", Content: []byte(commandGoodWorkflow), Config: cfg, Project: &Project{root: root}}},
					OnRulesCreated: func([]Rule) []Rule {
						rule := NewRuleBase("expression", "dependency finding")
						rule.Error(&Pos{Line: 1, Col: 1}, "dependency finding")
						rule.errs[0].Filepath = filepath.Join(root, dependency)
						rule.errs[0].source = []byte("dependency content")
						return []Rule{&rule}
					},
				})
				if err != nil {
					t.Fatal(err)
				}
				want := 1
				if ignore {
					want = 0
				}
				if len(result.Diagnostics) != want {
					t.Fatalf("got findings %+v, want %d", result.Diagnostics, want)
				}
			})
		}
	}
}

func TestInheritedWarningsSurviveReplacement(t *testing.T) {
	for _, replacement := range []string{"self-hosted-runner: null", "self-hosted-runner: {typo: replacement}"} {
		t.Run(replacement, func(t *testing.T) {
			root := t.TempDir()
			base := writeShellcheckFixture(t, root, "base.yml", "self-hosted-runner:\n  typo: value\n")
			writeShellcheckFixture(t, root, "left.yml", "extends: [base.yml]\n")
			writeShellcheckFixture(t, root, "right.yml", "extends: [base.yml]\n")
			leaf := writeShellcheckFixture(t, root, "leaf.yml", "extends: [left.yml, right.yml]\n"+replacement+"\n")
			inspection, err := InspectConfig(ConfigSelection{Path: leaf}, true)
			if err != nil {
				t.Fatal(err)
			}
			want := 1
			if strings.Contains(replacement, "replacement") {
				want = 2
			}
			if len(inspection.Warnings) != want || inspection.Warnings[0].File != base || inspection.Warnings[0].Line != 2 || inspection.Warnings[0].Column != 3 {
				t.Fatalf("source warning lost or duplicated: %+v", inspection.Warnings)
			}
			if want == 2 && inspection.Warnings[1].File != leaf {
				t.Fatalf("leaf warning attribution: %+v", inspection.Warnings)
			}
			for _, text := range []string{"self-hosted-runner: null", "config-variables: [SAFE]"} {
				overlay, err := ParseConfigOverlay("config", []byte(text))
				if err != nil {
					t.Fatal(err)
				}
				var report ConfigReport
				session, err := NewAnalysisSession(AnalysisOptions{ConfigFile: leaf, ConfigOverlays: []ConfigOverlay{overlay}, OnConfigLoaded: func(r ConfigReport) { report = r }})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := session.configForProject(nil); err != nil {
					t.Fatal(err)
				}
				if !slices.Equal(report.Inspection.Warnings, inspection.Warnings) {
					t.Fatalf("overlay changed source warnings: %+v, want %+v", report.Inspection.Warnings, inspection.Warnings)
				}
				var log bytes.Buffer
				session, err = NewAnalysisSession(AnalysisOptions{ConfigFile: leaf, ConfigOverlays: []ConfigOverlay{overlay}, LogWriter: &log})
				if err != nil {
					t.Fatal(err)
				}
				for range 2 {
					if _, err := session.configForProject(nil); err != nil {
						t.Fatal(err)
					}
				}
				if strings.Count(log.String(), "warning:") != want || !strings.Contains(log.String(), base+":2:3: warning:") {
					t.Fatalf("incorrect warning log: %s", &log)
				}
			}
		})
	}
}

func TestConfigInheritedOverlayProvenance(t *testing.T) {
	root := t.TempDir()
	base := writeShellcheckFixture(t, root, "base.yml", "# base settings\nconfig-variables: [BASE]\nconfig-variable: [TYPO]\n")
	leaf := writeShellcheckFixture(t, root, "leaf.yml", "extends: [base.yml]\nconfig-secrets: [SECRET]\n")
	for _, overlayText := range []string{"", "policy: {require-commit-hash: false}", "config-variables: [OVERLAY]"} {
		t.Run(overlayText, func(t *testing.T) {
			var overlays []ConfigOverlay
			if overlayText != "" {
				overlay, err := ParseConfigOverlay("config", []byte(overlayText))
				if err != nil {
					t.Fatal(err)
				}
				overlays = append(overlays, overlay)
			}
			var report ConfigReport
			session, err := NewAnalysisSession(AnalysisOptions{ConfigFile: leaf, ConfigOverlays: overlays, OnConfigLoaded: func(r ConfigReport) { report = r }})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := session.configForProject(nil); err != nil {
				t.Fatal(err)
			}
			origin := report.Inspection.Origins["/config-variables"]
			if strings.HasPrefix(overlayText, "config-variables:") {
				if origin.Source != "input" || origin.Input != "config" || origin.File != "" {
					t.Fatalf("overlay origin=%+v", origin)
				}
			} else if origin.File != base || origin.Line != 2 || origin.Source != "config" {
				t.Fatalf("inherited origin=%+v", origin)
			}
			if origin := report.Inspection.Origins["/config-secrets"]; origin.File != leaf || origin.Line != 2 {
				t.Fatalf("leaf origin=%+v", origin)
			}
			warnings := report.Inspection.Warnings
			if len(warnings) != 1 || warnings[0].File != base || warnings[0].Line != 3 || warnings[0].Column != 1 {
				t.Fatalf("inherited warning=%+v", warnings)
			}
			var log bytes.Buffer
			session, err = NewAnalysisSession(AnalysisOptions{ConfigFile: leaf, ConfigOverlays: overlays, LogWriter: &log})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := session.configForProject(nil); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(log.String(), base+":3:1: warning:") || strings.Contains(log.String(), leaf+":3:1:") {
				t.Fatalf("warning attributed to wrong file: %s", &log)
			}
		})
	}
}

func TestWorkflowOverridesApplyToDependencyDiagnostics(t *testing.T) {
	for _, tc := range []struct{ name, lint, severity string }{
		{"disabled workflow", "enabled: false", ""},
		{"disabled rule", "rules: {correctness: {expression: off}}", ""},
		{"warning rule", "rules: {correctness: {expression: warn}}", "warning"},
		{"info rule", "rules: {correctness: {expression: info}}", "info"},
	} {
		for _, dependency := range []string{"local/action.yml", "scripts/lib.sh"} {
			t.Run(tc.name+"/"+dependency, func(t *testing.T) {
				root := t.TempDir()
				cfg, err := ParseConfig([]byte("overrides: [{includes: ['./.github/workflows/**'], lint: {" + tc.lint + "}}]"))
				if err != nil {
					t.Fatal(err)
				}
				result, err := Analyze(t.Context(), AnalysisRequest{
					WorkingDir: root,
					Sources:    []SourceUnit{{Path: ".github/workflows/ci.yml", Content: []byte(commandGoodWorkflow), Config: cfg, Project: &Project{root: root}}},
					OnRulesCreated: func([]Rule) []Rule {
						rule := NewRuleBase("expression", "dependency finding")
						rule.Error(&Pos{Line: 1, Col: 1}, "dependency finding")
						rule.errs[0].Filepath = filepath.Join(root, dependency)
						rule.errs[0].source = []byte("dependency content")
						return []Rule{&rule}
					},
				})
				if err != nil {
					t.Fatal(err)
				}
				if tc.severity == "" {
					if len(result.Diagnostics) != 0 {
						t.Fatalf("disabled findings remain: %+v", result.Diagnostics)
					}
				} else if len(result.Diagnostics) != 1 || result.Diagnostics[0].Severity != tc.severity {
					t.Fatalf("dependency severity=%+v", result.Diagnostics)
				}
			})
		}
	}
}

func TestWorkflowOverrideControlsCompositeFindings(t *testing.T) {
	for _, tc := range []struct{ lint, severity string }{
		{"rules: {correctness: {action: warn}}", "warning"},
		{"enabled: false", ""},
	} {
		t.Run(tc.lint, func(t *testing.T) {
			root := t.TempDir()
			writeShellcheckFixture(t, root, "local/action.yml", "name: local\ndescription: test\nruns:\n  using: composite\n  steps:\n    - uses: actions/checkout@v5\n      with:\n        nonexistent-input: value\n")
			cfg, err := ParseConfig([]byte("overrides: [{includes: ['.github/workflows/**'], lint: {" + tc.lint + "}}]"))
			if err != nil {
				t.Fatal(err)
			}
			result, err := Analyze(t.Context(), AnalysisRequest{WorkingDir: root, Sources: []SourceUnit{{
				Path: ".github/workflows/ci.yml", Config: cfg, Project: &Project{root: root},
				Content: []byte("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: $/local\n"),
			}}})
			if err != nil {
				t.Fatal(err)
			}
			if tc.severity == "" {
				if len(result.Diagnostics) != 0 {
					t.Fatalf("disabled composite findings remain: %+v", result.Diagnostics)
				}
			} else if len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != "action" || result.Diagnostics[0].Severity != tc.severity || filepath.ToSlash(result.Diagnostics[0].Path) != "local/action.yml" {
				t.Fatalf("composite override not applied: %+v", result.Diagnostics)
			}
		})
	}
}
