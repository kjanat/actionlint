package action_test

import (
	"fmt"
	"strings"
	"testing"

	actioncheck "actionlint.kjanat.dev/v2/internal/checking/rules/correctness/action"
	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/model/actionmeta"
	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

func TestDeprecatedRuntimeKeepsInputValidation(t *testing.T) {
	t.Parallel()
	const spec = "example/script@v1"
	meta, err := actionmeta.ParseActionMetadata("action.yml", []byte("name: script\ndescription: script\ninputs:\n  script: {required: true}\noutputs:\n  result: {description: result}\nruns: {using: node20, main: index.js}\n"))
	if err != nil {
		t.Fatal(err)
	}
	pos := &sourcepos.Position{Line: 1, Column: 1}
	exec := &workflowlang.ExecAction{Uses: &workflowlang.String{Value: spec, Pos: pos}}
	findings := actioncheck.Repository(ownedActionFixture(exec), actionmeta.NewDefinition(meta), actionmeta.LifecycleContext{})
	var deprecated, missingScript bool
	for _, err := range findings {
		deprecated = deprecated || strings.Contains(err.Message, "deprecated")
		missingScript = missingScript || strings.Contains(err.Message, `missing input "script"`)
	}
	if !deprecated || !missingScript {
		t.Fatalf("deprecation disabled input validation: %v", findings)
	}
}

func TestReferencedActionRuntimeCodeAndMessage(t *testing.T) {
	t.Parallel()
	const spec = "example/script@v1"
	for _, tc := range []struct {
		using   string
		code    actionmeta.ProblemCode
		message string
	}{
		{"node20", actionmeta.CodeRuntimeDeprecated, `use "node24" instead`},
		{"NODE20", actionmeta.CodeRuntimeDeprecated, `use "node24" instead`},
		{"node16", actionmeta.CodeRuntimeDeprecated, `use "node24" instead`},
		{"node24", "", ""},
		{"node999", "", ""},
	} {
		t.Run(tc.using, func(t *testing.T) {
			t.Parallel()
			source := fmt.Sprintf("name: script\ndescription: script\nruns: {using: %s, main: index.js}\n", tc.using)
			meta, err := actionmeta.ParseActionMetadata("action.yml", []byte(source))
			if err != nil {
				t.Fatal(err)
			}
			exec := &workflowlang.ExecAction{Uses: &workflowlang.String{Value: spec, Pos: &sourcepos.Position{Line: 1, Column: 1}}}
			findings := actioncheck.Repository(ownedActionFixture(exec), actionmeta.NewDefinition(meta), actionmeta.LifecycleContext{})
			if tc.code == "" {
				if len(findings) != 0 {
					t.Fatalf("unexpected lifecycle findings: %+v", findings)
				}
				return
			}
			if len(findings) != 1 || findings[0].Code != string(tc.code) || !strings.Contains(findings[0].Message, tc.message) {
				t.Fatalf("wanted code %q and message containing %q, got %+v", tc.code, tc.message, findings)
			}
		})
	}
}
