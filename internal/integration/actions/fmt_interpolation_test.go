package actions_test

import (
	"os"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/application"
	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/testutil"
)

func TestFmtCompositeArgumentInterpolation(t *testing.T) {
	fixture, err := os.ReadFile("internal/integration/actions/testdata/fmt/action.yml")
	if err != nil {
		t.Fatal(err)
	}
	root, _ := testutil.ExecutableFixture(t, "shellcheck", "ruff")
	for _, tc := range []struct {
		name, source string
		unsafe       bool
	}{
		{"exact upstream without suppression", string(fixture), false},
		{"unknown branch output", strings.Replace(string(fixture), "'--excludes .github'", "inputs.version", 1), true},
		{"unknown fallback output", strings.Replace(string(fixture), "'--excludes .github', ''", "'--excludes .github', inputs.version", 1), true},
		{"package interpolated into quoted script", strings.Replace(string(fixture), "${pkg}", "${{ env.pkg }}", 1), true},
		{"command separator", strings.Replace(string(fixture), "--excludes .github", "--excludes .github; id", 1), true},
		{"command substitution", strings.Replace(string(fixture), "--excludes .github", "--excludes $(id)", 1), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testutil.WriteFixture(t, root, "fmt/action.yml", tc.source)
			result := testutil.Composite(t, root, "- uses: ./fmt", application.Options{})
			interpolation := false
			for _, finding := range result.Diagnostics {
				if finding.Rule != "script-interpolation" {
					t.Fatalf("unexpected diagnostic: %+v", finding)
				}
				interpolation = true
			}
			if interpolation != tc.unsafe {
				t.Fatalf("interpolation=%t, want %t: %+v", interpolation, tc.unsafe, result.Diagnostics)
			}
		})
	}
}

func TestFmtCompositeUnusedSuppression(t *testing.T) {
	fixture, err := os.ReadFile("internal/integration/actions/testdata/fmt/action.yml")
	if err != nil {
		t.Fatal(err)
	}
	root, _ := testutil.ExecutableFixture(t, "shellcheck", "ruff")
	source := strings.Replace(string(fixture), "${{ env.cmd }}", "${{ env.cmd }} # ignore script-interpolation", 1)
	testutil.WriteFixture(t, root, "fmt/action.yml", source)
	for _, strict := range []bool{false, true} {
		result := testutil.Composite(t, root, "- uses: ./fmt", application.Options{Presets: repoconfig.Presets{Strict: strict}})
		found := false
		for _, finding := range result.Diagnostics {
			if finding.Rule != "unused-suppression" {
				continue
			}
			found = true
			severity := "info"
			if strict {
				severity = "error"
			}
			if finding.Severity != severity || !strings.HasSuffix(finding.Path, "action.yml") || finding.Reference == nil {
				t.Fatalf("strict=%v unused suppression lost severity or source identity: %+v", strict, finding)
			}
		}
		if !found {
			t.Fatalf("strict=%v: redundant fmt suppression was not reported: %+v", strict, result.Diagnostics)
		}
	}
}
