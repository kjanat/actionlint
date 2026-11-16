package suspicious

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/evaluate"

	"actionlint.kjanat.dev/v2/internal/model/contexts"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/typecheck"

	exprtypes "actionlint.kjanat.dev/v2/internal/languages/expressions/types"
)

func TestConditionValuesUseCheckedTypes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ source, rule string }{
		{"env.FLAG", "string-conditions"},
		{"github.event.issue.title", "string-conditions"},
		{"github.ref == 1", "mixed-type-comparisons"},
		{"true == github.ref", "mixed-type-comparisons"},
		{"github.ref == '1'", ""},
		{"fromJSON(env.FLAG)", ""},
		{"contains(github.ref, 'main')", ""},
	} {
		t.Run(tc.source, func(t *testing.T) {
			t.Parallel()
			node, err, _ := syntax.ParseConditionExpression(tc.source)
			if err != nil {
				t.Fatal(err)
			}
			checker := typecheck.NewChecker(contexts.DefaultExpressionScope(), nil)
			checker.SetAvailability(contexts.WorkflowExpressionScope("jobs.<job_id>.steps.if"))
			checked := checker.CheckResult(evaluate.EvaluateSubexpressions(node))
			if !checked.Valid {
				t.Fatal(checked.Findings)
			}
			if len(checked.Findings) != 0 {
				t.Fatalf("type checker emitted policy findings: %v", checked.Findings)
			}
			for _, enabled := range []bool{false, true} {
				var findings []*typecheck.Error
				if enabled {
					findings = append(StringConditions(node, checked.Types), MixedTypeComparisons(node, checked.Types)...)
				}
				want := 0
				if enabled && tc.rule != "" {
					want = 1
				}
				if len(findings) != want {
					t.Fatalf("enabled=%t got %v, want %d findings", enabled, findings, want)
				}
				if want == 1 {
					finding := findings[0]
					if finding.Rule != tc.rule || !finding.Advisory || finding.Offset != 0 || finding.EndOffset != len(tc.source) {
						t.Fatalf("wrong diagnostic: %+v", finding)
					}
				}
			}
			if findings := append(StringConditions(node, typecheck.InferredTypes{}), MixedTypeComparisons(node, typecheck.InferredTypes{})...); len(findings) != 0 {
				t.Fatalf("unchecked syntax acquired policy findings: %v", findings)
			}
			if checked.Type.Kind() != exprtypes.KindString && tc.rule == "string-conditions" && tc.source == "env.FLAG" {
				t.Fatal("string type lost")
			}
		})
	}
}
