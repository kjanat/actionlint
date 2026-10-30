package expressions

import (
	"strings"
	"testing"

	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
)

func TestInterpolationSemanticErrorsRemainInvalid(t *testing.T) {
	for _, script := range []bool{false, true} {
		for _, source := range []string{
			"${{ unknown.value }} ${{ github.ref && '' || 'fallback' }}",
			"${{ github.ref && '' || 'fallback' }} ${{ unknown.value }}",
			"${{ unknown.first }} ${{ unknown.second }} ${{ github.ref && '' || 'fallback' }}",
			"${{ missingFunction('}}') }} ${{ github.ref && '' || 'fallback' }}",
		} {
			rule := newTestChecker()
			types, valid := rule.checkExprsIn(source, &workflowlang.Pos{Line: 1, Col: 1}, false, script, "env")
			if valid || len(types) != strings.Count(source, "${{") {
				t.Fatalf("must scan all parsed interpolations without treating their types as valid: %v, %+v", valid, types)
			}
			ternaries := 0
			for _, finding := range rule.Findings() {
				if finding.Rule == "unsound-ternary" {
					ternaries++
				}
			}
			if ternaries != 1 {
				t.Fatalf("later diagnostic missing: %+v", rule.Findings())
			}
			if ty, ok := rule.checkOneExpression(ownedStringFixture(&workflowlang.String{Value: source, Pos: &workflowlang.Pos{Line: 1, Col: 1}}), "value", "env"); ok {
				t.Fatalf("invalid interpolation types escaped into type checking: %v", ty)
			}
		}
	}
}

func TestInterpolationParseErrorsStillStopScanning(t *testing.T) {
	for _, script := range []bool{false, true} {
		for _, source := range []string{
			"${{ github.ref + }} ${{ github.ref && '' || 'fallback' }}",
			"${{ 'unterminated }} ${{ github.ref && '' || 'fallback' }}",
			"${{ unknown.value",
		} {
			rule := newTestChecker()
			types, valid := rule.checkExprsIn(source, &workflowlang.Pos{Line: 1, Col: 1}, false, script, "env")
			if valid || types != nil || len(rule.Findings()) == 0 {
				t.Fatalf("unparsed interpolation must remain invalid: %v, %+v, %+v", valid, types, rule.Findings())
			}
			for _, finding := range rule.Findings() {
				if finding.Rule != "expression-syntax" {
					t.Fatalf("must not resume from an unparsed expression boundary: %+v", finding)
				}
			}
		}
	}
}
