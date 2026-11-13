package workflow_test

import (
	"fmt"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	testutil "actionlint.kjanat.dev/v2/internal/development/tests/support"
	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/results"
)

func TestScriptRuntimeRuleSelection(t *testing.T) {
	t.Parallel()
	for _, rule := range []struct {
		name, script      string
		column, endColumn int
	}{
		{"errexit-status", `false; status=$?`, 25, 27},
		{"workflow-command-syntax", `echo "::notice:Up to date"`, 16, 37},
	} {
		for _, tc := range []struct {
			name, level, comment, severity string
			strict                         bool
			experimental                   *bool
			preset                         string
		}{
			{name: "default"},
			{name: "none preset", preset: "none"},
			{name: "all stable preset", preset: "all"},
			{name: "recommended preset", preset: "recommended"},
			{name: "enabled", level: "on", severity: "warning"},
			{name: "warning", level: "warn", severity: "warning"},
			{name: "error", level: "error", severity: "error"},
			{name: "off", level: "off"},
			{name: "suppressed", level: "on", comment: " # ignore[" + rule.name + "]"},
			{name: "strict", strict: true},
			{name: "experimental", experimental: new(true), severity: "warning"},
			{name: "experimental explicit off", level: "off", experimental: new(true)},
		} {
			t.Run(rule.name+"/"+tc.name, func(t *testing.T) {
				t.Parallel()
				config := ""
				if tc.preset != "" {
					config = "lint: {rules: {preset: " + tc.preset + "}}"
				}
				if tc.level != "" {
					config = "lint: {rules: {preset: none, suspicious: {" + rule.name + ": " + tc.level + "}}}"
				}
				if tc.experimental != nil {
					config = "lint: {rules: {suspicious: {}}}"
					if tc.level == "off" {
						config = "lint: {rules: {suspicious: {" + rule.name + ": off}}}"
					}
				}
				workflow := fmt.Sprintf("on: push\npermissions: {}\njobs:\n  check:\n    runs-on: ubuntu-latest\n    steps:\n      - shell: bash\n        run: |%s\n          %s\n", tc.comment, rule.script)
				all := testutil.WorkflowDiagnosticsWithOptions(t, workflow, config, checking.Options{Presets: repoconfig.Presets{Strict: tc.strict, Experimental: tc.experimental}})
				var found []results.Diagnostic
				for _, d := range all {
					if d.Rule == rule.name || d.Rule == "unused-suppression" {
						found = append(found, d)
					}
				}
				if tc.severity == "" {
					if len(found) != 0 {
						t.Fatalf("unexpected findings: %+v", found)
					}
					return
				}
				if len(found) != 1 || found[0].Rule != rule.name || found[0].Severity != tc.severity || found[0].Start.Line != 9 || len(found[0].Fixes) != 0 {
					t.Fatalf("expected one %s with no automatic edits: %+v (all: %+v)", tc.severity, found, all)
				}
				if found[0].Start.Column != rule.column || found[0].End.Column != rule.endColumn {
					t.Fatalf("unexpected range: %+v", found[0])
				}
			})
		}
	}
}

func TestScriptRuntimeWorkflowShells(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, shell, script, rule string
		want                      int
	}{
		{"bash failure capture", "bash", `CHANGED="$(git diff --exit-code)"; EXIT_CODE=$?`, "errexit-status", 1},
		{"bash guarded", "bash", `CHANGED="$(git diff --exit-code)" || EXIT_CODE=$?`, "errexit-status", 0},
		{"bash disabled", "bash +e {0}", `false; status=$?`, "errexit-status", 0},
		{"unknown", "mystery {0}", `false; status=$?`, "errexit-status", 0},
		{"bash notice", "bash", `echo "::notice:Up to date"`, "workflow-command-syntax", 1},
		{"pwsh error", "pwsh", `Write-Output '::error:msg'`, "workflow-command-syntax", 1},
		{"python warning", "python", `print("::warning single-colon")`, "workflow-command-syntax", 1},
		{"python group", "python", `print("::group unterminated")`, "workflow-command-syntax", 1},
		{"pwsh unknown command", "pwsh", `echo '::notic::msg'`, "workflow-command-syntax", 1},
		{"pwsh type", "pwsh", `[System.IO.Path]::GetTempPath()`, "workflow-command-syntax", 0},
		{"python unused", "python", `example = "::notice:msg"`, "workflow-command-syntax", 0},
		{"bash dynamic", "bash", `echo "$message"`, "workflow-command-syntax", 0},
		{"bash noexec", "bash -n {0}", `echo '::notic::msg'`, "workflow-command-syntax", 0},
		{"bash one command", "bash -t {0}", `true; echo '::notic::msg'`, "workflow-command-syntax", 0},
		{"joined stdout annotation", "bash", `printf '::notice'; echo '::ok'`, "workflow-command-syntax", 0},
		{"joined stdout stop token", "bash", `printf '::stop-commands::'; echo token; echo '::notice: bad'`, "workflow-command-syntax", 0},
		{"joined stdout resume token", "bash", `echo '::stop-commands::token'; printf '::to'; echo 'ken::'; echo '::notice: bad'`, "workflow-command-syntax", 1},
		{"errexit output unreachable", "bash", `false; echo '::notice: bad'`, "workflow-command-syntax", 0},
		{"runtime errexit disabled", "bash", `set +e; false; echo '::notice: bad'`, "workflow-command-syntax", 1},
		{"invocation errexit disabled", "bash +e {0}", `false; echo '::notice: bad'`, "workflow-command-syntax", 1},
		{"ignored errexit group", "bash", `{ false; echo '::notice: bad'; } || :`, "workflow-command-syntax", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			workflow := fmt.Sprintf("on: push\npermissions: {}\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - shell: %s\n        run: |\n          %s\n", tc.shell, tc.script)
			config := "lint: {rules: {preset: none, suspicious: {" + tc.rule + ": on}}}"
			all := testutil.WorkflowDiagnostics(t, workflow, config)
			if len(all) != tc.want {
				t.Fatalf("got %+v, want %d", all, tc.want)
			}
		})
	}
}
