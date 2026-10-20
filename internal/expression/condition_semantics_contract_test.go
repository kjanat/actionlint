package expression_test

import (
	"testing"

	exprlang "actionlint.kjanat.dev/internal/expression"
)

func TestConditionConstantCase(t *testing.T) {
	for _, tc := range []struct {
		expression string
		value      any
		known      bool
	}{
		{"case(true, false, true)", false, true},
		{"case(false, false, true)", true, true},
		{"case(false, 'first', true, 'second', 'fallback')", "second", true},
		{"case(false, 'first', false, 'second', 'fallback')", "fallback", true},
		{"case(true, 'first', true, 'second', 'fallback')", "first", true},
		{"CASE(true, null, 1)", nil, true},
		{"case(true, 0, 1)", float64(0), true},
		{"case(true, '', 'fallback')", "", true},
		{"case(true, false, inputs.fallback)", false, true},
		{"case(false, inputs.skipped, true)", true, true},
		{"case(true, false, inputs.predicate, inputs.value, inputs.fallback)", false, true},
		{"case(false, fromJSON('broken'), true)", true, true},
		{"case(true, case(false, false, true), false)", true, true},
		{"case(inputs.predicate, false, true)", nil, false},
		{"case(true, inputs.value, false)", nil, false},
		{"case(false, true, inputs.fallback)", nil, false},
		{"case(1, true, false)", nil, false},
		{"case('false', true, false)", nil, false},
		{"case(null, true, false)", nil, false},
		{"case(false, true, 1, false, true)", nil, false},
		{"case(0, true, false)", nil, false},
		{"case('', true, false)", nil, false},
		{"case(fromJSON('[]'), true, false)", nil, false},
		{"case()", nil, false},
		{"case(true)", nil, false},
		{"case(true, false)", nil, false},
		{"case(true, false, true, false)", nil, false},
	} {
		t.Run(tc.expression, func(t *testing.T) {
			expr := exprlang.ParseAssignedExpression("${{ " + tc.expression + " }}")
			if expr == nil {
				t.Fatal("invalid fixture")
			}
			value, known := exprlang.ConditionConstantValue(expr)
			if known != tc.known || value != tc.value {
				t.Fatalf("value=%v known=%v, want value=%v known=%v", value, known, tc.value, tc.known)
			}
		})
	}
}

func TestConditionConstantOutcomes(t *testing.T) {
	for _, tc := range []struct {
		expression string
		truthy     bool
	}{
		{"'0'", true},
		{"'0' == false", true},
		{"'' == 0", true},
		{"0 == '0'", true},
		{"'' == '0'", false},
		{"'0.0' == 0", true},
		{"'0x0' == 0", true},
		{"'0x80000000' <= 100", true},
		{"'false' == false", false},
		{"1 == 2", false},
		{"false || (true && true)", true},
		{"0 || 1", true},
		{"format('')", false},
		{"format('false')", true},
		{"contains(format('{0} {1}', 'foo', 'bar'), 'O B')", true},
		{"fromJSON('false')", false},
		{"fromJSON('[]')", true},
		{`contains(fromJSON('["x"]'), 'X')`, true},
		{`contains(fromJSON('["x"]'), 'y')`, false},
		{`contains(fromJSON('[]'), null)`, false},
		{`contains(fromJSON('[false]'), 0)`, true},
		{`contains(fromJSON('[null]'), '')`, true},
		{`contains(fromJSON('[{}, null]'), null)`, true},
		{`contains(fromJSON('[{}]'), null)`, false},
		{`fromJSON('{}') == null`, false},
		{`null == fromJSON('[]')`, false},
		{`fromJSON('[]') != null`, true},
		{`null != fromJSON('{}')`, true},
	} {
		t.Run(tc.expression, func(t *testing.T) {
			expr, err := exprlang.NewParser().Parse(exprlang.NewLexer(tc.expression + "}}"))
			if err != nil {
				t.Fatal(err)
			}
			value, known := exprlang.ConditionConstantValue(expr)
			if !known || exprlang.ExpressionTruthy(value) != tc.truthy {
				t.Fatalf("value=%v known=%v", value, known)
			}
		})
	}
}

func TestConditionConstantSerialization(t *testing.T) {
	for _, tc := range []struct{ expression, want string }{
		{"toJSON(null)", "null"},
		{"toJSON(false)", "false"},
		{"toJSON(true)", "true"},
		{"toJSON('')", `""`},
		{"toJSON('hello world')", `"hello world"`},
		{"TOJSON('false')", `"false"`},
		{`join(fromJSON('[{}, ""]'), '')`, "Object"},
		{`join(fromJSON('[[], ""]'), '')`, "Array"},
		{`join(fromJSON('[{}, []]'))`, "Object,Array"},
		{`join(fromJSON('[{}, []]'), fromJSON('{}'))`, "Object,Array"},
		{`join(fromJSON('[{}, []]'), fromJSON('[]'))`, "Object,Array"},
		{`join(fromJSON('{}'))`, ""},
		{`join(fromJSON('[]'))`, ""},
		{`join(fromJSON('[null, false, true]'), '-')`, "-false-true"},
	} {
		t.Run(tc.expression, func(t *testing.T) {
			expr := exprlang.ParseAssignedExpression("${{ " + tc.expression + " }}")
			value, known := exprlang.ConditionConstantValue(expr)
			if !known || value != tc.want {
				t.Fatalf("value=%v known=%v, want %q", value, known, tc.want)
			}
		})
	}
}

func TestConditionSerializationRemainsUnknown(t *testing.T) {
	// The engines disagree on signed-zero serialization; strict JSON rejects
	// non-finite round trips. Do not invent a single folded outcome.
	for _, expression := range []string{
		"toJSON(-0)", "format('{0}', -0)", "fromJSON(toJSON(fromJSON('1e309')))",
		"toJSON(1)", "toJSON(fromJSON('{}'))", "toJSON(fromJSON('[]'))",
		"toJSON('<html>')", "toJSON('é')", `toJSON('"')`, `toJSON('\')`,
		"toJSON('''')", "toJSON('+')", "toJSON('`')", "toJSON('\n')",
		`fromJSON('{"approved":false,"APPROVED":true}')`,
		`fromJSON('{}') == fromJSON('{}')`,
		`contains(fromJSON('[{}]'), fromJSON('{}'))`,
	} {
		expr := exprlang.ParseAssignedExpression("${{ " + expression + " }}")
		if expr == nil {
			t.Fatalf("invalid fixture %s", expression)
		}
		if v, known := exprlang.ConditionConstantValue(expr); known {
			t.Fatalf("invented outcome for %s: %v", expression, v)
		}
	}
}
