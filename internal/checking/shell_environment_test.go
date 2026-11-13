package checking_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	"actionlint.kjanat.dev/v2/internal/checking/scripts/registry"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
	runnerapi "actionlint.kjanat.dev/v2/internal/model/runners/contract"
)

func TestShellcheckEnvironmentDeclarations(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("shellcheck"); err != nil {
		t.Skip("ShellCheck is required for the native environment comparison")
	}
	for _, tc := range []struct {
		name, runner, script string
		want                 []string
	}{
		{"Linux names", "ubuntu-latest", `printf '%s\n' "$lowercase" "$UPPERCASE" "$miXedCase" "$FOO"`, nil},
		{"Windows Bash names", "windows-latest", `printf '%s\n' "$lowercase" "$UPPERCASE" "$miXedCase" "$LOWERCASE"`, nil},
		{"unknown inherited name", "ubuntu-latest", `printf '%s\n' "$lowercase" "$missing_lowercase"`, nil},
		{"unknown case variant", "ubuntu-latest", `printf '%s\n' "$MIXEDCASE"`, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			content := "on: push\nenv: {lowercase: lower, UPPERCASE: upper, miXedCase: mixed, foo: outer}\njobs:\n  test:\n    runs-on: " + tc.runner + "\n    env: {FOO: inner, LOWERCASE: override}\n    steps:\n      - shell: bash\n        run: |\n          " + tc.script + "\n"
			report, err := checking.Analyze(t.Context(), checking.Options{Tools: registry.Registrations(registry.Selections{"shellcheck": {Command: "shellcheck"}})}, []workspace.Source{{Path: "environment.yml", Content: []byte(content)}})
			if err != nil {
				t.Fatal(err)
			}
			var codes []string
			for _, finding := range report.Diagnostics {
				if finding.Rule == "shellcheck" {
					codes = append(codes, finding.Code)
					if finding.Start.Line != 10 {
						t.Errorf("environment prelude changed the source line: %+v", finding)
					}
				}
			}
			if !slices.Equal(codes, tc.want) {
				t.Fatalf("ShellCheck codes = %v, want %v; findings=%+v", codes, tc.want, report.Diagnostics)
			}
		})
	}
}

func TestScriptEngineReceivesUnknownRunnerEnvironment(t *testing.T) {
	t.Parallel()
	engine := &recordingEngine{code: "SC2086"}
	content := "on: push\npermissions: {}\nenv: {WORKFLOW_NAME: workflow}\njobs:\n  test:\n    runs-on: ubuntu-24.04\n    env: {JOB_NAME: job}\n    steps:\n      - shell: bash\n        env: {STEP_NAME: step}\n        run: echo missing\n"
	_, err := checking.Analyze(t.Context(), checking.Options{Tools: registry.Registrations(registry.Selections{"shellcheck": {Backend: engine}})}, []workspace.Source{{Path: "environment.yml", Content: []byte(content)}})
	if err != nil {
		t.Fatal(err)
	}
	if len(engine.requests) != 1 || len(engine.requests[0].Environment) != 1 {
		t.Fatalf("expected one script and environment: %+v", engine.requests)
	}
	env := engine.requests[0].Environment[0]
	if env.OperatingSystem != runnerapi.Linux || !env.UnknownNames {
		t.Fatalf("expected Linux with unknown inherited names: %+v", env)
	}
	for _, want := range []string{"WORKFLOW_NAME", "JOB_NAME", "STEP_NAME"} {
		if !slices.ContainsFunc(env.Names, func(name checkinputs.EnvironmentVariableName) bool { return name.Name == want && name.CaseKnown }) {
			t.Errorf("missing known declaration %q: %+v", want, env.Names)
		}
	}
}

func TestShellcheckInheritedRunnerEnvironment(t *testing.T) {
	t.Parallel()
	command, err := exec.LookPath("shellcheck")
	if err != nil {
		t.Skip("ShellCheck is required for the native environment comparison")
	}
	configFile := filepath.Join(t.TempDir(), "actionlint.yaml")
	if err := os.WriteFile(configFile, []byte("tools:\n  shellcheck:\n    config: {enable: [all]}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Runner processes inherit environment variables in addition to workflow declarations.
	// https://github.com/actions/runner/blob/main/src/Runner.Sdk/ProcessInvoker.cs
	for _, tc := range []struct {
		name, runner, previousStep, variable string
	}{
		{"hosted image", "ubuntu-24.04", "", "ANDROID_HOME"},
		{"dynamic runner", "${{ matrix.runner }}", "", "ANDROID_HOME"},
		{"self-hosted runner", "self-hosted", "", "PRIVATE_RUNNER_VALUE"},
		{"custom runner", "custom-pool", "", "CUSTOM_POOL_VALUE"},
		{"earlier GITHUB_ENV step", "ubuntu-24.04", "      - run: echo 'FROM_PREVIOUS_STEP=value' >> \"$GITHUB_ENV\"\n", "FROM_PREVIOUS_STEP"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			content := "on: push\npermissions: {}\njobs:\n"
			if tc.name == "dynamic runner" {
				content += "  plan:\n    runs-on: ubuntu-24.04\n    timeout-minutes: 5\n    outputs:\n      matrix: ${{ steps.plan.outputs.matrix }}\n    steps:\n      - id: plan\n        run: echo 'matrix={\"include\":[{\"runner\":\"ubuntu-24.04\"}]}' >> \"$GITHUB_OUTPUT\"\n"
			}
			content += "  test:\n"
			if tc.name == "dynamic runner" {
				content += "    needs: plan\n    strategy:\n      matrix: ${{ fromJSON(needs.plan.outputs.matrix) }}\n"
			}
			content += "    runs-on: " + tc.runner + "\n    timeout-minutes: 5\n    steps:\n" + tc.previousStep + "      - shell: bash\n        run: |\n          printf '%s\\n' \"$" + tc.variable + "\"\n          echo $UNQUOTED\n"
			report, err := checking.Analyze(t.Context(), checking.Options{ConfigFile: configFile, Tools: registry.Registrations(registry.Selections{"shellcheck": {Command: command}})}, []workspace.Source{{Path: "environment.yml", Content: []byte(content)}})
			if err != nil {
				t.Fatal(err)
			}
			before, _, _ := strings.Cut(content, "          echo $UNQUOTED")
			wantLine := strings.Count(before, "\n") + 1
			foundQuoting := false
			for _, finding := range report.Diagnostics {
				if finding.Rule != "shellcheck" {
					t.Errorf("unexpected workflow diagnostic: %+v", finding)
					continue
				}
				if finding.Code == "SC2154" {
					t.Errorf("inherited environment reported as absent: %+v", finding)
				}
				if finding.Code == "SC2086" {
					foundQuoting = true
					if finding.Start.Line != wantLine || finding.Start.Column != 16 || finding.End.Line != wantLine || finding.End.Column != 25 {
						t.Errorf("wrong SC2086 position: %+v", finding)
					}
				}
			}
			if !foundQuoting {
				t.Fatal("missing unrelated SC2086 diagnostic")
			}
		})
	}
}
