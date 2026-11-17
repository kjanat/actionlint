package assembly

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/checking/scriptanalysis/contract"
	"actionlint.kjanat.dev/v2/internal/checking/scriptanalysis/registry"
	"actionlint.kjanat.dev/v2/internal/input/checkrequest"
	"actionlint.kjanat.dev/v2/internal/input/configuration/configreport"
	"actionlint.kjanat.dev/v2/internal/integrations/hostprocess"
	"actionlint.kjanat.dev/v2/internal/model/scriptengine"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginselection"
	"actionlint.kjanat.dev/v2/internal/results/report"
)

type completedProvider struct{ calls int }

func (p *completedProvider) Check(context.Context, scriptengine.Request) ([]scriptengine.Diagnostic, error) {
	p.calls++
	return nil, nil
}

func TestProviderRemainsActiveWhenAnotherInvocationIsUnavailable(t *testing.T) {
	t.Parallel()
	for _, available := range []bool{false, true} {
		t.Run(map[bool]string{false: "all unavailable", true: "partly completed"}[available], func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			config := "{}"
			if available {
				config = "overrides: [{includes: ['inner/**'], tools: {ruff: {backend: {wasm: {path: engine.wasm}}}}}]"
			}
			for path, content := range map[string]string{
				".github/actionlint.yaml":  config,
				".github/workflows/ci.yml": "on: push\npermissions: {}\njobs: {check: {runs-on: ubuntu-latest, steps: [{shell: python, run: print(1)}, {uses: $/inner}]}}\n",
				".git/HEAD":                "ref: refs/heads/master\n",
				"inner/action.yml":         "name: inner\ndescription: test\nruns: {using: composite, steps: [{shell: python, run: print(1)}]}\n",
			} {
				path = filepath.Join(root, path)
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			tools := registry.Registrations(registry.ToolSelections{"ruff": {Command: "ruff", Process: &hostprocess.CommandOptions{
				Executable: &executable, Optional: true, LauncherArguments: []string{"-test.run=^$", "--"},
			}}})
			engine := &completedProvider{}
			result, err := checking.Check(t.Context(), checking.Options{
				WorkingDir: root, Tools: tools,
				LoadScriptEngine: func(context.Context, string) (scriptengine.Backend, error) { return engine, nil },
			}, checkrequest.Request{Kind: checkrequest.Files, Paths: []string{filepath.Join(root, ".github/workflows/ci.yml")}})
			if err != nil {
				t.Fatal(err)
			}
			statuses := providerStatuses(pluginselection.Sources{}, toolRegistrations("ruff"), result)
			status := statuses[0]
			wantInactive, wantCalls := inactiveNoCheck, 0
			if available {
				wantInactive, wantCalls = "", 1
			}
			if engine.calls != wantCalls || status.Inactive != wantInactive || len(status.Incomplete) != 1 || status.Incomplete[0] != "tool was unavailable for this analysis" {
				t.Fatalf("calls=%d, status=%+v; want calls=%d, inactive=%q and the unavailable-tool reason; diagnostics=%+v", engine.calls, status, wantCalls, wantInactive, result.Diagnostics)
			}
		})
	}
}

func toolRegistrations(names ...string) []contract.Registration {
	registrations := make([]contract.Registration, len(names))
	for i, name := range names {
		registrations[i].Name = name
	}
	return registrations
}

// A tool's status follows the documents it completed: a path override can
// enable it where the base configuration disables it, and one incomplete
// check does not hide the work it finished elsewhere.
func TestProviderStatusesFollowParticipation(t *testing.T) {
	t.Parallel()
	disabled := map[string]any{"tools": map[string]any{
		"ruff": map[string]any{"enabled": false}, "zizmor": map[string]any{"enabled": false},
	}}
	result := &report.Analysis{
		Configurations: []configreport.Report{{Inspection: configreport.Inspection{Config: disabled}}},
		CollectedByRule: []report.RuleCollection{
			{Rule: "ruff", Findings: 2, Completed: 1},
			{Rule: "shellcheck", Completed: 3},
			{Rule: "actionlint-tool", Completed: 0},
			{Rule: "zizmor", Completed: 0},
		},
		IncompleteChecks: []report.IncompleteCheck{
			{Rule: "shellcheck", Reason: "timed out"},
			{Rule: "shellcheck", Reason: "exited with status 3"},
			{Rule: "shellcheck", Reason: "timed out"},
			{Rule: "actionlint-tool", Reason: "executable not found"},
		},
	}
	got := providerStatuses(pluginselection.Sources{}, toolRegistrations("ruff", "shellcheck", "actionlint-tool", "zizmor"), result)
	want := []report.ProviderStatus{
		{Kind: report.ToolProvider, Name: "actionlint-tool", Inactive: inactiveNoCheck, Incomplete: []string{"executable not found"}},
		{Kind: report.ToolProvider, Name: "ruff"},
		{Kind: report.ToolProvider, Name: "shellcheck", Incomplete: []string{"exited with status 3", "timed out"}},
		{Kind: report.ToolProvider, Name: "zizmor", Inactive: inactiveDisabledConfig},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("statuses = %+v, want %+v", got, want)
	}
}
