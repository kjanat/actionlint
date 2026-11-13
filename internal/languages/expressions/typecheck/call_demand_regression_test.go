package typecheck

import (
	"strings"
	"testing"

	runnerreference "actionlint.kjanat.dev/v2/internal/development/tests/conformance/expressions"

	contextspec "actionlint.kjanat.dev/v2/internal/model/contexts"

	expreval "actionlint.kjanat.dev/v2/internal/languages/expressions/evaluate"

	exprfunctions "actionlint.kjanat.dev/v2/internal/languages/expressions/functions"

	exprvalues "actionlint.kjanat.dev/v2/internal/languages/expressions/values"

	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"

	exprtypes "actionlint.kjanat.dev/v2/internal/languages/expressions/types"
	"github.com/google/go-cmp/cmp"
)

// These paths follow Runny's next engine, pinned through ACTIONLINT_REFERENCE_IMAGE.
// Parsing always visits nested calls; evaluation visits only demanded arguments.
func TestCallEvaluationDemand(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		input   string
		valid   bool
		state   exprvalues.EvaluationState
		value   any
		demands []exprfunctions.ArgumentDemand
		failure runnerreference.FailurePhase
		error   string
	}{
		{"format(true, fromJSON(null))", true, exprvalues.EvaluationKnown, "true", []exprfunctions.ArgumentDemand{exprfunctions.DemandRequired, exprfunctions.DemandSkipped}, runnerreference.Success, ""},
		{"format('{1}', fromJSON(null), true)", true, exprvalues.EvaluationKnown, "true", []exprfunctions.ArgumentDemand{exprfunctions.DemandRequired, exprfunctions.DemandSkipped, exprfunctions.DemandRequired}, runnerreference.Success, ""},
		{"format('{0}', fromJSON(null))", false, exprvalues.EvaluationInvalid, nil, []exprfunctions.ArgumentDemand{exprfunctions.DemandRequired, exprfunctions.DemandRequired}, runnerreference.RuntimeFailure, "System.Text.Json.JsonException"},
		{"format('{1}', true)", false, exprvalues.EvaluationInvalid, nil, []exprfunctions.ArgumentDemand{exprfunctions.DemandRequired, exprfunctions.DemandSkipped}, runnerreference.RuntimeFailure, "System.FormatException"},
		{"format('{+0}', fromJSON(null))", false, exprvalues.EvaluationInvalid, nil, []exprfunctions.ArgumentDemand{exprfunctions.DemandRequired, exprfunctions.DemandSkipped}, runnerreference.RuntimeFailure, "System.FormatException"},
		{"format('{0}{1}', fromJSON(null))", false, exprvalues.EvaluationInvalid, nil, []exprfunctions.ArgumentDemand{exprfunctions.DemandRequired, exprfunctions.DemandSkipped}, runnerreference.RuntimeFailure, "System.FormatException"},
		{"format('{0:N2}', fromJSON(null))", false, exprvalues.EvaluationInvalid, nil, []exprfunctions.ArgumentDemand{exprfunctions.DemandRequired, exprfunctions.DemandRequired}, runnerreference.RuntimeFailure, "System.Text.Json.JsonException"},
		{"format('{0:x}{1}', true, fromJSON(null))", false, exprvalues.EvaluationInvalid, nil, []exprfunctions.ArgumentDemand{exprfunctions.DemandRequired, exprfunctions.DemandRequired, exprfunctions.DemandSkipped}, runnerreference.RuntimeFailure, "System.FormatException"},
		{"format('plain', startsWith(true))", false, exprvalues.EvaluationNotAttempted, nil, []exprfunctions.ArgumentDemand{exprfunctions.DemandRequired, exprfunctions.DemandSkipped}, runnerreference.ParseFailure, "GitHub.Actions.Expressions.ParseException"},
		{"format('plain', noSuchFunction())", false, exprvalues.EvaluationNotAttempted, nil, []exprfunctions.ArgumentDemand{exprfunctions.DemandRequired, exprfunctions.DemandSkipped}, runnerreference.ParseFailure, "GitHub.Actions.Expressions.ParseException"},
		{"case(1, 'a', 'b')", false, exprvalues.EvaluationInvalid, nil, []exprfunctions.ArgumentDemand{exprfunctions.DemandRequired, exprfunctions.DemandSkipped, exprfunctions.DemandSkipped}, runnerreference.RuntimeFailure, "System.InvalidOperationException"},
		{"case(false || 1, 'a', 'b')", false, exprvalues.EvaluationInvalid, nil, []exprfunctions.ArgumentDemand{exprfunctions.DemandRequired, exprfunctions.DemandSkipped, exprfunctions.DemandSkipped}, runnerreference.RuntimeFailure, "System.InvalidOperationException"},
		{"case(true && 'text', 'a', 'b')", false, exprvalues.EvaluationInvalid, nil, []exprfunctions.ArgumentDemand{exprfunctions.DemandRequired, exprfunctions.DemandSkipped, exprfunctions.DemandSkipped}, runnerreference.RuntimeFailure, "System.InvalidOperationException"},
		{"case(false || fromJSON('{}'), 'a', 'b')", false, exprvalues.EvaluationInvalid, nil, []exprfunctions.ArgumentDemand{exprfunctions.DemandRequired, exprfunctions.DemandSkipped, exprfunctions.DemandSkipped}, runnerreference.RuntimeFailure, "System.InvalidOperationException"},
		{"case(true, 'a', 'b')", true, exprvalues.EvaluationKnown, "a", []exprfunctions.ArgumentDemand{exprfunctions.DemandRequired, exprfunctions.DemandRequired, exprfunctions.DemandSkipped}, runnerreference.Success, ""},
		{"case(false, fromJSON(null), 'b')", true, exprvalues.EvaluationKnown, "b", []exprfunctions.ArgumentDemand{exprfunctions.DemandRequired, exprfunctions.DemandSkipped, exprfunctions.DemandRequired}, runnerreference.Success, ""},
		{"case(true, 'a', 1, fromJSON(null), 'c')", true, exprvalues.EvaluationKnown, "a", []exprfunctions.ArgumentDemand{exprfunctions.DemandRequired, exprfunctions.DemandRequired, exprfunctions.DemandSkipped, exprfunctions.DemandSkipped, exprfunctions.DemandSkipped}, runnerreference.Success, ""},
		{"case(true, 'a', false || 1, fromJSON(null), 'c')", true, exprvalues.EvaluationKnown, "a", []exprfunctions.ArgumentDemand{exprfunctions.DemandRequired, exprfunctions.DemandRequired, exprfunctions.DemandSkipped, exprfunctions.DemandSkipped, exprfunctions.DemandSkipped}, runnerreference.Success, ""},
		{"startsWith(fromJSON('{}'), fromJSON(null))", true, exprvalues.EvaluationKnown, false, []exprfunctions.ArgumentDemand{exprfunctions.DemandRequired, exprfunctions.DemandSkipped}, runnerreference.Success, ""},
		{"startsWith(fromJSON(null), fromJSON(null))", false, exprvalues.EvaluationInvalid, nil, []exprfunctions.ArgumentDemand{exprfunctions.DemandRequired, exprfunctions.DemandSkipped}, runnerreference.RuntimeFailure, "System.Text.Json.JsonException"},
		{"contains(fromJSON('[]'), fromJSON(null))", true, exprvalues.EvaluationKnown, false, []exprfunctions.ArgumentDemand{exprfunctions.DemandRequired, exprfunctions.DemandSkipped}, runnerreference.Success, ""},
		{"join(fromJSON('[true]'), fromJSON(null))", true, exprvalues.EvaluationKnown, "true", []exprfunctions.ArgumentDemand{exprfunctions.DemandRequired, exprfunctions.DemandSkipped}, runnerreference.Success, ""},
		{"join(fromJSON('[true,false]'), fromJSON(null))", false, exprvalues.EvaluationInvalid, nil, []exprfunctions.ArgumentDemand{exprfunctions.DemandRequired, exprfunctions.DemandRequired}, runnerreference.RuntimeFailure, "System.Text.Json.JsonException"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			got := checkCallDemandResult(t, tc.input)
			var wantPhase FailurePhase
			switch tc.failure {
			case runnerreference.Success:
				wantPhase = FailureNone
			case runnerreference.ParseFailure:
				wantPhase = FailureStaticValidation
			case runnerreference.RuntimeFailure:
				wantPhase = FailureEvaluation
			}
			if got.FailurePhase != wantPhase {
				t.Errorf("failure phase=%v, want %v", got.FailurePhase, wantPhase)
			}
			if got.Valid != tc.valid || got.Evaluation.State != tc.state || !cmp.Equal(got.Evaluation.Value, tc.value) {
				t.Fatalf("valid=%v evaluation=%+v findings=%v; want valid=%v state=%v value=%v", got.Valid, got.Evaluation, got.Findings, tc.valid, tc.state, tc.value)
			}
			checkCallArgumentDemands(t, got, tc.demands)
			if tc.failure != runnerreference.ParseFailure {
				node := exprsyntax.ParseAssignedExpression("${{ " + tc.input + " }}")
				evaluated := expreval.Evaluate(node, nil)
				if evaluated.State != tc.state || !cmp.Equal(evaluated.Value, tc.value) {
					t.Fatalf("standalone evaluation=%+v, want state=%v value=%v", evaluated, tc.state, tc.value)
				}
			}
			runnerreference.CheckOutcome(t, tc.input, nil, runnerreference.Expectation{Failure: tc.failure, Value: tc.value, ExceptionType: tc.error})
		})
	}
}

func TestRuntimeFailurePropagationAgainstRunner(t *testing.T) {
	t.Parallel()
	for _, input := range []string{
		"!fromJSON(null)",
		"fromJSON(null) || true",
		"false || fromJSON(null)",
		"true && fromJSON(null)",
		"fromJSON(null) == false",
		"false == fromJSON(null)",
		"fromJSON(null).value",
		"fromJSON(null)[0]",
		"fromJSON('[1]')[fromJSON(null)]",
	} {
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			node := exprsyntax.ParseAssignedExpression("${{ " + input + " }}")
			if node == nil {
				t.Fatal("expression did not parse")
			}
			if result := expreval.Evaluate(node, nil); result.State != exprvalues.EvaluationInvalid {
				t.Fatalf("evaluation=%+v, want runtime failure", result)
			}
			runnerreference.CheckOutcome(t, input, nil, runnerreference.Expectation{Failure: runnerreference.RuntimeFailure, ExceptionType: "System.Text.Json.JsonException"})
		})
	}
}

func TestCallConditionalDemand(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		input   string
		demands []exprfunctions.ArgumentDemand
	}{
		{"format(github.event.unknown, fromJSON(null))", []exprfunctions.ArgumentDemand{exprfunctions.DemandRequired, exprfunctions.DemandConditional}},
		{"startsWith(github.event.unknown, fromJSON(null))", []exprfunctions.ArgumentDemand{exprfunctions.DemandRequired, exprfunctions.DemandConditional}},
		{"contains(github.event.unknown, fromJSON(null))", []exprfunctions.ArgumentDemand{exprfunctions.DemandRequired, exprfunctions.DemandConditional}},
		{"join(github.event.unknown, fromJSON(null))", []exprfunctions.ArgumentDemand{exprfunctions.DemandRequired, exprfunctions.DemandConditional}},
		{"case(github.event.unknown, 'a', fromJSON(null))", []exprfunctions.ArgumentDemand{exprfunctions.DemandRequired, exprfunctions.DemandConditional, exprfunctions.DemandConditional}},
		{"case(github.event.unknown, 'a', true && 'text', 'b', 'c')", []exprfunctions.ArgumentDemand{exprfunctions.DemandRequired, exprfunctions.DemandConditional, exprfunctions.DemandConditional, exprfunctions.DemandSkipped, exprfunctions.DemandSkipped}},
	} {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			got := checkCallDemandResult(t, tc.input)
			if !got.Valid || got.Evaluation.State != exprvalues.EvaluationUnknown || got.FailurePhase != FailureNone {
				t.Fatalf("conditional failure became certain: %+v", got)
			}
			checkCallArgumentDemands(t, got, tc.demands)
		})
	}
	for _, tc := range []struct {
		input, name string
		context     any
		want        runnerreference.Expectation
	}{
		{"format(github.event.unknown, fromJSON(null))", "unused", "plain", runnerreference.Expectation{Value: "plain"}},
		{"format(github.event.unknown, fromJSON(null))", "used", "{0}", runnerreference.Expectation{Failure: runnerreference.RuntimeFailure, ExceptionType: "System.Text.Json.JsonException"}},
		{"startsWith(github.event.unknown, fromJSON(null))", "object", map[string]any{}, runnerreference.Expectation{Value: false}},
		{"startsWith(github.event.unknown, fromJSON(null))", "primitive", "x", runnerreference.Expectation{Failure: runnerreference.RuntimeFailure, ExceptionType: "System.Text.Json.JsonException"}},
		{"case(github.event.unknown, 'a', fromJSON(null))", "selected", true, runnerreference.Expectation{Value: "a"}},
		{"case(github.event.unknown, 'a', fromJSON(null))", "default", false, runnerreference.Expectation{Failure: runnerreference.RuntimeFailure, ExceptionType: "System.Text.Json.JsonException"}},
		{"case(github.event.unknown, 'a', fromJSON(null))", "invalid predicate", float64(1), runnerreference.Expectation{Failure: runnerreference.RuntimeFailure, ExceptionType: "System.InvalidOperationException"}},
	} {
		t.Run(tc.input+"/"+tc.name, func(t *testing.T) {
			t.Parallel()
			variables := map[string]any{"github": map[string]any{"event": map[string]any{"unknown": tc.context}}}
			runnerreference.CheckOutcome(t, tc.input, variables, tc.want)
		})
	}
}

func TestFormatTemplateGrammar(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		input string
		value string
		valid bool
	}{
		{"format('{0:}', 'x')", "x", true},
		{"format('{{{0}}}', 'x')", "{x}", true},
		{"format('{+0}', 'x')", "", false},
		{"format('{0:x}', 'x')", "", false},
		{"format('{0:N2}', 1)", "", false},
		{"format('{0:}}}', 'x')", "", false},
		{"format('{0:foo}}bar}', 'x')", "", false},
		{"format('{0', 'x')", "", false},
		{"format('x}', 'x')", "", false},
		{"format('{0} {} {x} {', 1)", "", false},
	} {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			got := checkCallDemandResult(t, tc.input)
			want := runnerreference.Expectation{Value: tc.value}
			if tc.valid {
				if !got.Valid || got.Evaluation.State != exprvalues.EvaluationKnown || got.Evaluation.Value != tc.value || got.FailurePhase != FailureNone {
					t.Fatalf("valid format did not evaluate to %q: %+v", tc.value, got)
				}
			} else {
				if got.Valid || got.Evaluation.State != exprvalues.EvaluationInvalid || got.FailurePhase != FailureEvaluation {
					t.Fatalf("invalid format was not rejected: %+v", got)
				}
				want = runnerreference.Expectation{Failure: runnerreference.RuntimeFailure, ExceptionType: "System.FormatException"}
			}
			runnerreference.CheckOutcome(t, tc.input, nil, want)
		})
	}
}

func TestLogicalBranchEvaluationDemand(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		input string
		state exprvalues.EvaluationState
		phase FailurePhase
		value any
	}{
		{"github.event.unknown || fromJSON(null)", exprvalues.EvaluationUnknown, FailureNone, nil},
		{"github.event.unknown && fromJSON(null)", exprvalues.EvaluationUnknown, FailureNone, nil},
		{"!(github.event.unknown || fromJSON(null))", exprvalues.EvaluationUnknown, FailureNone, nil},
		{"github.event.unknown && (true || fromJSON(null))", exprvalues.EvaluationUnknown, FailureNone, nil},
		{"true || fromJSON(null)", exprvalues.EvaluationKnown, FailureNone, true},
		{"false && fromJSON(null)", exprvalues.EvaluationKnown, FailureNone, false},
		{"false || fromJSON(null)", exprvalues.EvaluationInvalid, FailureEvaluation, nil},
		{"true && fromJSON(null)", exprvalues.EvaluationInvalid, FailureEvaluation, nil},
		{"true || startsWith(true)", exprvalues.EvaluationNotAttempted, FailureStaticValidation, nil},
		{"false && noSuchFunction()", exprvalues.EvaluationNotAttempted, FailureStaticValidation, nil},
	} {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			got := checkCallDemandResult(t, tc.input)
			if got.Valid != (tc.phase == FailureNone) || got.Evaluation.State != tc.state || got.FailurePhase != tc.phase || !cmp.Equal(got.Evaluation.Value, tc.value) {
				t.Fatalf("logical branch result: %+v; want state=%v phase=%v value=%v", got, tc.state, tc.phase, tc.value)
			}
			if tc.state == exprvalues.EvaluationUnknown {
				return
			}
			var want runnerreference.Expectation
			switch tc.phase {
			case FailureNone:
				want = runnerreference.Expectation{Value: tc.value}
			case FailureStaticValidation:
				want = runnerreference.Expectation{Failure: runnerreference.ParseFailure, ExceptionType: "GitHub.Actions.Expressions.ParseException"}
			case FailureEvaluation:
				want = runnerreference.Expectation{Failure: runnerreference.RuntimeFailure, ExceptionType: "System.Text.Json.JsonException"}
			}
			runnerreference.CheckOutcome(t, tc.input, nil, want)
		})
	}
	for _, tc := range []struct {
		input string
		left  bool
		want  runnerreference.Expectation
	}{
		{"github.event.unknown || fromJSON(null)", true, runnerreference.Expectation{Value: true}},
		{"github.event.unknown || fromJSON(null)", false, runnerreference.Expectation{Failure: runnerreference.RuntimeFailure, ExceptionType: "System.Text.Json.JsonException"}},
		{"github.event.unknown && fromJSON(null)", false, runnerreference.Expectation{Value: false}},
		{"github.event.unknown && fromJSON(null)", true, runnerreference.Expectation{Failure: runnerreference.RuntimeFailure, ExceptionType: "System.Text.Json.JsonException"}},
		{"!(github.event.unknown || fromJSON(null))", true, runnerreference.Expectation{Value: false}},
		{"!(github.event.unknown || fromJSON(null))", false, runnerreference.Expectation{Failure: runnerreference.RuntimeFailure, ExceptionType: "System.Text.Json.JsonException"}},
	} {
		name := tc.input + "/false"
		if tc.left {
			name = tc.input + "/true"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			variables := map[string]any{"github": map[string]any{"event": map[string]any{"unknown": tc.left}}}
			runnerreference.CheckOutcome(t, tc.input, variables, tc.want)
		})
	}
}

func TestFormatReplacementExecutionOrder(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		input         string
		demands       []exprfunctions.ArgumentDemand
		code          string
		exceptionType string
	}{
		{"format('{1}{0}', fromJSON(null), case(1, 'a', 'b'))", []exprfunctions.ArgumentDemand{exprfunctions.DemandRequired, exprfunctions.DemandSkipped, exprfunctions.DemandRequired}, "operand-type", "System.InvalidOperationException"},
		{"format('{0}{1}', case(1, 'a', 'b'), fromJSON(null))", []exprfunctions.ArgumentDemand{exprfunctions.DemandRequired, exprfunctions.DemandRequired, exprfunctions.DemandSkipped}, "operand-type", "System.InvalidOperationException"},
		{"format('{1}{0}',fromJSON(null),fromJSON('bad'))", []exprfunctions.ArgumentDemand{exprfunctions.DemandRequired, exprfunctions.DemandSkipped, exprfunctions.DemandRequired}, "invalid-json", "System.Text.Json.JsonException"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			got := checkCallDemandResult(t, tc.input)
			if got.Valid || got.Evaluation.State != exprvalues.EvaluationInvalid || got.FailurePhase != FailureEvaluation {
				t.Fatalf("missing first replacement failure: %+v", got)
			}
			checkCallArgumentDemands(t, got, tc.demands)
			if len(got.Findings) != 1 || got.Findings[0].Code != tc.code {
				t.Errorf("expected only the first replacement's %q finding: %v", tc.code, got.Findings)
			}
			runnerreference.CheckOutcome(t, tc.input, nil, runnerreference.Expectation{Failure: runnerreference.RuntimeFailure, ExceptionType: tc.exceptionType})
		})
	}
}

func TestFormatUnknownReplacementOrder(t *testing.T) {
	t.Parallel()
	const input = "format('{1}{0}',fromJSON(null),github.event.unknown)"
	got := checkCallDemandResult(t, input)
	if got.Valid || got.Evaluation.State != exprvalues.EvaluationInvalid || got.FailurePhase != FailureEvaluation {
		t.Fatalf("the required JSON failure after the unknown replacement was lost: %+v", got)
	}
	checkCallArgumentDemands(t, got, []exprfunctions.ArgumentDemand{exprfunctions.DemandRequired, exprfunctions.DemandRequired, exprfunctions.DemandRequired})
	if len(got.Findings) != 1 || got.Findings[0].Code != "invalid-json" {
		t.Fatalf("expected the required replacement's JSON finding: %v", got.Findings)
	}
	for _, left := range []bool{false, true} {
		name := "false"
		if left {
			name = "true"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			variables := map[string]any{"github": map[string]any{"event": map[string]any{"unknown": left}}}
			runnerreference.CheckOutcome(t, input, variables, runnerreference.Expectation{Failure: runnerreference.RuntimeFailure, ExceptionType: "System.Text.Json.JsonException"})
		})
	}
}

func TestNestedConditionalCallDemand(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		input   string
		demands []exprfunctions.ArgumentDemand
		value   string
	}{
		{"case(github.event.unknown || fromJSON(null),'a','b')", []exprfunctions.ArgumentDemand{exprfunctions.DemandRequired, exprfunctions.DemandConditional, exprfunctions.DemandConditional}, "a"},
		{"format('{0}',github.event.unknown || fromJSON(null))", []exprfunctions.ArgumentDemand{exprfunctions.DemandRequired, exprfunctions.DemandRequired}, "true"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			got := checkCallDemandResult(t, tc.input)
			if !got.Valid || got.Evaluation.State != exprvalues.EvaluationUnknown || got.FailurePhase != FailureNone || len(got.Findings) != 0 {
				t.Fatalf("nested conditional JSON failure became certain: %+v", got)
			}
			checkCallArgumentDemands(t, got, tc.demands)
			for _, left := range []bool{false, true} {
				name := "false"
				want := runnerreference.Expectation{Failure: runnerreference.RuntimeFailure, ExceptionType: "System.Text.Json.JsonException"}
				if left {
					name = "true"
					want = runnerreference.Expectation{Value: tc.value}
				}
				t.Run(name, func(t *testing.T) {
					t.Parallel()
					variables := map[string]any{"github": map[string]any{"event": map[string]any{"unknown": left}}}
					runnerreference.CheckOutcome(t, tc.input, variables, want)
				})
			}
		})
	}
}

func TestCaseTypedPredicateFailures(t *testing.T) {
	t.Parallel()
	t.Run("later predicate range", func(t *testing.T) {
		t.Parallel()
		const input = "case(false, 'a', 'text', 'b', 'c')"
		got := checkCallDemandResult(t, input)
		if got.Valid || got.FailurePhase != FailureEvaluation || len(got.Findings) != 1 {
			t.Fatalf("expected one runtime predicate failure: %+v", got)
		}
		finding := got.Findings[0]
		start := strings.Index(input, "'text'")
		if finding.Code != "operand-type" || finding.Offset != start || finding.EndOffset != start+len("'text'") {
			t.Fatalf("finding=%+v, want operand-type at [%d,%d)", finding, start, start+len("'text'"))
		}
		runnerreference.CheckOutcome(t, input, nil, runnerreference.Expectation{Failure: runnerreference.RuntimeFailure, ExceptionType: "System.InvalidOperationException"})
	})

	t.Run("required predicate", func(t *testing.T) {
		t.Parallel()
		const input = "case(github.event_name, fromJSON(null), fromJSON(null))"
		got := checkCallDemandResult(t, input)
		if got.Valid || got.Evaluation.State != exprvalues.EvaluationInvalid || got.FailurePhase != FailureEvaluation || got.Type.Kind() != exprtypes.KindAny {
			t.Fatalf("required nonboolean predicate did not fail: %+v", got)
		}
		checkCallArgumentDemands(t, got, []exprfunctions.ArgumentDemand{exprfunctions.DemandRequired, exprfunctions.DemandSkipped, exprfunctions.DemandSkipped})
		if len(got.Findings) != 1 || got.Findings[0].Code != "operand-type" || len(got.Call.RefinedAlternatives) != 0 {
			t.Fatalf("unreachable branches contributed findings or return alternatives: %+v", got)
		}
		predicate := got.Call.Arguments[0]
		if predicate.Source.Kind() != exprtypes.KindString || predicate.SourceValue.State != exprvalues.EvaluationUnknown || predicate.Accepted || predicate.Parameter.Kind() != exprtypes.KindBool || predicate.Value.State != exprvalues.EvaluationInvalid {
			t.Fatalf("source evaluation and rejected predicate contract were mixed: %+v", predicate)
		}
		runnerreference.CheckOutcome(t, input, map[string]any{"github": map[string]any{"event_name": "push"}}, runnerreference.Expectation{Failure: runnerreference.RuntimeFailure, ExceptionType: "System.InvalidOperationException"})
	})

	t.Run("conditional predicate", func(t *testing.T) {
		t.Parallel()
		const input = "case(github.event.unknown, fromJSON('[]'), github.event_name, 'b', 'c')"
		got := checkCallDemandResult(t, input)
		if !got.Valid || got.Evaluation.State != exprvalues.EvaluationUnknown || got.FailurePhase != FailureNone || len(got.Findings) != 0 || got.Type.Kind() != exprtypes.KindArray {
			t.Fatalf("conditional failure obscured the only reachable return: %+v", got)
		}
		checkCallArgumentDemands(t, got, []exprfunctions.ArgumentDemand{exprfunctions.DemandRequired, exprfunctions.DemandConditional, exprfunctions.DemandConditional, exprfunctions.DemandSkipped, exprfunctions.DemandSkipped})
		if len(got.Call.RefinedAlternatives) != 1 || got.Call.RefinedAlternatives[0].Kind() != exprtypes.KindArray {
			t.Fatalf("unreachable branches contributed return alternatives: %+v", got.Call)
		}
		predicate := got.Call.Arguments[2]
		if predicate.Source.Kind() != exprtypes.KindString || predicate.SourceValue.State != exprvalues.EvaluationUnknown || predicate.Accepted || predicate.Parameter.Kind() != exprtypes.KindBool || predicate.Value.State != exprvalues.EvaluationInvalid {
			t.Fatalf("source evaluation and rejected predicate contract were mixed: %+v", predicate)
		}
		for _, first := range []bool{false, true} {
			name := "false"
			want := runnerreference.Expectation{Failure: runnerreference.RuntimeFailure, ExceptionType: "System.InvalidOperationException"}
			if first {
				name = "true"
				want = runnerreference.Expectation{Value: []any{}}
			}
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				variables := map[string]any{"github": map[string]any{"event_name": "push", "event": map[string]any{"unknown": first}}}
				runnerreference.CheckOutcome(t, input, variables, want)
			})
		}
	})
}

func TestNestedFunctionProblemsFollowExecutionDemand(t *testing.T) {
	t.Parallel()
	for _, operation := range []struct {
		input, exception string
	}{
		{"format('{1}', true)", "System.FormatException"},
		{"fromJSON(null)", "System.Text.Json.JsonException"},
		{"case(1, 'a', 'b')", "System.InvalidOperationException"},
	} {
		for _, predicate := range []string{"true", "false", "github.event.unknown"} {
			input := "case(" + predicate + ", " + operation.input + ", 'fallback')"
			t.Run(input, func(t *testing.T) {
				t.Parallel()
				got := checkCallDemandResult(t, input)
				wantState := exprvalues.EvaluationUnknown
				wantPhase := FailureNone
				var wantValue any
				wantFindings := 0
				switch predicate {
				case "true":
					wantState, wantPhase, wantFindings = exprvalues.EvaluationInvalid, FailureEvaluation, 1
				case "false":
					wantState, wantValue = exprvalues.EvaluationKnown, "fallback"
				}
				if got.Valid != (wantPhase == FailureNone) || got.FailurePhase != wantPhase || got.Evaluation.State != wantState || !cmp.Equal(got.Evaluation.Value, wantValue) || len(got.Findings) != wantFindings {
					t.Fatalf("result=%+v, want phase=%v state=%v value=%v findings=%d", got, wantPhase, wantState, wantValue, wantFindings)
				}
				failure := runnerreference.Expectation{Failure: runnerreference.RuntimeFailure, ExceptionType: operation.exception}
				success := runnerreference.Expectation{Value: "fallback"}
				switch predicate {
				case "true":
					runnerreference.CheckOutcome(t, input, nil, failure)
				case "false":
					runnerreference.CheckOutcome(t, input, nil, success)
				default:
					for _, selected := range []bool{false, true} {
						want := success
						if selected {
							want = failure
						}
						variables := map[string]any{"github": map[string]any{"event": map[string]any{"unknown": selected}}}
						runnerreference.CheckOutcome(t, input, variables, want)
					}
				}
			})
		}
	}
}

func TestCaseArityRejectsSkippedPaths(t *testing.T) {
	t.Parallel()
	for _, input := range []string{
		"case(true, 'a')",
		"case(true, 'a', false, 'b')",
		"case(true, 'a', startsWith(true))",
	} {
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			got := checkCallDemandResult(t, input)
			if got.Valid || got.Evaluation.State != exprvalues.EvaluationNotAttempted || got.FailurePhase != FailureStaticValidation || len(got.Findings) == 0 {
				t.Fatalf("invalid arity survived static validation: %+v", got)
			}
			runnerreference.CheckOutcome(t, input, nil, runnerreference.Expectation{Failure: runnerreference.ParseFailure, ExceptionType: "GitHub.Actions.Expressions.ParseException"})
		})
	}
}

func TestJoinPreservesArrayArgumentFacts(t *testing.T) {
	t.Parallel()
	const input = "join(fromJSON('[true,false]'), ',')"
	got := checkCallDemandResult(t, input)
	if !got.Valid || got.Evaluation.State != exprvalues.EvaluationKnown || got.Evaluation.Value != "true,false" {
		t.Fatalf("result: %+v", got)
	}
	checkCallArgumentDemands(t, got, []exprfunctions.ArgumentDemand{exprfunctions.DemandRequired, exprfunctions.DemandRequired})
	arg := got.Call.Arguments[0]
	parameter, parameterArray := arg.Parameter.Array()
	source, sourceArray := arg.Source.Array()
	converted, convertedArray := arg.Converted.Array()
	if !arg.Accepted || !parameterArray || parameter.Elem().Kind() != exprtypes.KindAny || !sourceArray || source.Elem().Kind() != exprtypes.KindBool || !convertedArray || converted.Elem().Kind() != exprtypes.KindBool || arg.Conversion != exprfunctions.ConversionNone {
		t.Fatalf("array argument falsely claims an aggregate string conversion: %+v", arg)
	}
	want := exprvalues.EvaluationResult{State: exprvalues.EvaluationKnown, Value: []any{true, false}}
	if diff := cmp.Diff(want, arg.SourceValue); diff != "" {
		t.Fatalf("raw argument (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(want, arg.Value); diff != "" {
		t.Fatalf("consumed array (-want +got):\n%s", diff)
	}
	runnerreference.CheckValue(t, input, nil, "true,false")
}

func TestCaseArgumentContracts(t *testing.T) {
	t.Parallel()
	got := checkCallDemandResult(t, "case(true, 'a', 'b')")
	checkCallArgumentDemands(t, got, []exprfunctions.ArgumentDemand{exprfunctions.DemandRequired, exprfunctions.DemandRequired, exprfunctions.DemandSkipped})
	for i, arg := range got.Call.Arguments {
		wantParameter := exprtypes.KindAny
		if i == 0 {
			wantParameter = exprtypes.KindBool
		}
		if !arg.Accepted || arg.Parameter.Kind() != wantParameter || arg.Conversion != exprfunctions.ConversionNone || !arg.Source.Equal(arg.Converted) {
			t.Fatalf("argument %d contract: %+v", i, arg)
		}
	}
	if got.Call.Arguments[0].Value.Value != true || got.Call.Arguments[1].Value.Value != "a" {
		t.Fatalf("missing consumed case arguments: %+v", got.Call.Arguments)
	}
}

func checkCallDemandResult(t *testing.T, input string) ExpressionCheckResult {
	t.Helper()
	node, err := exprsyntax.NewParser().Parse(exprsyntax.NewLexer(input + "}}"))
	if err != nil {
		t.Fatal(err)
	}
	checker := NewChecker(contextspec.DefaultExpressionScope(), nil)
	checker.SetAvailability(contextspec.WorkflowExpressionScope("jobs.<job_id>.steps.if"))
	return checker.CheckResult(expreval.EvaluateSubexpressions(node))
}

func checkCallArgumentDemands(t *testing.T, got ExpressionCheckResult, want []exprfunctions.ArgumentDemand) {
	t.Helper()
	if got.Call == nil || len(got.Call.Arguments) != len(want) {
		t.Fatalf("missing argument facts: %+v; want %d arguments", got.Call, len(want))
	}
	for i, arg := range got.Call.Arguments {
		if arg.Demand != want[i] {
			t.Errorf("argument %d demand=%v, want %v", i, arg.Demand, want[i])
		}
		if want[i] == exprfunctions.DemandSkipped && arg.Value.State != exprvalues.EvaluationUnknown {
			t.Errorf("skipped argument %d was evaluated: %+v", i, arg)
		}
	}
}
