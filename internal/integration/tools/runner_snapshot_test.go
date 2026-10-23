package tools_test

import (
	"context"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/testutil"
	"actionlint.kjanat.dev/v2/internal/tools/adapters"
	"actionlint.kjanat.dev/v2/internal/tools/backend"
	"actionlint.kjanat.dev/v2/internal/workspace"
	"actionlint.kjanat.dev/v2/rule"
	"actionlint.kjanat.dev/v2/runner"
)

type snapshotScriptBackend struct{ calls int }

func (b *snapshotScriptBackend) Check(context.Context, analyzer.ScriptRequest) ([]analyzer.ScriptDiagnostic, error) {
	b.calls++
	return []analyzer.ScriptDiagnostic{{Code: "PS001", Message: "PowerShell diagnostic", Location: analyzer.ScriptPosition{Row: 1, Column: 1}}}, nil
}

func TestRunnerSnapshotScriptRouting(t *testing.T) {
	for _, tc := range []struct {
		name, workflowDefaults, jobSettings, stepShell, selection string
		freshness                                                 runner.Freshness
		powershell, shellcheck, runnerFindings, shellFindings     int
	}{
		{name: "custom Windows runner", powershell: 1},
		{name: "group selector", selection: "{group: build, labels: desktop-pool}", powershell: 1},
		{name: "matrix selector", selection: "${{ matrix.runner }}", jobSettings: "    strategy:\n      matrix:\n        runner: [desktop-pool]\n", powershell: 1},
		{name: "step override", stepShell: "        shell: bash\n", shellcheck: 1},
		{name: "job override", jobSettings: "    defaults:\n      run:\n        shell: bash\n", shellcheck: 1},
		{name: "workflow override", workflowDefaults: "defaults:\n  run:\n    shell: bash\n", shellcheck: 1},
		{name: "container override", jobSettings: "    container: alpine:3\n", shellcheck: 1},
		{name: "shell validation uses supplied OS", stepShell: "        shell: sh\n", shellcheck: 1, shellFindings: 1},
		{name: "stale", freshness: runner.Stale},
		{name: "explicit shell with stale evidence", freshness: runner.Stale, stepShell: "        shell: pwsh\n", powershell: 1},
		{name: "unknown runner", selection: "absent", runnerFindings: 1},
		{name: "unknown runner explicit shell", selection: "absent", stepShell: "        shell: bash\n", shellcheck: 1, runnerFindings: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			engine := &snapshotScriptBackend{}
			registration, err := backend.Register(rule.Descriptor{Name: "powershell-test", Provider: "test"}, []string{"pwsh", "powershell"}, engine)
			if err != nil {
				t.Fatal(err)
			}
			var shellCalls int
			registrations := (adapters.Options{ShellcheckRunner: func([]string, string) ([]byte, error) {
				shellCalls++
				return []byte(`{"comments":[]}`), nil
			}}).Registrations()
			registrations = append(registrations, registration)
			freshness := tc.freshness
			if freshness == "" {
				freshness = runner.Fresh
			}
			selection := tc.selection
			if selection == "" {
				selection = "desktop-pool"
			}
			options := application.Options{Tools: registrations, Runners: runner.Options{Provider: "snapshot", Scope: "repo", Snapshot: runner.Snapshot{
				Scope: "repo", Freshness: freshness, Complete: true,
				Content: `{"runners":[{"name":"desktop","labels":["desktop-pool"],"group":"build","os":"windows","default_shell":"pwsh"}]}`,
			}}}
			content := "on: push\n" + tc.workflowDefaults + "jobs:\n  test:\n    runs-on: " + selection + "\n" + tc.jobSettings + "    steps:\n      - run: echo ok\n" + tc.stepShell
			findings, err := testutil.Diagnostics(application.Analyze(t.Context(), options, []workspace.Source{{Path: "test.yml", InputPath: "test.yml", Content: []byte(content)}}))
			if err != nil {
				t.Fatal(err)
			}
			var runnerFindings, psFindings, shellFindings int
			for _, finding := range findings {
				if finding.Rule == "runner-label" {
					runnerFindings++
				}
				if finding.Rule == "shell-name" {
					shellFindings++
				}
				if finding.Rule == "powershell-test" {
					psFindings++
					line := strings.Split(content, "\n")[finding.Start.Line-1]
					if !strings.Contains(line, "run: echo ok") {
						t.Fatalf("script diagnostic mapped to %q", line)
					}
				}
			}
			if engine.calls != tc.powershell || psFindings != tc.powershell || shellCalls != tc.shellcheck || runnerFindings != tc.runnerFindings {
				t.Fatalf("pwsh calls/findings=%d/%d shellcheck=%d runner findings=%d; want %d/%d/%d; diagnostics=%+v", engine.calls, psFindings, shellCalls, runnerFindings, tc.powershell, tc.shellcheck, tc.runnerFindings, findings)
			}
			if shellFindings != tc.shellFindings {
				t.Fatalf("shell findings=%d, want %d: %+v", shellFindings, tc.shellFindings, findings)
			}
		})
	}
}
