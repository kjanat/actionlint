package validation_test

import (
	"strings"
	"testing"

	actioncheck "actionlint.kjanat.dev/v2/internal/action/validation"
	"actionlint.kjanat.dev/v2/internal/actioncatalog"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
)

func TestDeprecatedPopularActionKeepsInputAndOutputMetadata(t *testing.T) {
	const spec = "actions/github-script@v7"
	meta := actioncatalog.PopularActions[spec]
	if meta == nil || meta.Runs.Using != "node20" || meta.Outputs["result"] == nil {
		t.Fatal("deprecated action lost its runtime or output metadata")
	}
	pos := &workflowlang.Pos{Line: 1, Col: 1}
	exec := &workflowlang.ExecAction{Uses: &workflowlang.String{Value: spec, Pos: pos}}
	findings := actioncheck.Repository(ownedActionFixture(exec), actioncatalog.Lookup, nil)
	var deprecated, missingScript bool
	for _, err := range findings {
		deprecated = deprecated || strings.Contains(err.Message, "deprecated")
		missingScript = missingScript || strings.Contains(err.Message, `missing input "script"`)
	}
	if !deprecated || !missingScript {
		t.Fatalf("deprecation disabled input validation: %v", findings)
	}
}
