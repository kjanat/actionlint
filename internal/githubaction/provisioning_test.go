package githubaction

import (
	"context"
	"maps"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/localapp"
	"actionlint.kjanat.dev/v2/internal/tools/adapters"
)

func TestActionNativeBackendRequirements(t *testing.T) {
	for _, tc := range []struct {
		name       string
		files      map[string]string
		inputs     map[string]string
		shellcheck bool
	}{
		{name: "defaults", shellcheck: true},
		{name: "discovered includes", files: map[string]string{".github/actionlint.yaml": "files: {includes: ['.github/workflows/*.yml']}"}, shellcheck: true},
		{name: "discovered excludes", files: map[string]string{".github/actionlint.yaml": "files: {excludes: ['.github/workflows/*.yml']}"}},
		{name: "discovered override", files: map[string]string{".github/actionlint.yaml": "overrides: [{includes: ['**/*.yml'], lint: {rules: {external: {shellcheck: off}}}}]"}},
		{name: "yaml", files: map[string]string{".github/actionlint.yaml": `tools:
  shellcheck: false
`}},
		{name: "yml", files: map[string]string{".github/actionlint.yml": `tools:
  shellcheck:
    enabled: false
`}},
		{name: "yaml precedence", files: map[string]string{
			".github/actionlint.yaml": "tools: {shellcheck: false}",
			".github/actionlint.yml":  "tools: {shellcheck: true}",
		}},
		{name: "whole config overlay", inputs: map[string]string{"INPUT_CONFIG": "tools: {shellcheck: false}"}},
		{name: "expanded config overlay", inputs: map[string]string{
			"INPUT_CONFIG": "tools: {shellcheck: {enabled: false}}",
		}},
		{name: "reset overlay", files: map[string]string{".github/actionlint.yaml": "tools: {shellcheck: false}"},
			inputs: map[string]string{"INPUT_CONFIG": "tools: null"}, shellcheck: true},
		{name: "explicit config and working directory", files: map[string]string{
			".github/actionlint.yaml": "tools: {shellcheck: true}",
			"sub/custom.yml":          "tools: {shellcheck: false}",
		}, inputs: map[string]string{"INPUT_WORKING-DIRECTORY": "sub", "INPUT_CONFIG-FILE": "custom.yml"}},
		{name: "selected project", files: map[string]string{
			"child/.git": "", "child/.github/actionlint.yaml": "tools: {shellcheck: false}",
			"child/.github/workflows/clean.yml": cleanWorkflow,
		}, inputs: map[string]string{"INPUT_FILES": "child/.github/workflows/missing.yml"}},
		{name: "multiple projects", files: map[string]string{
			"child/.git": "", "child/.github/actionlint.yaml": "tools: {shellcheck: false}",
			"child/.github/workflows/clean.yml": cleanWorkflow,
		}, inputs: map[string]string{"INPUT_FILES": "child/.github/workflows/missing.yml\n.github/workflows/clean.yml"}, shellcheck: true},
		{name: "action input disables tools", inputs: map[string]string{
			"INPUT_SHELLCHECK": "false", "INPUT_PYFLAKES": "false", "INPUT_CONFIG": "tools: {shellcheck: true}",
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := map[string]string{".git": "", ".github/workflows/clean.yml": cleanWorkflow}
			maps.Copy(files, tc.files)
			env := map[string]string{"GITHUB_WORKSPACE": workspaceWith(t, files)}
			maps.Copy(env, tc.inputs)
			req, err := provisioningRequest(func(key string) string { return env[key] })
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			selected, err := req.pluginSelection().Resolve(ctx, req.workingDir, req.pluginInput())
			if err != nil {
				t.Fatal(err)
			}
			needed, err := localapp.RequiredToolsSources(ctx, application.Options{
				WorkingDir: req.workingDir, ConfigFile: req.configFile, ConfigOverlays: req.overlays,
				IgnorePatterns: req.ignore, Tools: adapters.Options{ShellCheck: req.shellcheck, Ruff: req.ruff}.Registrations(),
			}, req.files, selected)
			if err != nil || needed["shellcheck"] != tc.shellcheck {
				t.Fatalf("requirements=%v error=%v; want shellcheck=%v", needed, err, tc.shellcheck)
			}
		})
	}
}

func TestPluginPlanRejectsInvalidConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name   string
		config string
		inputs map[string]string
		code   int
	}{
		{name: "input", inputs: map[string]string{"INPUT_SHELLCHECK": "yes"}, code: 2},
		{name: "overlay", inputs: map[string]string{"INPUT_CONFIG": "tools: {shellcheck: {enabled: wrong}}"}, code: 2},
		{name: "file", config: "tools: {shellcheck: {enabled: wrong}}", code: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := map[string]string{"GITHUB_WORKSPACE": workspaceWith(t, map[string]string{
				".git": "", ".github/actionlint.yaml": tc.config,
				".github/workflows/clean.yml": cleanWorkflow,
			})}
			maps.Copy(env, tc.inputs)
			var stdout, stderr strings.Builder
			if code := PluginPlan(func(key string) string { return env[key] }, &stdout, &stderr); code != tc.code {
				t.Fatalf("got status %d; want %d: %s", code, tc.code, &stderr)
			}
			if stdout.Len() != 0 || stderr.Len() == 0 {
				t.Fatalf("got stdout=%q stderr=%q", stdout.String(), stderr.String())
			}
		})
	}
}
