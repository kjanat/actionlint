package json_test

import (
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"

	contextspec "actionlint.kjanat.dev/v2/internal/model/contexts"

	evaluation "actionlint.kjanat.dev/v2/internal/languages/expressions/evaluate"

	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"

	typecheck "actionlint.kjanat.dev/v2/internal/languages/expressions/typecheck"

	jsonlang "actionlint.kjanat.dev/v2/internal/languages/json"
)

// These cases follow the runner receipts in docs/expression-results/2026-09-14.md
// and Runny 0.3.0's loose engines (runner 1ba885c2065fd9be317808ab3fd406f392635cf7).
func TestFromJSONValue(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		source string
		want   any
	}{
		{`[/* npm ships inside nodejs */ {pm:'bun', tools:'bun'},] that is all, folks`, []any{map[string]any{"pm": "bun", "tools": "bun"}}},
		{`{install_args:'nodejs uv python bun' /* npm ships inside nodejs */,} that is all, folks`, map[string]any{"install_args": "nodejs uv python bun"}},
		{`['nodejs','uv','python','bun',] // bun, if you insist`, []any{"nodejs", "uv", "python", "bun"}},
		{`/* comment */{"n":1}`, " comment "},
		{"// comment\n31", " comment"},
		{`{"approved":/*x*/true/*y*/,"n":1}`, map[string]any{"approved": true, "n": float64(1)}},
		{`31 trailing`, float64(31)},
		{`'-0'`, "-0"},
		{`{"n":1}{"n":2}`, map[string]any{"n": float64(1)}},
		{`017`, float64(15)},
		{`0x1F`, float64(31)},
		{`.5`, 0.5},
		{`1.`, float64(1)},
		{`Infinity`, math.Inf(1)},
		{`-Infinity`, math.Inf(-1)},
		{`1e309`, math.Inf(1)},
		{`undefined`, ""},
		{`,`, ""},
		{`{a:,}`, map[string]any{"a": ""}},
		{`0xffffffffffffffff`, float64(-1)},
		{`[undefined,,null]`, []any{"", "", nil}},
		{`'\uD83D\uDE00'`, "😀"},
		{`'\uD800\uD800\uDC00'`, "�𐀀"},
		{`'\uDC00'`, "�"},
		{`'\"\'\\\/\b\f\n\r\t'`, "\"'\\/\b\f\n\r\t"},
		{"'line\nbreak'", "line\nbreak"},
		{`{123:1,é:2,$n:3,_n:4}`, map[string]any{"123": float64(1), "é": float64(2), "$n": float64(3), "_n": float64(4)}},
	} {
		t.Run(tc.source, func(t *testing.T) {
			t.Parallel()
			got, err := jsonlang.FromJSONValue(tc.source)
			if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %#v, %v; want %#v", got, err, tc.want)
			}
			expr := exprsyntax.ParseAssignedExpression("${{ fromJSON('" + strings.ReplaceAll(tc.source, "'", "''") + "') }}")
			if expr == nil {
				t.Fatal("failed to parse expression")
			}
			folded, known := evaluation.ConditionConstantValue(expr)
			if !known || !reflect.DeepEqual(folded, tc.want) {
				t.Fatalf("constant got %#v, known=%v", folded, known)
			}
			if _, errs := typecheck.NewChecker(contextspec.DefaultExpressionScope(), nil).Check(evaluation.EvaluateSubexpressions(expr)); len(errs) != 0 {
				t.Fatal(errs)
			}
		})
	}
	got, err := jsonlang.FromJSONValue("NaN")
	n, ok := got.(float64)
	if err != nil || !ok || !math.IsNaN(n) {
		t.Fatalf("NaN got %v, %v", got, err)
	}
}

func TestFromJSONIntegerNegativeZeroRemainsModeDependent(t *testing.T) {
	t.Parallel()
	for _, text := range []string{"-0", "[-0]", "{value:-0}"} {
		value, err := jsonlang.FromJSONValue(text)
		if err != nil {
			t.Fatalf("valid loose JSON rejected: %s: %v", text, err)
		}
		if text == "-0" {
			number, ok := value.(float64)
			if !ok || number != 0 || math.Signbit(number) {
				t.Fatalf("loose integer -0 did not lose its sign: %#v", value)
			}
		}
		expr := exprsyntax.ParseAssignedExpression("${{ fromJSON('" + text + "') }}")
		if value, known := evaluation.ConditionConstantValue(expr); known {
			t.Fatalf("mode-dependent sign became a constant: %s: %#v", text, value)
		}
	}
}

func TestFromJSONInvalidSyntax(t *testing.T) {
	t.Parallel()
	for _, source := range []string{
		"", " ", "0o17", "+1", "08", "0x", "0x10000000000000000", "truex", "31trailing",
		"[1 2]", "{a:1 b:2}", "{a:}", "[", "{", "'unterminated", `"\x20"`, `"\uZZZZ"`,
		"/* unterminated", "[/* unterminated", "{a/*comment*/:1}", "{a-b:1}", "[1e]",
		"new Date(", "new Date(0 1)", "[new Date(0) false]", "new Foo_bar()",
		strings.Repeat("[", 65) + "0" + strings.Repeat("]", 65), strings.Repeat("9", 381),
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			if got, err := jsonlang.FromJSONValue(source); err == nil {
				t.Fatalf("accepted invalid input: %#v", got)
			}
			expr := exprsyntax.ParseAssignedExpression("${{ fromJSON('" + strings.ReplaceAll(source, "'", "''") + "') }}")
			if _, known := evaluation.ConditionConstantValue(expr); known {
				t.Fatal("folded invalid input")
			}
			_, errs := typecheck.NewChecker(contextspec.DefaultExpressionScope(), nil).Check(evaluation.EvaluateSubexpressions(expr))
			if len(errs) != 1 || !strings.Contains(errs[0].Message, "broken JSON string") {
				t.Fatal(errs)
			}
		})
	}
}

func TestFromJSONErrorOffsets(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		source string
		offset int
	}{
		{`{"foo": true`, 12},
		{"", 0},
		{`[1 x]`, 4},
	} {
		_, err := jsonlang.FromJSONValue(tc.source)
		var syntax *jsonlang.ReadError
		if !errors.As(err, &syntax) || syntax.Offset != tc.offset {
			t.Fatalf("%q: got %v; want offset %d", tc.source, err, tc.offset)
		}
	}
}

func TestFromJSONUnsupportedConstructor(t *testing.T) {
	t.Parallel()
	for _, source := range []string{"new Date(0)", "{a:new Date(0),}", "[new Foo(true,undefined,)]"} {
		if _, err := jsonlang.FromJSONValue(source); !errors.Is(err, jsonlang.ErrUnsupportedValue) {
			t.Fatalf("%s: %v", source, err)
		}
		expr := exprsyntax.ParseAssignedExpression("${{ fromJSON('" + source + "') }}")
		if _, known := evaluation.ConditionConstantValue(expr); known {
			t.Fatal("folded unsupported constructor")
		}
		if _, errs := typecheck.NewChecker(contextspec.DefaultExpressionScope(), nil).Check(evaluation.EvaluateSubexpressions(expr)); len(errs) != 0 {
			t.Fatal(errs)
		}
	}
	expr := exprsyntax.ParseAssignedExpression("${{ fromJSON('{a:new Date(0), A:1}') }}")
	_, errs := typecheck.NewChecker(contextspec.DefaultExpressionScope(), nil).Check(evaluation.EvaluateSubexpressions(expr))
	if len(errs) != 1 || !strings.Contains(errs[0].Message, "collide under case-insensitive lookup") {
		t.Fatal(errs)
	}
}

func TestFromJSONCollisionOffsets(t *testing.T) {
	t.Parallel()
	source := `{/*π*/ 'é':1, É:2, '\u00e9':3,} trailing`
	collisions := jsonlang.JSONMemberCollisions(source)
	want := []jsonlang.MemberCollision{
		{"é", "É", int64(strings.Index(source, "É:") + len("É"))},
		{"é", "é", int64(strings.Index(source, "'\\u00e9'") + len("'\\u00e9'"))},
	}
	if !reflect.DeepEqual(collisions, want) {
		t.Fatalf("got %#v; want %#v", collisions, want)
	}
	expr := exprsyntax.ParseAssignedExpression("${{ fromJSON('" + strings.ReplaceAll(source, "'", "''") + "') }}")
	if _, known := evaluation.ConditionConstantValue(expr); known {
		t.Fatal("folded ambiguous member")
	}
	_, errs := typecheck.NewChecker(contextspec.DefaultExpressionScope(), nil).Check(evaluation.EvaluateSubexpressions(expr))
	if len(errs) != 2 {
		t.Fatal(errs)
	}
	if got := jsonlang.JSONMemberCollisions(`/*comment*/{"a":1,"A":2}`); len(got) != 0 {
		t.Fatal(got)
	}
}
