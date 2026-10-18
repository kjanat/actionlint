package expressions_test

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/internal/application"
	repoconfig "actionlint.kjanat.dev/internal/config"
	"actionlint.kjanat.dev/internal/workspace"
)

func TestScriptInterpolationWorkflowProofs(t *testing.T) {
	for _, tc := range []struct {
		name, matrix, env, expression, want string
	}{
		{name: "unknown input", expression: "inputs.text", want: "script-interpolation"},
		{name: "secret", expression: "secrets.TOKEN", want: "script-injection"},
		{name: "boolean", expression: "inputs.flag"},
		{name: "boolean branches", expression: "inputs.flag && 'release' || 'debug'"},
		{name: "safe matrix", matrix: "target: [x86_64-linux, aarch64-linux]", expression: "matrix.target"},
		{name: "unsafe matrix alternative", matrix: `target: [safe, "bad'quote"]`, expression: "matrix.target", want: "script-interpolation"},
		{name: "unsafe include", matrix: "target: [safe]\n        include: [{target: \"bad'quote\"}]", expression: "matrix.target", want: "script-interpolation"},
		{name: "unknown include", matrix: "target: [safe]\n        include: ${{ fromJSON(inputs.text) }}", expression: "matrix.target", want: "script-interpolation"},
		{name: "known step env", env: "VALUE: safe", expression: "env.VALUE"},
		{name: "unsafe step env", env: `VALUE: "bad'quote"`, expression: "env.VALUE", want: "script-interpolation"},
		{name: "unknown step env", env: "VALUE: ${{ inputs.text }}", expression: "env.VALUE", want: "script-interpolation"},
		{name: "known url", expression: "github.repositoryUrl"},
		{name: "unknown ref", expression: "github.ref_name", want: "script-interpolation"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			content := "on: {workflow_dispatch: {inputs: {text: {type: string}, flag: {type: boolean}}}}\njobs:\n  test:\n    runs-on: ubuntu-latest\n"
			if tc.matrix != "" {
				content += "    strategy:\n      matrix:\n        " + tc.matrix + "\n"
			}
			content += "    steps:\n      - run: |\n          printf '%s\\n' '${{ " + tc.expression + " }}'\n"
			if tc.env != "" {
				content += "        env:\n          " + tc.env + "\n"
			}
			cfg, err := repoconfig.Parse([]byte("tools: {shellcheck: false, ruff: false}"))
			if err != nil {
				t.Fatal(err)
			}
			result, err := application.Analyze(t.Context(), application.Options{}, []workspace.Source{{Path: "workflow.yml", Content: []byte(content), Config: cfg}})
			if err != nil {
				t.Fatal(err)
			}
			if tc.want == "" {
				if len(result.Diagnostics) != 0 {
					t.Fatal(result.Diagnostics)
				}
			} else if len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != tc.want || !strings.Contains(result.Diagnostics[0].Message, "environment variable") {
				t.Fatalf("want one %s; got %v", tc.want, result.Diagnostics)
			}
		})
	}
}
