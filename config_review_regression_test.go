package actionlint

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

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
