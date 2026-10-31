package validation_test

import (
	"strings"
	"testing"

	actionmeta "actionlint.kjanat.dev/v2/internal/action"
	actioncheck "actionlint.kjanat.dev/v2/internal/action/validation"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
)

func TestDeprecatedRuntimeKeepsInputValidation(t *testing.T) {
	const spec = "example/script@v1"
	meta, err := actionmeta.Parse("action.yml", []byte("name: script\ndescription: script\ninputs:\n  script: {required: true}\noutputs:\n  result: {description: result}\nruns: {using: node20, main: index.js}\n"))
	if err != nil {
		t.Fatal(err)
	}
	pos := &workflowlang.Pos{Line: 1, Col: 1}
	exec := &workflowlang.ExecAction{Uses: &workflowlang.String{Value: spec, Pos: pos}}
	findings := actioncheck.Repository(ownedActionFixture(exec), actionmeta.NewDefinition(meta))
	var deprecated, missingScript bool
	for _, err := range findings {
		deprecated = deprecated || strings.Contains(err.Message, "deprecated")
		missingScript = missingScript || strings.Contains(err.Message, `missing input "script"`)
	}
	if !deprecated || !missingScript {
		t.Fatalf("deprecation disabled input validation: %v", findings)
	}
}
