package correctness

import (
	"strings"
	"testing"

	expreval "actionlint.kjanat.dev/v2/internal/languages/expressions/evaluate"

	contextspec "actionlint.kjanat.dev/v2/internal/model/contexts"

	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"

	exprlang "actionlint.kjanat.dev/v2/internal/languages/expressions/typecheck"
)

func TestUnusedFormatArgumentsUseCheckedCall(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		source   string
		unused   []string
		failures int
	}{
		{"format('format {0} {1} {2}', 'foo', 1, true, null)", []string{"{3}"}, 0},
		{"format('format {0} {2}', 1, 2, 3)", []string{"{1}"}, 0},
		{"format('format {0} {2}', 1, 2)", []string{"{1}"}, 1},
		{"Format('{0}', 1, 2)", []string{"{1}"}, 0},
		{"format('hello {{0}}', 'world')", []string{"{0}"}, 0},
		{"format('hello {{{{0}}', 'world')", []string{"{0}"}, 0},
		{"format('{0}{1}', 1, 2)", nil, 0},
		{"format(github.event.unknown, 1)", nil, 0},
		{"format()", nil, 2},
		{"true || format('constant', 1)", nil, 0},
		{"case(false, format('constant', 1), 'fallback')", nil, 0},
		{"github.event.unknown && format('constant', 1)", []string{"{0}"}, 0},
	} {
		t.Run(tc.source, func(t *testing.T) {
			t.Parallel()
			node, err := exprsyntax.NewParser().Parse(exprsyntax.NewLexer(tc.source + "}}"))
			if err != nil {
				t.Fatal(err)
			}
			checker := exprlang.NewChecker(contextspec.DefaultExpressionScope(), nil)
			checker.SetAvailability(contextspec.WorkflowExpressionScope("jobs.<job_id>.steps.if"))
			result := checker.CheckResult(expreval.EvaluateSubexpressions(node))
			if result.Valid != (tc.failures == 0) || len(result.Findings) != tc.failures {
				t.Fatalf("language findings=%v, want %d", result.Findings, tc.failures)
			}
			findings := UnusedFormatArguments(node, result.Calls)
			if len(findings) != len(tc.unused) {
				t.Fatalf("findings=%v, want unused %v", findings, tc.unused)
			}
			for index, finding := range findings {
				if finding.Rule != "expression-call" || finding.Code != "format-placeholder" || !finding.Advisory || !strings.Contains(finding.Message, "placeholder "+tc.unused[index]) {
					t.Fatalf("wrong finding: %+v", finding)
				}
				span := tc.source[finding.Offset:finding.EndOffset]
				if !strings.EqualFold(span, "format") {
					t.Fatalf("range=%q, want callee", span)
				}
			}
		})
	}
}

func TestUnusedFormatArgumentRangeExcludesResultAccess(t *testing.T) {
	t.Parallel()
	const source = `format('{0}{1}', 1, 2, 3)[0]`
	root, err := exprsyntax.NewParser().Parse(exprsyntax.NewLexer(source + "}}"))
	if err != nil {
		t.Fatal(err)
	}
	checked := exprlang.NewChecker(contextspec.DefaultExpressionScope(), nil).CheckResult(expreval.EvaluateSubexpressions(root))
	findings := UnusedFormatArguments(root, checked.Calls)
	if len(findings) != 1 || source[findings[0].Offset:findings[0].EndOffset] != "format" {
		t.Fatalf("wrong callee range: %+v", findings)
	}
	if findings := UnusedFormatArguments(root, exprlang.CheckedCalls{}); len(findings) != 0 {
		t.Fatalf("unchecked syntax acquired a finding: %+v", findings)
	}
}

func TestGroupedUnusedFormatArgumentRanges(t *testing.T) {
	t.Parallel()
	for _, suffix := range []string{"", ".x", "[0]", "['x'].y"} {
		source := "((format('{0}', 1, 2)))" + suffix
		root, err := exprsyntax.NewParser().Parse(exprsyntax.NewLexer(source + "}}"))
		if err != nil {
			t.Fatal(err)
		}
		checked := exprlang.NewChecker(contextspec.DefaultExpressionScope(), nil).CheckResult(expreval.EvaluateSubexpressions(root))
		findings := UnusedFormatArguments(root, checked.Calls)
		if len(findings) != 1 || source[findings[0].Offset:findings[0].EndOffset] != "format" {
			t.Fatalf("%s: wrong range %+v", source, findings)
		}
	}
}
