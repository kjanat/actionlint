package mapping

import (
	"testing"

	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/workflow/value"
)

func TestMappingDistinguishesAbsentFromExplicitScalars(t *testing.T) {
	inputs := &Inputs{Values: map[string]Input{
		"null":  {Name: text("null"), Value: value.YAMLScalar("null", "!!null", value.Origin{})},
		"false": {Name: text("false"), Value: value.YAMLScalar("false", "!!bool", value.Origin{})},
		"zero":  {Name: text("zero"), Value: value.YAMLScalar("0", "!!int", value.Origin{})},
		"empty": {Name: text("empty"), Value: value.YAMLScalar("", "!!str", value.Origin{})},
	}}
	resolved := Resolve(inputs)
	if !resolved.Complete() {
		t.Fatal("literal mapping must be complete")
	}
	for key, state := range map[string]value.State{"null": value.Literal, "false": value.Literal, "zero": value.Literal, "empty": value.Literal} {
		if got := resolved.Lookup(key).State(); got != state {
			t.Errorf("Lookup(%q).State()=%v, want %v", key, got, state)
		}
	}
	if got := resolved.Lookup("absent").State(); got != value.Missing {
		t.Fatalf("absent key state=%v, want Missing", got)
	}
	if got, _ := resolved.Lookup("false").Bool(); got {
		t.Fatal("explicit false became true")
	}
	if got, _ := resolved.Lookup("zero").Number(); got != 0 {
		t.Fatalf("explicit zero=%v", got)
	}
	if got, _ := resolved.Lookup("empty").Text(); got != "" {
		t.Fatalf("explicit empty string=%q", got)
	}
}

func TestMappingIncompleteAndDuplicateLookupsAreDynamic(t *testing.T) {
	base := &Inputs{Values: map[string]Input{"known": {Name: text("known"), Value: text("value")}}}
	incomplete := Resolve(&Inputs{Values: base.Values, Expression: value.DynamicValue("${{ inputs.mapping }}", value.KindUnknown, value.Origin{})})
	if got := incomplete.Lookup("known").State(); got != value.Dynamic {
		t.Fatalf("known key beside dynamic mapping state=%v, want Dynamic", got)
	}
	duplicate := Resolve(&Inputs{Values: map[string]Input{
		"name": {Name: text("name"), Value: text("first")},
		"NAME": {Name: text("NAME"), Value: text("second")},
	}})
	if got := duplicate.Lookup("name").State(); got != value.Dynamic {
		t.Fatalf("duplicate key lookup state=%v, want Dynamic", got)
	}
	if !duplicate.Complete() {
		t.Fatal("known duplicate must not make unrelated names globally unknown")
	}
	if got, ok := duplicate.Lookup("other").Text(); !ok || got != "" {
		if got := duplicate.Lookup("other").State(); got != value.Missing {
			t.Fatalf("unrelated absent key state=%v, want Missing", got)
		}
	}
	if got := (Mapping{}).Lookup("missing").State(); got != value.Missing {
		t.Fatalf("zero Mapping lookup state=%v, want Missing", got)
	}
}

func TestMappingIterationPreservesCaseDistinctNames(t *testing.T) {
	resolved := Resolve(&Inputs{Values: map[string]Input{
		"PATH": {Name: text("PATH"), Value: text("upper")},
		"path": {Name: text("path"), Value: text("lower")},
	}})
	seen := map[string]string{}
	for key, item := range resolved.KeyedEntries() {
		name, _ := key.ScalarString()
		value, _ := item.ScalarString()
		seen[name] = value
	}
	if seen["PATH"] != "upper" || seen["path"] != "lower" || len(seen) != 2 {
		t.Fatalf("case-distinct mapping entries=%v", seen)
	}
	if got := resolved.Lookup("PATH").State(); got != value.Dynamic {
		t.Fatalf("case-folded ambiguous lookup=%v, want Dynamic", got)
	}
}

func TestEvaluatedExpressionKeyRemainsDataAndResolutionIsDetached(t *testing.T) {
	props := []value.Prop{{Name: "${{ inputs.name }}", Value: value.FromJSON("old", value.Origin{})}}
	object := value.Map(value.Origin{}, props...)
	resolved := Resolve(&Inputs{Expression: object})
	props[0].Name = "changed"
	if got, ok := resolved.Lookup("${{ inputs.name }}").Text(); !ok || got != "old" {
		t.Fatalf("expression-looking key was evaluated or source mutation leaked: %q, %v", got, ok)
	}
	if got := resolved.Lookup("changed").State(); got != value.Missing {
		t.Fatalf("mutated source key leaked into mapping: %v", got)
	}
}

func TestKeyedEntriesKeepKeyAndValueOriginsSeparate(t *testing.T) {
	keyOrigin := value.Origin{Span: value.Span{Start: diagnostic.Position{Line: 4, Column: 3}}}
	itemOrigin := value.Origin{Span: value.Span{Start: diagnostic.Position{Line: 4, Column: 10}}}
	resolved := Resolve(&Inputs{Values: map[string]Input{
		"Name": {Name: value.YAMLScalar("Name", "!!str", keyOrigin), Value: value.YAMLScalar("source", "!!str", itemOrigin)},
	}})
	for key, item := range resolved.KeyedEntries() {
		if name, ok := key.Text(); !ok || name != "Name" {
			t.Fatalf("canonical key=%q known=%v", name, ok)
		}
		if key.Origin().Span.Start != keyOrigin.Span.Start {
			t.Fatalf("key origin=%+v, want %+v", key.Origin().Span.Start, keyOrigin.Span.Start)
		}
		if item.Origin().Span.Start != itemOrigin.Span.Start {
			t.Fatalf("value origin=%+v, want %+v", item.Origin().Span.Start, itemOrigin.Span.Start)
		}
		return
	}
	t.Fatal("resolved mapping yielded no keyed entries")
}

func TestDeclarationsPreserveUnknownInvalidAndSourceKeys(t *testing.T) {
	literalOrigin := value.Origin{Span: value.Span{Start: diagnostic.Position{Line: 2, Column: 7}}}
	constantOrigin := value.Origin{Span: value.Span{Start: diagnostic.Position{Line: 3, Column: 7}}, FromExpression: true}
	runtimeOrigin := value.Origin{Span: value.Span{Start: diagnostic.Position{Line: 4, Column: 7}}, FromExpression: true}
	invalidOrigin := value.Origin{Span: value.Span{Start: diagnostic.Position{Line: 5, Column: 7}}}
	objectOrigin := value.Origin{Span: value.Span{Start: diagnostic.Position{Line: 6, Column: 12}}, FromExpression: true}
	object := value.FromJSON(map[string]any{"derived": "data"}, objectOrigin)
	resolved := Resolve(&Inputs{
		Values: map[string]Input{
			"literal":  {Name: value.YAMLScalar("literal", "!!str", literalOrigin), Value: text("one"), Source: "literal"},
			"constant": {Name: value.FromJSON("constant", constantOrigin), Value: text("two"), Source: "${{ 'constant' }}"},
			"runtime":  {Name: value.DynamicValue("${{ inputs.key }}", value.KindUnknown, runtimeOrigin), Value: text("three"), Source: "${{ inputs.key }}"},
			"invalid":  {Name: value.FromJSON([]any{"not-a-scalar"}, invalidOrigin), Value: text("four"), Source: "[not-a-scalar]"},
		},
		Expression: object,
	})
	if resolved.Complete() {
		t.Fatal("unknown and invalid declarations must make mapping incomplete")
	}
	declarations := map[string]Declaration{}
	for declaration := range resolved.Declarations() {
		key := declaration.Source
		if declaration.Evaluated {
			key = "evaluated:" + declaration.Name
		}
		declarations[key] = declaration
	}
	for source, name := range map[string]string{"literal": "literal", "${{ 'constant' }}": "constant"} {
		declaration, ok := declarations[source]
		if !ok || declaration.State != KnownKey || declaration.Name != name {
			t.Errorf("declaration %q = %#v, %v; want known source declaration", source, declaration, ok)
		}
	}
	if got := declarations["${{ inputs.key }}"]; got.State != UnknownKey || got.Name != "" || got.Origin.Span.Start != runtimeOrigin.Span.Start {
		t.Errorf("runtime declaration = %#v; want UnknownKey and key origin", got)
	}
	if got := declarations["[not-a-scalar]"]; got.State != InvalidKey || got.Name != "" || got.Origin.Span.Start != invalidOrigin.Span.Start {
		t.Errorf("invalid declaration = %#v; want InvalidKey and key origin", got)
	}
	if got := declarations["evaluated:derived"]; got.State != KnownKey || !got.Evaluated || got.Source != "derived" || got.Origin.Span.Start != objectOrigin.Span.Start {
		t.Errorf("evaluated declaration = %#v; want evaluated key text and source expression origin", got)
	}
	known := 0
	for key := range resolved.KeyedEntries() {
		if key.State() != value.Literal && key.State() != value.Evaluated {
			t.Errorf("lookup entries included unresolved key state %v", key.State())
		}
		known++
	}
	if known != 3 {
		t.Fatalf("known lookup entries = %d; want literal, constant, and evaluated-object keys", known)
	}
}

func TestDynamicWholeMappingMarksIncompleteWithoutInventingDeclaration(t *testing.T) {
	resolved := Resolve(&Inputs{Expression: value.DynamicValue("${{ inputs.mapping }}", value.KindUnknown, value.Origin{})})
	if resolved.Complete() {
		t.Fatal("dynamic whole mapping must be incomplete")
	}
	for declaration := range resolved.Declarations() {
		t.Fatalf("dynamic whole mapping invented declaration: %#v", declaration)
	}
}

func text(source string) value.Value {
	return value.YAMLScalar(source, "!!str", value.Origin{})
}
