package expressions

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/exprtypes"
)

func TestPermissiveJSONInformation(t *testing.T) {
	t.Parallel()
	checker := newTestChecker()
	types, valid := checker.checkExprsIn("${{ fromJSON('[1,]') }}", &sourcetext.Position{Line: 3, Column: 5}, false, false, "env")
	if !valid || len(types) != 1 || types[0].ty.Kind() != exprtypes.KindArray {
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
