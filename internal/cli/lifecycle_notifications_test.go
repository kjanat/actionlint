package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/results/report"
)

func TestLifecycleNotificationStagesThroughCLI(t *testing.T) {
	t.Chdir(t.TempDir())
	const config = `[[lint.lifecycle.policies]]
id = "prepare-for-support-removal"
description = "Give teams time to migrate."
[lint.lifecycle.policies.match]
subjects = ["language-runtimes"]
events = ["support-ends"]
[[lint.lifecycle.policies.notifications]]
starts = { before = "180d" }
severity = "info"
[[lint.lifecycle.policies.notifications]]
starts = { before = "90d" }
severity = "warning"
[[lint.lifecycle.policies.notifications]]
starts = { on = "effective-date" }
severity = "error"
`
	if err := os.WriteFile("actionlint.toml", []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	const workflow = "on: push\npermissions: {}\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/setup-node@v6\n        with:\n          node-version: 26\n"
	for _, tc := range []struct{ date, flag, severity string }{
		{"2028-01-01", "--as-of", ""},
		{"2028-12-01", "--as-of", "info"},
		{"2029-02-01", "--as-of", "warning"},
		{"2029-04-30", "--as-of", "error"},
		{"2029-02-01", "--runner-as-of", "warning"},
	} {
		var out, log bytes.Buffer
		command := Command{Stdin: strings.NewReader(workflow), Stdout: &out, Stderr: &log}
		status := command.MainContext(t.Context(), []string{"actionlint", "check", "--config", "actionlint.toml", "--offline", "--no-tools", tc.flag, tc.date, "--json", "--summary=false", "-"})
		if status > 1 {
			t.Fatalf("%s %s: status %d: %s", tc.flag, tc.date, status, &log)
		}
		var report report.CheckResult
		if err := json.Unmarshal(out.Bytes(), &report); err != nil {
			t.Fatalf("%s: %v; output: %s", tc.date, err, &out)
		}
		found := 0
		for _, diagnostic := range report.Diagnostics {
			if diagnostic.Rule != "runtime-lifecycle" {
				continue
			}
			found++
			if string(diagnostic.Severity) != tc.severity || diagnostic.Start.Line != 9 || diagnostic.Lifecycle == nil || diagnostic.Lifecycle.AsOf != tc.date || len(diagnostic.Lifecycle.Policies) != 1 || diagnostic.Lifecycle.Policies[0].ID != "prepare-for-support-removal" {
				t.Errorf("%s %s: unexpected lifecycle diagnostic: %+v", tc.flag, tc.date, diagnostic)
			}
		}
		want := 1
		if tc.severity == "" {
			want = 0
		}
		if found != want {
			t.Errorf("%s: found %d runtime-lifecycle diagnostics, want %d: %s", tc.date, found, want, &out)
		}
	}
}
