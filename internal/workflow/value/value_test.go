package value

import (
	"errors"
	"math"
	"testing"

	"actionlint.kjanat.dev/v2/diagnostic"
	exprlang "actionlint.kjanat.dev/v2/internal/expression"
)

func TestMissingAndExplicitScalarStates(t *testing.T) {
	cases := []struct {
		name  string
		value Value
		state State
		kind  Kind
	}{
		{name: "missing", state: Missing, kind: KindUnknown},
		{name: "null", value: YAMLScalar("null", "!!null", Origin{}), state: Literal, kind: Null},
		{name: "false", value: YAMLScalar("false", "!!bool", Origin{}), state: Literal, kind: Bool},
		{name: "zero", value: YAMLScalar("0", "!!int", Origin{}), state: Literal, kind: Number},
		{name: "empty string", value: YAMLScalar("", "!!str", Origin{}), state: Literal, kind: String},
		{name: "evaluated null", value: FromJSON(nil, Origin{}), state: Evaluated, kind: Null},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.value.State(); got != tc.state {
				t.Fatalf("state=%v want %v", got, tc.state)
			}
			if got := tc.value.Kind(); got != tc.kind {
				t.Fatalf("kind=%v want %v", got, tc.kind)
			}
			if tc.state != Missing && !tc.value.Known() {
				t.Fatal("explicit scalar became unknown")
			}
		})
	}
}

func TestTemplateClassifiesLiteralEvaluatedDynamicAndInvalid(t *testing.T) {
	origin := Origin{Span: Span{Start: diagnostic.Position{Line: 3, Column: 5}, End: diagnostic.Position{Line: 3, Column: 28}}}
	for _, tc := range []struct {
		name  string
		input string
		state State
		kind  Kind
		text  string
		known bool
	}{
		{name: "literal", input: "plain text", state: Literal, kind: String, text: "plain text", known: true},
		{name: "evaluated", input: "${{ 'literal text' }}", state: Evaluated, kind: String, text: "literal text", known: true},
		{name: "dynamic assigned", input: "${{ github.ref }}", state: Dynamic, kind: KindUnknown, known: false},
		{name: "dynamic fragment", input: "prefix ${{ github.ref }} suffix", state: Dynamic, kind: String, known: false},
		{name: "invalid", input: "${{ true + }}", state: Invalid, kind: KindUnknown, known: false},
		{name: "unterminated", input: "${{ true", state: Invalid, kind: KindUnknown, known: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			value := Template(tc.input, origin)
			if value.State() != tc.state || value.Kind() != tc.kind || value.Known() != tc.known {
				t.Fatalf("got state=%v kind=%v known=%v", value.State(), value.Kind(), value.Known())
			}
			if value.Known() {
				text, ok := value.Text()
				if !ok || text != tc.text {
					t.Fatalf("text=%q known=%v", text, ok)
				}
			}
			if tc.state == Dynamic || tc.state == Invalid {
				if value.Source() != tc.input {
					t.Fatalf("source=%q", value.Source())
				}
			}
			if tc.state != Literal && (!value.Origin().FromExpression || value.Origin().Span != origin.Span) {
				t.Fatalf("expression provenance lost: %+v", value.Origin())
			}
			if tc.state == Invalid && value.Err() == nil {
				t.Fatal("invalid value omitted parse error")
			}
		})
	}
}

func TestEvaluatedExpressionTextRemainsData(t *testing.T) {
	value := Template(`${{ fromJSON('"${{ runtime }}"') }}`, Origin{})
	text, ok := value.Text()
	if !ok || text != "${{ runtime }}" || value.State() != Evaluated {
		t.Fatalf("evaluated expression-looking text was reinterpreted: state=%v text=%q", value.State(), text)
	}
}

func TestConditionPreservesInvalidAndDynamicStates(t *testing.T) {
	origin := Origin{Span: Span{Start: diagnostic.Position{Line: 4, Column: 3}}}
	if got := Condition("true", origin); got.State() != Evaluated || got.Kind() != Bool || !got.Origin().FromExpression || got.Origin().Span != origin.Span {
		t.Fatalf("constant condition: state=%v kind=%v", got.State(), got.Kind())
	}
	if got := Condition("${{ github.ref }}", Origin{}); got.State() != Dynamic || got.Known() {
		t.Fatalf("runtime condition became known: state=%v", got.State())
	}
	if got := Condition("${{ true + }}", Origin{}); got.State() != Invalid || got.Err() == nil {
		t.Fatalf("malformed condition lost parse error: state=%v err=%v", got.State(), got.Err())
	}
}

func TestEvaluatedChildrenKeepExpressionOriginAndAreDeepOwned(t *testing.T) {
	span := Span{Start: diagnostic.Position{Line: 5, Column: 7}, End: diagnostic.Position{Line: 5, Column: 39}}
	input := map[string]any{"outer": []any{map[string]any{"value": "before"}}}
	value := FromJSON(input, Origin{Span: span, FromExpression: true})
	input["outer"].([]any)[0].(map[string]any)["value"] = "after"

	outer, ok := value.Prop("OUTER")
	if !ok || outer.State() != Evaluated || outer.Kind() != Array || outer.Origin().Span != span || !outer.Origin().FromExpression {
		t.Fatalf("outer projection lost semantic origin: %+v", outer)
	}
	first, ok := outer.Index(0)
	if !ok || first.Origin().Span != span || !first.Origin().FromExpression {
		t.Fatalf("nested object lost expression origin: %+v", first)
	}
	textValue, ok := first.Prop("value")
	text, textKnown := textValue.Text()
	if !ok || !textKnown || text != "before" || textValue.Origin().Span != span {
		t.Fatalf("host mutation leaked or nested origin lost: value=%q", text)
	}
}

func TestSemanticEqualityIgnoresOriginAndKnownState(t *testing.T) {
	literal := YAMLScalar("3.10", "!!float", Origin{Span: Span{Start: diagnostic.Position{Line: 1, Column: 1}}})
	evaluated := FromJSON(3.1, Origin{Span: Span{Start: diagnostic.Position{Line: 2, Column: 4}}, FromExpression: true})
	if !literal.Equal(evaluated) {
		t.Fatal("equal numeric values with different provenance/state compared unequal")
	}
	if !YAMLScalar(".nan", "!!float", Origin{}).Equal(YAMLScalar(".NaN", "!!float", Origin{})) {
		t.Fatal("NaN values should compare equal under raw YAML equality")
	}
	if YAMLScalar("true", "!!bool", Origin{}).Equal(YAMLScalar("true", "!!str", Origin{})) {
		t.Fatal("different YAML semantic kinds compared equal")
	}
	if DynamicValue("", KindUnknown, Origin{}).Equal(DynamicValue("", KindUnknown, Origin{})) {
		t.Fatal("unknown values compared equal as known runtime truth")
	}
	if (Value{}).Equal(Value{}) {
		t.Fatal("missing values compared equal")
	}
	if !FromJSON(math.NaN(), Origin{}).Equal(FromJSON(math.NaN(), Origin{})) {
		t.Fatal("evaluated NaN equality changed")
	}
}

func TestYAMLCoreScalarCoercionAndScalarFormatting(t *testing.T) {
	for _, tc := range []struct {
		text string
		kind Kind
		want string
	}{
		{text: "TRUE", kind: Bool, want: "true"},
		{text: "~", kind: Null, want: ""},
		{text: "010", kind: Number, want: "10"},
		{text: "0x10", kind: Number, want: "16"},
		{text: "0o10", kind: Number, want: "8"},
		{text: "3.10", kind: Number, want: "3.1"},
		{text: "yes", kind: String, want: "yes"},
	} {
		t.Run(tc.text, func(t *testing.T) {
			tag := "!!int"
			if tc.text == "TRUE" {
				tag = "!!bool"
			} else if tc.text == "~" {
				tag = "!!null"
			} else if tc.text == "3.10" {
				tag = "!!float"
			} else if tc.text == "yes" {
				tag = "!!bool"
			}
			value := YAMLScalar(tc.text, tag, Origin{})
			got, ok := value.ScalarString()
			if value.Kind() != tc.kind || !ok || got != tc.want {
				t.Fatalf("got kind=%v scalar=%q known=%v", value.Kind(), got, ok)
			}
		})
	}
}

func TestObjectPropertiesPreferExactCaseThenIgnoreCase(t *testing.T) {
	value := Map(Origin{},
		Prop{Name: "Name", Value: FromJSON("first", Origin{})},
		Prop{Name: "name", Value: FromJSON("exact", Origin{})},
	)
	exact, ok := value.Prop("name")
	text, _ := exact.Text()
	if !ok || text != "exact" {
		t.Fatalf("exact property lookup returned %q", text)
	}
	folded, ok := value.Prop("NAME")
	text, _ = folded.Text()
	if !ok || text != "first" {
		t.Fatalf("case-insensitive property lookup returned %q", text)
	}
	fromJSON := FromJSON(map[string]any{"Image": "upper", "image": "lower"}, Origin{})
	exact, ok = fromJSON.Prop("image")
	text, _ = exact.Text()
	if !ok || text != "lower" {
		t.Fatalf("FromJSON exact property lookup returned %q", text)
	}
}

func TestFromJSONRejectsUnsupportedHostValues(t *testing.T) {
	value := FromJSON(make(chan int), Origin{})
	if value.State() != Invalid || value.Err() == nil {
		t.Fatalf("unsupported host type did not become an explicit invalid value: %+v", value)
	}
}

func TestInvalidErrorCannotBeMutatedThroughAccessor(t *testing.T) {
	value := Template("${{ true + }}", Origin{})
	err := value.Err()
	if err == nil {
		t.Fatal("expected invalid expression")
	}
	want := err.Error()
	if parseError, ok := errors.AsType[*exprlang.Error](err); ok {
		parseError.Message = "caller mutation"
	}
	if got := value.Err().Error(); got != want {
		t.Fatalf("error accessor leaked mutable expression error: got %q want %q", got, want)
	}
}

func TestSourceContainersAreLiteralAndEvaluatedContainersAreEvaluated(t *testing.T) {
	unknown := DynamicValue("", KindUnknown, Origin{})
	sourceArray := Seq(Origin{}, YAMLScalar("source", "!!str", Origin{}), unknown)
	sourceObject := Map(Origin{}, Prop{Name: "item", Value: unknown})
	if sourceArray.State() != Literal || sourceObject.State() != Literal || !sourceArray.Known() || !sourceObject.Known() {
		t.Fatalf("source container state: array=%v object=%v", sourceArray.State(), sourceObject.State())
	}
	if sourceArray.Equal(sourceArray) || sourceObject.Equal(sourceObject) {
		t.Fatal("container with dynamic child compared equal as known runtime truth")
	}
	evaluated := FromJSON([]any{map[string]any{"item": "value"}}, Origin{FromExpression: true})
	child, _ := evaluated.Index(0)
	if evaluated.State() != Evaluated || child.State() != Evaluated {
		t.Fatalf("JSON expression containers not evaluated recursively: outer=%v child=%v", evaluated.State(), child.State())
	}
}

func TestContainerConstructorsCopyTheirInputs(t *testing.T) {
	items := []Value{FromJSON("first", Origin{})}
	array := Seq(Origin{}, items...)
	items[0] = FromJSON("changed", Origin{})
	item, _ := array.Index(0)
	text, _ := item.Text()
	if text != "first" {
		t.Fatalf("sequence input mutation leaked: %q", text)
	}
	props := []Prop{{Name: "name", Value: FromJSON("first", Origin{})}}
	object := Map(Origin{}, props...)
	props[0].Name = "changed"
	prop, ok := object.Prop("name")
	text, _ = prop.Text()
	if !ok || text != "first" {
		t.Fatalf("map input mutation leaked: %q", text)
	}
}
