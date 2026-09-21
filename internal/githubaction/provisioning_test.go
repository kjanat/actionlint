package githubaction

import (
	"encoding/json"
	"maps"
	"strings"
	"testing"
)

func TestToolPlanUsesEffectiveConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name       string
		files      map[string]string
		inputs     map[string]string
		shellcheck bool
		pyflakes   bool
	}{
		{name: "defaults", shellcheck: true, pyflakes: true},
		{name: "yaml", files: map[string]string{".github/actionlint.yaml": "tools:\n  shellcheck: false\n"}, pyflakes: true},
		{name: "yml", files: map[string]string{".github/actionlint.yml": "tools:\n  shellcheck:\n    enabled: false\n"}, pyflakes: true},
		{name: "yaml precedence", files: map[string]string{
			".github/actionlint.yaml": "tools: {shellcheck: false}",
			".github/actionlint.yml":  "tools: {shellcheck: true}",
		}, pyflakes: true},
		{name: "whole config overlay", inputs: map[string]string{"INPUT_CONFIG": "tools: {shellcheck: false}"}, pyflakes: true},
		{name: "section overlay", inputs: map[string]string{
			"INPUT_CONFIG": "tools: {shellcheck: true}",
			"INPUT_TOOLS":  "shellcheck: {enabled: false}",
		}, pyflakes: true},
		{name: "reset overlay", files: map[string]string{".github/actionlint.yaml": "tools: {shellcheck: false}"},
			inputs: map[string]string{"INPUT_TOOLS": "null"}, shellcheck: true, pyflakes: true},
		{name: "explicit config and working directory", files: map[string]string{
			".github/actionlint.yaml": "tools: {shellcheck: true}",
			"sub/custom.yml":          "tools: {shellcheck: false}",
		}, inputs: map[string]string{"INPUT_WORKING-DIRECTORY": "sub", "INPUT_CONFIG-FILE": "custom.yml"}, pyflakes: true},
		{name: "selected project", files: map[string]string{
			"child/.git": "", "child/.github/actionlint.yaml": "tools: {shellcheck: false}",
			"child/.github/workflows/clean.yml": cleanWorkflow,
		}, inputs: map[string]string{"INPUT_FILES": "child/.github/workflows/missing.yml"}, pyflakes: true},
		{name: "multiple projects", files: map[string]string{
			"child/.git": "", "child/.github/actionlint.yaml": "tools: {shellcheck: false}",
			"child/.github/workflows/clean.yml": cleanWorkflow,
		}, inputs: map[string]string{"INPUT_FILES": "child/.github/workflows/missing.yml\n.github/workflows/clean.yml"}, shellcheck: true, pyflakes: true},
		{name: "action input disables tools", inputs: map[string]string{
			"INPUT_SHELLCHECK": "false", "INPUT_PYFLAKES": "false", "INPUT_TOOLS": "shellcheck: true",
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := map[string]string{".git": "", ".github/workflows/clean.yml": cleanWorkflow}
			maps.Copy(files, tc.files)
			env := map[string]string{"GITHUB_WORKSPACE": workspaceWith(t, files)}
			maps.Copy(env, tc.inputs)
			var stdout, stderr strings.Builder
			if code := ToolPlan(func(key string) string { return env[key] }, &stdout, &stderr); code != 0 {
				t.Fatalf("preflight failed with %d: %s", code, &stderr)
			}
			var got struct {
				SchemaVersion int  `json:"schema_version"`
				Shellcheck    bool `json:"shellcheck"`
				Pyflakes      bool `json:"pyflakes"`
			}
			if err := json.Unmarshal([]byte(stdout.String()), &got); err != nil {
				t.Fatal(err)
			}
			if got.SchemaVersion != 1 || got.Shellcheck != tc.shellcheck || got.Pyflakes != tc.pyflakes {
				t.Fatalf("got %+v; want shellcheck=%v pyflakes=%v", got, tc.shellcheck, tc.pyflakes)
			}
			if stderr.Len() != 0 {
				t.Fatalf("unexpected stderr: %s", &stderr)
			}
		})
	}
}

func TestToolPlanRejectsInvalidConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name   string
		config string
		inputs map[string]string
		code   int
	}{
		{name: "input", inputs: map[string]string{"INPUT_SHELLCHECK": "yes"}, code: 2},
		{name: "overlay", inputs: map[string]string{"INPUT_TOOLS": "shellcheck: {enabled: wrong}"}, code: 2},
		{name: "file", config: "tools: {shellcheck: {enabled: wrong}}", code: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := map[string]string{"GITHUB_WORKSPACE": workspaceWith(t, map[string]string{
				".git": "", ".github/actionlint.yaml": tc.config,
				".github/workflows/clean.yml": cleanWorkflow,
			})}
			maps.Copy(env, tc.inputs)
			var stdout, stderr strings.Builder
			if code := ToolPlan(func(key string) string { return env[key] }, &stdout, &stderr); code != tc.code {
				t.Fatalf("got status %d; want %d: %s", code, tc.code, &stderr)
			}
			if stdout.Len() != 0 || stderr.Len() == 0 {
				t.Fatalf("got stdout=%q stderr=%q", stdout.String(), stderr.String())
			}
		})
	}
}
