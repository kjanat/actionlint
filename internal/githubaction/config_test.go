package githubaction

import (
	"maps"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"actionlint.kjanat.dev"
	"go.yaml.in/yaml/v4"
)

func TestMainConfigurationInputs(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  map[string]string
		code int
		text string
	}{
		{"yaml discovery wins over yml", nil, 1, ".github/actionlint.yaml (automatically discovered)"},
		{"inline yaml", map[string]string{"INPUT_CONFIG": "self-hosted-runner: {labels: [unknown-runner]}"}, 0, "overrides: config"},
		{"inline json", map[string]string{"INPUT_CONFIG": `{"self-hosted-runner":{"labels":["unknown-runner"]}}`}, 0, "overrides: config"},

		{"explicit config file", map[string]string{"INPUT_CONFIG-FILE": ".github/actionlint.yml"}, 0, ".github/actionlint.yml (config-file input)"},
		{"explicit file with cleared labels", map[string]string{"INPUT_CONFIG-FILE": ".github/actionlint.yml", "INPUT_CONFIG": "self-hosted-runner: {labels: []}"}, 1, "overrides: config"},
		{"blank inherits", map[string]string{"INPUT_CONFIG": " \n"}, 1, ".github/actionlint.yaml (automatically discovered)"},
		{"unknown key", map[string]string{"INPUT_CONFIG": "config-variable: [TYPO]"}, 2, "Invalid action input"},
		{"invalid section", map[string]string{"INPUT_CONFIG": "self-hosted-runner: [unknown-runner]"}, 2, "input config"},
		{"conflicting merged bounds", map[string]string{"INPUT_CONFIG": "policy: {require-job-timeout: {min-minutes: 20, max-minutes: 10}}"}, 2, "input config"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workspace := workspaceWith(t, map[string]string{
				".git": "", ".github/workflows/test.yaml": brokenWorkflow,
				".github/actionlint.yaml": "self-hosted-runner: {labels: [yaml-label]}",
				".github/actionlint.yml":  "self-hosted-runner: {labels: [unknown-runner]}",
			})
			output := filepath.Join(t.TempDir(), "outputs")
			env := map[string]string{"GITHUB_WORKSPACE": workspace, "GITHUB_OUTPUT": output, "INPUT_SHELLCHECK": "false", "INPUT_PYFLAKES": "false"}
			maps.Copy(env, tc.env)
			var out strings.Builder
			if code := Main(func(key string) string { return env[key] }, &out); code != tc.code {
				t.Fatalf("want code %d, got %d: %s", tc.code, code, out.String())
			}
			if !strings.Contains(out.String(), tc.text) {
				t.Errorf("want %q in %s", tc.text, out.String())
			}
			if tc.code < 2 {
				outputs := parseOutputs(read(t, output))
				if outputs["exit-code"] != strconv.Itoa(tc.code) {
					t.Errorf("lint exit code must remain exposed: %#v", outputs)
				}
			}
		})
	}
}

func TestQuotedIgnoreHintPreservesDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		pattern string
		want    bool
	}{
		{`'label "ubuntu-24\.04-custom" is unknown\.'`, true},
		{`"label "ubuntu-24\.04-custom" is unknown\."`, true},
		{`label "ubuntu-24\.04-custom" is unknown\.`, false},
		{`'different message'`, false},
		{`'('`, false},
	} {
		errs := []actionlint.Diagnostic{{Message: `label "ubuntu-24.04-custom" is unknown.`}}
		hints := quotedIgnoreHints([]string{tc.pattern}, errs)
		if (len(hints) == 1) != tc.want {
			t.Errorf("%q: hints=%v", tc.pattern, hints)
		}
	}
}

func TestToolCommandsFromEnvironment(t *testing.T) {
	python := `C:\tool cache\someone's python\python.exe`
	script := `C:\tool cache\$pyflakes\launcher.py`
	req := &lintRequest{shellcheck: "shellcheck", pyflakes: "pyflakes"}
	env := map[string]string{"ACTIONLINT_SHELLCHECK_COMMAND": "/cache/shellcheck", "ACTIONLINT_PYTHON": python, "ACTIONLINT_PYFLAKES_SCRIPT": script}
	if err := req.configureEnvironment(func(k string) string { return env[k] }); err != nil {
		t.Fatal(err)
	}
	if req.pyflakesOptions == nil || req.pyflakesOptions.Executable == nil || *req.pyflakesOptions.Executable != python || !reflect.DeepEqual(req.pyflakesOptions.Arguments, []string{"-I", script}) {
		t.Errorf("want literal Python executable and arguments, got %#v", req.pyflakesOptions)
	}
	if req.shellcheckOptions == nil || req.shellcheckOptions.Executable == nil || *req.shellcheckOptions.Executable != "/cache/shellcheck" {
		t.Errorf("want provisioned shellcheck, got %#v", req.shellcheckOptions)
	}
	disabled := &lintRequest{}
	if err := disabled.configureEnvironment(func(k string) string { return env[k] }); err != nil || disabled.shellcheck != "" || disabled.pyflakes != "" || disabled.shellcheckOptions != nil || disabled.pyflakesOptions != nil {
		t.Errorf("disabled tools must stay disabled: %#v, %v", disabled, err)
	}
}

func TestActionMetadataHasOneConfigOverlay(t *testing.T) {
	var metadata struct {
		Inputs map[string]struct {
			Default string `yaml:"default"`
		} `yaml:"inputs"`
	}
	if err := yaml.Unmarshal([]byte(read(t, filepath.Join("..", "..", "action.yml"))), &metadata); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"config"} {
		input, ok := metadata.Inputs[key]
		if !ok || input.Default != "" {
			t.Errorf("%s must have a blank input default so file settings are inherited", key)
		}
	}
}

func TestActionMetadataOmitsSectionMirrors(t *testing.T) {
	var metadata struct {
		Inputs map[string]any `yaml:"inputs"`
	}
	if err := yaml.Unmarshal([]byte(read(t, filepath.Join("..", "..", "action.yml"))), &metadata); err != nil {
		t.Fatal(err)
	}
	for _, key := range actionlint.ConfigKeys() {
		if _, exists := metadata.Inputs[key]; exists {
			t.Errorf("config section %s must use config overlay", key)
		}
	}
}

func TestAnnotationsDisabledPreserveOutput(t *testing.T) {
	workspace := workspaceWith(t, map[string]string{".git": "", ".github/workflows/test.yaml": brokenWorkflow})
	output := filepath.Join(t.TempDir(), "outputs")
	env := map[string]string{"GITHUB_WORKSPACE": workspace, "GITHUB_OUTPUT": output, "INPUT_SHELLCHECK": "false", "INPUT_PYFLAKES": "false", "INPUT_ANNOTATIONS": "false"}
	var out strings.Builder
	if code := Main(func(key string) string { return env[key] }, &out); code != 1 {
		t.Fatalf("exit %d: %s", code, &out)
	}
	if strings.Contains(out.String(), "::error file=") {
		t.Fatalf("annotations not disabled: %s", &out)
	}
	if value := parseOutputs(read(t, output))["output"]; !strings.Contains(value, "::error file=") {
		t.Fatalf("legacy output changed: %s", value)
	}
}

func TestConfigWarningsVisibleInAction(t *testing.T) {
	workspace := workspaceWith(t, map[string]string{".git": "", ".github/workflows/test.yaml": cleanWorkflow, ".github/actionlint.yaml": "config-variable: [TYPO]\n"})
	env := map[string]string{"GITHUB_WORKSPACE": workspace, "INPUT_SHELLCHECK": "false", "INPUT_PYFLAKES": "false"}
	var out strings.Builder
	if code := Main(func(key string) string { return env[key] }, &out); code != 0 {
		t.Fatalf("exit %d: %s", code, &out)
	}
	if !strings.Contains(out.String(), "::warning title=Check configuration::") || !strings.Contains(out.String(), "config-variable") {
		t.Fatal(out.String())
	}
}
