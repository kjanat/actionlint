package workflow_test

import (
	"os"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	testutil "actionlint.kjanat.dev/v2/internal/development/tests/support"
	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"
	runner "actionlint.kjanat.dev/v2/internal/model/runners/contract"
)

// GitHub requires owner/repo[/path]@ref syntax for remote uses references.
// https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax#jobsjob_idstepsuses
func TestVendorLockIn(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, uses   string
		count, fixes int
	}{
		{"foreign host", "https://codeberg.org/mlugg/setup-zig@v2", 1, 0},
		{"GitHub URL", "https://github.com/mlugg/setup-zig@v2", 1, 1},
		{"canonical", "mlugg/setup-zig@v2", 0, 0},
		{"Docker registry", "docker://codeberg.org/team/image:v2", 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			source := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: " + tc.uses + "\n"
			findings := testutil.WorkflowDiagnostics(t, source, "tools: {shellcheck: false, ruff: false}\nlint: {rules: {preset: none, correctness: {vendor-lock-in: on}}}")
			if len(findings) != tc.count {
				t.Fatalf("findings=%+v, want %d", findings, tc.count)
			}
			if tc.count != 0 && (findings[0].Rule != "vendor-lock-in" || findings[0].Severity != "error" || len(findings[0].Fixes) != tc.fixes) {
				t.Fatalf("unexpected finding: %+v", findings[0])
			}
		})
	}
}

func TestVendorLockInSelection(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, config, severity, comment, target string
		strict                                  bool
	}{
		{"default", "", "error", "", "", false},
		{"warn", "lint: {rules: {correctness: {vendor-lock-in: warn}}}", "warning", "", "", false},
		{"off", "lint: {rules: {correctness: {vendor-lock-in: off}}}", "", "", "", false},
		{"strict", "", "error", "", "", true},
		{"suppressed", "", "", " # ignore[vendor-lock-in]", "", false},
		{"other target", "", "", "", "buildkite", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// A reusable-only workflow does not request any runners. Its target
			// must still be known without consulting a runner provider.
			source := "on: push\njobs:\n  call:\n    uses: https://github.com/example/repo/.github/workflows/build.yml@v1" + tc.comment + "\n"
			findings := testutil.WorkflowDiagnosticsWithOptions(t, source, "tools: {shellcheck: false, ruff: false}\n"+tc.config, checking.Options{Runners: runner.Options{Target: tc.target}, Presets: repoconfig.Presets{Strict: tc.strict}})
			count := 0
			for _, finding := range findings {
				if finding.Rule == "action" || finding.Rule == "workflow-call" {
					t.Fatalf("duplicate generic format error: %+v", finding)
				}
				if finding.Rule == "vendor-lock-in" {
					count++
					if finding.Severity != tc.severity || finding.Start.Line != 4 {
						t.Fatalf("wrong level or location: %+v", finding)
					}
				}
			}
			want := 0
			if tc.severity != "" {
				want = 1
			}
			if count != want {
				t.Fatalf("findings=%+v, want %d", findings, want)
			}
		})
	}
}

func TestVendorLockInRecordedWorkflow(t *testing.T) {
	t.Parallel()
	// The fixture header records the source repository and exact commit.
	content, err := os.ReadFile("internal/development/tests/integration/workflow/testdata/repositories/actions-rs.yml")
	if err != nil {
		t.Fatal(err)
	}
	original := string(content)
	modified := strings.Replace(original, "uses: actions/checkout@", "uses: https://github.com/actions/checkout@", 1)
	if modified == original {
		t.Fatal("recorded checkout reference missing")
	}
	for _, tc := range []struct {
		source string
		want   int
	}{{original, 0}, {modified, 1}} {
		findings := testutil.WorkflowDiagnostics(t, tc.source, "tools: {shellcheck: false, ruff: false}\nlint: {rules: {preset: none, correctness: {vendor-lock-in: on}}}")
		if len(findings) != tc.want {
			t.Fatalf("findings=%+v, want %d", findings, tc.want)
		}
	}
}
