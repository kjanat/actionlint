package expressions

import (
	"testing"

	exprlang "actionlint.kjanat.dev/v2/internal/expression"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
)

func TestPermissiveJSONInformation(t *testing.T) {
	checker := newTestChecker()
	types, valid := checker.checkExprsIn("${{ fromJSON('[1,]') }}", &workflowlang.Pos{Line: 3, Col: 5}, false, false, "env")
	if !valid || len(types) != 1 || types[0].ty.Kind() != exprlang.KindArray {
		t.Fatalf("information changed expression validity or type: %v, %+v", valid, types)
	}
	findings := checker.Findings()
	if len(findings) != 1 {
		t.Fatalf("expected one information diagnostic: %+v", findings)
	}
	finding := findings[0]
	if finding.Severity != "info" || finding.Rule != "expression-call" || finding.Code != "permissive-json" || finding.Message != "you naughty boy, I see you" {
		t.Fatalf("unexpected diagnostic: %+v", finding)
	}
	if finding.Start.Line != 3 || finding.Start.Column != 18 {
		t.Fatalf("diagnostic missed the JSON argument: %+v", finding)
	}
}
