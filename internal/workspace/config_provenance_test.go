package workspace_test

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/workspace"
)

func TestInheritedWarningsSurviveReplacement(t *testing.T) {
	for _, replacement := range []string{"self-hosted-runner: null", "self-hosted-runner: {typo: replacement}"} {
		t.Run(replacement, func(t *testing.T) {
			root := t.TempDir()
			base := writeToolFixture(t, root, "base.yml", `self-hosted-runner:
  typo: value
`)
			writeToolFixture(t, root, "left.yml", "extends: [base.yml]\n")
			writeToolFixture(t, root, "right.yml", "extends: [base.yml]\n")
			leaf := writeToolFixture(t, root, "leaf.yml", "extends: [left.yml, right.yml]\n"+replacement+"\n")
			inspection, err := repoconfig.Inspect(repoconfig.Selection{Path: leaf}, true)
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
				overlay, err := repoconfig.ParseOverlay("config", []byte(text))
				if err != nil {
					t.Fatal(err)
				}
				var report repoconfig.Report
				session, err := workspace.New(t.Context(), workspace.Options{ConfigFile: leaf, ConfigOverlays: []repoconfig.Overlay{overlay}, OnConfigLoaded: func(r repoconfig.Report) { report = r }})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := session.ConfigForProject(nil); err != nil {
					t.Fatal(err)
				}
				if !slices.Equal(report.Inspection.Warnings, inspection.Warnings) {
					t.Fatalf("overlay changed source warnings: %+v, want %+v", report.Inspection.Warnings, inspection.Warnings)
				}
				var log bytes.Buffer
				session, err = workspace.New(t.Context(), workspace.Options{ConfigFile: leaf, ConfigOverlays: []repoconfig.Overlay{overlay}, LogWriter: &log})
				if err != nil {
					t.Fatal(err)
				}
				for range 2 {
					if _, err := session.ConfigForProject(nil); err != nil {
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
	base := writeToolFixture(t, root, "base.yml", `# base settings
config-variables: [BASE]
config-variable: [TYPO]
`)
	leaf := writeToolFixture(t, root, "leaf.yml", `extends: [base.yml]
config-secrets: [SECRET]
`)
	for _, overlayText := range []string{"", "policy: {require-commit-hash: false}", "config-variables: [OVERLAY]"} {
		t.Run(overlayText, func(t *testing.T) {
			var overlays []repoconfig.Overlay
			if overlayText != "" {
				overlay, err := repoconfig.ParseOverlay("config", []byte(overlayText))
				if err != nil {
					t.Fatal(err)
				}
				overlays = append(overlays, overlay)
			}
			var report repoconfig.Report
			session, err := workspace.New(t.Context(), workspace.Options{ConfigFile: leaf, ConfigOverlays: overlays, OnConfigLoaded: func(r repoconfig.Report) { report = r }})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := session.ConfigForProject(nil); err != nil {
				t.Fatal(err)
			}
			origin := report.Inspection.Origins["/lint/rules/correctness/expression-reference/options/variables"]
			if strings.HasPrefix(overlayText, "config-variables:") {
				if origin.Source != "input" || origin.Input != "config" || origin.File != "" {
					t.Fatalf("overlay origin=%+v", origin)
				}
			} else if origin.File != base || origin.Line != 2 || origin.Source != "config" {
				t.Fatalf("inherited origin=%+v", origin)
			}
			if origin := report.Inspection.Origins["/lint/rules/correctness/expression-reference/options/secrets"]; origin.File != leaf || origin.Line != 2 {
				t.Fatalf("leaf origin=%+v", origin)
			}
			warnings := report.Inspection.Warnings
			if len(warnings) != 1 || warnings[0].File != base || warnings[0].Line != 3 || warnings[0].Column != 1 {
				t.Fatalf("inherited warning=%+v", warnings)
			}
			var log bytes.Buffer
			session, err = workspace.New(t.Context(), workspace.Options{ConfigFile: leaf, ConfigOverlays: overlays, LogWriter: &log})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := session.ConfigForProject(nil); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(log.String(), base+":3:1: warning:") || strings.Contains(log.String(), leaf+":3:1:") {
				t.Fatalf("warning attributed to wrong file: %s", &log)
			}
		})
	}
}

func TestMigratedPolicyOriginsRetainSourceAndReset(t *testing.T) {
	const pointer = "/lint/rules/policy/require-commit-hash/level"
	for _, value := range []string{"true", "false", "null"} {
		t.Run(value, func(t *testing.T) {
			root := t.TempDir()
			line := "policy: {require-commit-hash: " + value + "}"
			base := writeToolFixture(t, root, "base.yml", "# source policy\n"+line+"\n")
			leaf := writeToolFixture(t, root, "leaf.yml", "extends: [base.yml]\nconfig-variables: [SAFE]\n")
			inspection, err := repoconfig.Inspect(repoconfig.Selection{Path: leaf}, true)
			if err != nil {
				t.Fatal(err)
			}
			state := "value"
			if value == "null" {
				state = "null"
			}
			want := repoconfig.Origin{File: base, Source: "config", State: state, Line: 2, Column: strings.Index(line, value) + 1}
			if got := inspection.Origins[pointer]; got != want {
				t.Fatalf("origin=%+v, want %+v", got, want)
			}
			if got := inspection.Origins["/lint/rules/correctness/expression-reference/level"]; got != (repoconfig.Origin{Source: "default", State: "missing"}) {
				t.Fatalf("implicit knowledge level acquired a source origin: %+v", got)
			}
			cfg, err := repoconfig.Load(leaf)
			if err != nil || cfg.RequiresCommitHash() != (value == "true") {
				t.Fatalf("policy behavior changed: %+v %v", cfg, err)
			}
		})
	}
}
