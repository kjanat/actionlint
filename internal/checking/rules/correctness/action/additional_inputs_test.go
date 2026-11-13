package action_test

import (
	"strings"
	"testing"

	actioncheck "actionlint.kjanat.dev/v2/internal/checking/rules/correctness/action"
	actionmeta "actionlint.kjanat.dev/v2/internal/model/actions"
	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"
	workflowparser "actionlint.kjanat.dev/v2/internal/model/workflows/parser"
)

func TestAdditionalInputsRetainDeclaredInputValidation(t *testing.T) {
	t.Parallel()
	metadata := actionmeta.ParseDefinition([]byte("name: request\ndescription: request\ninputs:\n  route: {required: true}\n  old: {deprecationMessage: use route}\noutputs:\n  result: {description: result}\nruns: {using: node24, main: index.js}\n"))
	additional := metadata.WithAdditionalInputs()
	if !additional.Present() || metadata.AllowsAdditionalInputs() || !additional.AllowsAdditionalInputs() || additional.OutputsComplete() {
		t.Fatal("additional inputs changed original definition or output contract")
	}
	for _, test := range []struct{ name, inputs, want string }{
		{"dynamic parameters", "{route: 'POST /repos/{owner}/{repo}/check-runs', owner: octokit, repo: request-action, name: check, head_sha: abc, output: body}", ""},
		{"missing route", "{owner: octokit, repo: request-action, body: payload}", `missing input "route"`},
		{"deprecated declaration", "{route: GET /, old: value, body: payload}", `avoid using deprecated input "old"`},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			text := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: octokit/request-action@v3.x\n        with: " + test.inputs + "\n"
			parsed, errors := workflowparser.Parse([]byte(text))
			if len(errors) != 0 {
				t.Fatal(errors)
			}
			view := workflowdecl.Publish(text, parsed, false, "").Bind("additional-inputs").Workflow()
			job, exists := view.Job("test")
			if !exists {
				t.Fatal("fixture job is missing")
			}
			for step := range job.Steps() {
				exec, ok := step.Action()
				if !ok {
					t.Fatal("fixture step is not an action")
				}
				findings := actioncheck.Repository(exec, additional)
				if test.want == "" && len(findings) != 0 || test.want != "" && (len(findings) != 1 || !strings.Contains(findings[0].Message, test.want)) {
					t.Fatalf("want %q, findings=%v", test.want, findings)
				}
				if test.want == "" && len(actioncheck.Repository(exec, metadata)) == 0 {
					t.Fatal("ordinary declared-input contract accepted unknown parameters")
				}
			}
		})
	}
}
