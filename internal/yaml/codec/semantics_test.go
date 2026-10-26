package codec_test

import (
	"errors"
	"reflect"
	"testing"

	"actionlint.kjanat.dev/v2/internal/yaml/codec"
	"actionlint.kjanat.dev/v2/internal/yaml/syntax"
	upstream "go.yaml.in/yaml/v4"
)

type codecNodeHook struct {
	Called bool
	Node   *syntax.Node
	Value  string
}

func (h *codecNodeHook) UnmarshalYAML(_ *codec.Decoder, node *syntax.Node) error {
	h.Called = true
	h.Node = node
	h.Value = node.Value
	return nil
}

type upstreamNodeHook struct {
	Called bool
	Value  string
}

func (h *upstreamNodeHook) UnmarshalYAML(node *upstream.Node) error {
	h.Called = true
	h.Value = node.Value
	return nil
}

type codecPrefilled struct {
	Keep  string         `yaml:"keep"`
	Items map[string]int `yaml:"items"`
	Value *string        `yaml:"value"`
	Hook  codecNodeHook  `yaml:"hook"`
}

type upstreamPrefilled struct {
	Keep  string           `yaml:"keep"`
	Items map[string]int   `yaml:"items"`
	Value *string          `yaml:"value"`
	Hook  upstreamNodeHook `yaml:"hook"`
}

func TestShadowDecodeMatchesUpstreamForPrefilledTargets(t *testing.T) {
	const source = "hook: seen\nitems: {new: 2}\nvalue: null\n"
	oldValue := "old"
	wantItems := map[string]int{"old": 1, "new": 2}

	got := codecPrefilled{Keep: "preserve", Items: map[string]int{"old": 1}, Value: &oldValue}
	if err := codec.Unmarshal([]byte(source), &got); err != nil {
		t.Fatalf("codec.Unmarshal: %v", err)
	}

	baselineValue := "old"
	baseline := upstreamPrefilled{Keep: "preserve", Items: map[string]int{"old": 1}, Value: &baselineValue}
	if err := upstream.Unmarshal([]byte(source), &baseline); err != nil {
		t.Fatalf("upstream.Unmarshal baseline: %v", err)
	}
	if got.Keep != baseline.Keep || got.Keep != "preserve" {
		t.Errorf("absent field = %q, upstream baseline %q; want preserved value", got.Keep, baseline.Keep)
	}
	if !reflect.DeepEqual(got.Items, baseline.Items) || !reflect.DeepEqual(got.Items, wantItems) {
		t.Errorf("merged map = %#v, upstream baseline %#v; want %#v", got.Items, baseline.Items, wantItems)
	}
	if got.Value != baseline.Value || got.Value != nil {
		t.Errorf("null pointer = %#v, upstream baseline %#v; want nil", got.Value, baseline.Value)
	}
	if !got.Hook.Called || !baseline.Hook.Called || got.Hook.Value != baseline.Hook.Value {
		t.Errorf("hook result = %#v, upstream baseline %#v", got.Hook, baseline.Hook)
	}
}

type aliasReceivers struct {
	Anchor codecNodeHook `yaml:"anchor"`
	Item   codecNodeHook `yaml:"item"`
}

type aliasRoot struct {
	Root aliasReceivers `yaml:"root"`
}

func mappingValue(node *syntax.Node, name string) *syntax.Node {
	if node.Kind == syntax.DocumentNode && len(node.Content) > 0 {
		node = node.Content[0]
	}
	if node.Kind != syntax.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == name {
			return node.Content[i+1]
		}
	}
	return nil
}

func TestHookReceivesNodeFromCallerGraphIncludingAliasTarget(t *testing.T) {
	const source = "root:\n  anchor: &payload\n    value: ok\n  item: *payload\n"
	var root syntax.Node
	if err := codec.Unmarshal([]byte(source), &root); err != nil {
		t.Fatalf("decode syntax graph: %v", err)
	}
	rootNode := mappingValue(&root, "root")
	anchor := mappingValue(rootNode, "anchor")
	alias := mappingValue(rootNode, "item")
	if anchor == nil || alias == nil || alias.Kind != syntax.AliasNode || alias.Alias != anchor {
		t.Fatalf("syntax alias graph is not linked to anchor: anchor=%p alias=%#v", anchor, alias)
	}

	var got aliasRoot
	if err := codec.Decode(&root, &got); err != nil {
		t.Fatalf("codec.Decode: %v", err)
	}
	if got.Root.Anchor.Node != anchor {
		t.Errorf("anchor hook node = %p, source graph node = %p", got.Root.Anchor.Node, anchor)
	}
	if got.Root.Item.Node != anchor {
		t.Errorf("alias hook node = %p, alias target in source graph = %p", got.Root.Item.Node, anchor)
	}
	if got.Root.Anchor.Node != got.Root.Item.Node {
		t.Errorf("hooks on anchor and alias received distinct graph nodes: %p and %p", got.Root.Anchor.Node, got.Root.Item.Node)
	}
}

type rejectingHook struct{ Value string }

func (h *rejectingHook) UnmarshalYAML(_ *codec.Decoder, node *syntax.Node) error {
	h.Value = node.Value
	return errors.New("reject " + node.Value)
}

type rejectingSlice struct {
	Items []rejectingHook `yaml:"items"`
}

type rejectingMap struct {
	Items map[string]rejectingHook `yaml:"items"`
}

func errorMarks(err error) ([]codec.Mark, error) {
	var decodeErr *codec.DecodeError
	if !errors.As(err, &decodeErr) {
		return nil, err
	}
	marks := make([]codec.Mark, 0, len(decodeErr.Errors))
	for _, item := range decodeErr.Errors {
		marks = append(marks, item.Mark)
	}
	return marks, nil
}

func TestSliceHookErrorsCollectMarksAndValues(t *testing.T) {
	const source = "items:\n  - first\n  - second\n"
	var root syntax.Node
	if err := codec.Unmarshal([]byte(source), &root); err != nil {
		t.Fatalf("decode syntax graph: %v", err)
	}
	values := mappingValue(&root, "items")
	wantMarks := []codec.Mark{{Line: values.Content[0].Line, Column: values.Content[0].Column}, {Line: values.Content[1].Line, Column: values.Content[1].Column}}

	var got rejectingSlice
	err := codec.Decode(&root, &got)
	marks, nonDecodeError := errorMarks(err)
	if nonDecodeError != nil {
		t.Fatalf("codec.Decode error = %v, want collected DecodeError", err)
	}
	if !reflect.DeepEqual(marks, wantMarks) {
		t.Errorf("hook error marks = %#v, want %#v", marks, wantMarks)
	}
	if len(got.Items) != 2 || got.Items[0].Value != "first" || got.Items[1].Value != "second" {
		t.Errorf("slice values = %#v, want both decoded values", got.Items)
	}
}

func TestMapHookErrorsCollectMarksAndValues(t *testing.T) {
	const source = "items:\n  first: one\n  second: two\n"
	var root syntax.Node
	if err := codec.Unmarshal([]byte(source), &root); err != nil {
		t.Fatalf("decode syntax graph: %v", err)
	}
	values := mappingValue(&root, "items")
	wantMarks := []codec.Mark{{Line: values.Content[1].Line, Column: values.Content[1].Column}, {Line: values.Content[3].Line, Column: values.Content[3].Column}}

	var got rejectingMap
	err := codec.Decode(&root, &got)
	marks, nonDecodeError := errorMarks(err)
	if nonDecodeError != nil {
		t.Fatalf("codec.Decode error = %v, want collected DecodeError", err)
	}
	if !reflect.DeepEqual(marks, wantMarks) {
		t.Errorf("hook error marks = %#v, want %#v", marks, wantMarks)
	}
	if len(got.Items) != 2 || got.Items["first"].Value != "one" || got.Items["second"].Value != "two" {
		t.Errorf("map values = %#v, want both decoded values", got.Items)
	}
}

type rootNullHook struct {
	Called bool
	Value  string
}

func (h *rootNullHook) UnmarshalYAML(_ *codec.Decoder, node *syntax.Node) error {
	h.Called = true
	h.Value = node.Value
	return nil
}

type upstreamRootNullHook struct {
	Called bool
	Value  string
}

func (h *upstreamRootNullHook) UnmarshalYAML(node *upstream.Node) error {
	h.Called = true
	h.Value = node.Value
	return nil
}

func TestRootNullDoesNotCallHookOrChangeTarget(t *testing.T) {
	got := rootNullHook{Value: "preserve"}
	if err := codec.Unmarshal([]byte("null\n"), &got); err != nil {
		t.Fatalf("codec.Unmarshal: %v", err)
	}
	baseline := upstreamRootNullHook{Value: "preserve"}
	if err := upstream.Unmarshal([]byte("null\n"), &baseline); err != nil {
		t.Fatalf("upstream.Unmarshal baseline: %v", err)
	}
	if got.Called || got.Value != "preserve" {
		t.Errorf("codec root null hook = %#v, want unchanged target with no call", got)
	}
	if got.Called != baseline.Called || got.Value != baseline.Value {
		t.Errorf("codec root null result %#v differs from upstream baseline %#v", got, baseline)
	}
}

func TestRecursiveAliasPreservesGraphAndDecodeReturns(t *testing.T) {
	const source = "&recursive [*recursive]\n"
	var root syntax.Node
	if err := codec.Unmarshal([]byte(source), &root); err != nil {
		t.Fatalf("decode recursive alias graph: %v", err)
	}
	if root.Kind != syntax.DocumentNode || len(root.Content) != 1 {
		t.Fatalf("root = %#v, want one document node", root)
	}
	sequence := root.Content[0]
	if sequence.Kind != syntax.SequenceNode || len(sequence.Content) != 1 {
		t.Fatalf("sequence = %#v, want one alias", sequence)
	}
	alias := sequence.Content[0]
	if alias.Kind != syntax.AliasNode || alias.Alias != sequence {
		t.Fatalf("recursive alias = %#v, want reference to sequence %p", alias, sequence)
	}
	var decoded []any
	if err := codec.Decode(&root, &decoded); err == nil {
		t.Fatal("recursive value alias decoded successfully, want cycle error")
	}
}

type codecNestedHook struct {
	Name string        `yaml:"name"`
	Hook codecNodeHook `yaml:"hook"`
}

type upstreamNestedHook struct {
	Name string           `yaml:"name"`
	Hook upstreamNodeHook `yaml:"hook"`
}

type codecPointerRoot struct {
	Nested *codecNestedHook `yaml:"nested"`
	Hook   codecNodeHook    `yaml:"hook"`
}

type upstreamPointerRoot struct {
	Nested *upstreamNestedHook `yaml:"nested"`
	Hook   upstreamNodeHook    `yaml:"hook"`
}

func TestShadowDecodePreservesAndClearsPointerToHookStruct(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		wantPresent  bool
	}{
		{name: "absent pointer preserves identity", source: "hook: outer\n", wantPresent: true},
		{name: "explicit null clears pointer", source: "nested: null\nhook: outer\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			oldNested := &codecNestedHook{Name: "keep", Hook: codecNodeHook{Value: "inner"}}
			got := codecPointerRoot{Nested: oldNested}
			if err := codec.Unmarshal([]byte(tc.source), &got); err != nil {
				t.Fatalf("codec.Unmarshal: %v", err)
			}

			baselineNested := &upstreamNestedHook{Name: "keep", Hook: upstreamNodeHook{Value: "inner"}}
			baseline := upstreamPointerRoot{Nested: baselineNested}
			if err := upstream.Unmarshal([]byte(tc.source), &baseline); err != nil {
				t.Fatalf("upstream.Unmarshal baseline: %v", err)
			}
			if tc.wantPresent {
				if got.Nested != oldNested || got.Nested == nil || got.Nested.Name != "keep" {
					t.Errorf("absent nested pointer = %#v, original %p; want same pointer and value", got.Nested, oldNested)
				}
				if baseline.Nested != baselineNested || baseline.Nested == nil || baseline.Nested.Name != "keep" {
					t.Errorf("upstream absent nested pointer = %#v, original %p; baseline behavior changed", baseline.Nested, baselineNested)
				}
			} else if got.Nested != nil || baseline.Nested != nil {
				t.Errorf("explicit null nested pointer = %#v, upstream baseline %#v; want nil", got.Nested, baseline.Nested)
			}
			if !got.Hook.Called || !baseline.Hook.Called || got.Hook.Value != baseline.Hook.Value {
				t.Errorf("outer hook result = %#v, upstream baseline %#v", got.Hook, baseline.Hook)
			}
		})
	}
}

func TestDecodeAliasDirectlyIntoHook(t *testing.T) {
	const source = "anchor: &payload value\nitem: *payload\n"
	var root syntax.Node
	if err := codec.Unmarshal([]byte(source), &root); err != nil {
		t.Fatalf("decode syntax graph: %v", err)
	}
	anchor := mappingValue(&root, "anchor")
	alias := mappingValue(&root, "item")
	if anchor == nil || alias == nil || alias.Kind != syntax.AliasNode || alias.Alias != anchor {
		t.Fatalf("syntax alias graph is not linked to anchor: anchor=%p alias=%#v", anchor, alias)
	}
	var got codecNodeHook
	if err := codec.Decode(alias, &got); err != nil {
		t.Fatalf("Decode(alias, hook): %v", err)
	}
	if !got.Called || got.Node != anchor || got.Value != "value" {
		t.Errorf("alias hook = %#v, source anchor = %p; want original anchor", got, anchor)
	}

	for _, tc := range []struct {
		name string
		node *syntax.Node
	}{
		{name: "missing target", node: &syntax.Node{Kind: syntax.AliasNode, Line: 4, Column: 2}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var hook codecNodeHook
			if err := codec.Decode(tc.node, &hook); err == nil {
				t.Fatal("Decode invalid alias succeeded, want error")
			}
			if hook.Called {
				t.Fatal("invalid alias called hook")
			}
		})
	}

	t.Run("looping target", func(t *testing.T) {
		loop := &syntax.Node{Kind: syntax.AliasNode, Line: 5, Column: 3}
		loop.Alias = loop
		var hook codecNodeHook
		if err := codec.Decode(loop, &hook); err == nil {
			t.Fatal("Decode looping alias succeeded, want error")
		}
		if hook.Called {
			t.Fatal("looping alias called hook")
		}
	})
}

type codecHookMap struct {
	Items map[string]codecNodeHook `yaml:"items"`
}

func TestPrefilledMapOfHookValuesMergesAndCallsNewHook(t *testing.T) {
	const source = "items: {new: fresh}\n"
	got := codecHookMap{Items: map[string]codecNodeHook{"old": {Called: true, Value: "keep"}}}
	if err := codec.Unmarshal([]byte(source), &got); err != nil {
		t.Fatalf("codec.Unmarshal: %v", err)
	}
	if len(got.Items) != 2 {
		t.Fatalf("map entries = %#v, want old and new", got.Items)
	}
	if old := got.Items["old"]; !old.Called || old.Value != "keep" {
		t.Errorf("existing entry = %#v, want unchanged", old)
	}
	if fresh := got.Items["new"]; !fresh.Called || fresh.Value != "fresh" || fresh.Node == nil {
		t.Errorf("new entry = %#v, want invoked hook with source node", fresh)
	}
}

func upstreamMappingValue(node *upstream.Node, name string) *upstream.Node {
	if node.Kind == upstream.DocumentNode && len(node.Content) > 0 {
		node = node.Content[0]
	}
	if node.Kind != upstream.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == name {
			return node.Content[i+1]
		}
	}
	return nil
}

func TestExplicitVerbatimTagAndCoordinatesMatchUpstream(t *testing.T) {
	const source = "value: !<tag:yaml.org,2002:str> 12\n"
	var wantRoot upstream.Node
	if err := upstream.Unmarshal([]byte(source), &wantRoot); err != nil {
		t.Fatalf("upstream.Unmarshal fixture: %v", err)
	}
	want := upstreamMappingValue(&wantRoot, "value")
	if want == nil {
		t.Fatal("upstream fixture has no value node")
	}
	var gotRoot syntax.Node
	if err := codec.Unmarshal([]byte(source), &gotRoot); err != nil {
		t.Fatalf("codec.Unmarshal fixture: %v", err)
	}
	got := mappingValue(&gotRoot, "value")
	if got == nil {
		t.Fatal("codec fixture has no value node")
	}
	if got.Tag != "!!str" || got.Tag != want.Tag || got.Value != want.Value || got.Line != want.Line || got.Column != want.Column {
		t.Errorf("verbatim tag node = %#v, upstream = %#v; want normalized tag, value, and coordinates preserved", got, want)
	}
	if got.Style != syntax.TaggedStyle || want.Style != upstream.TaggedStyle {
		t.Errorf("verbatim tag style = %b, upstream = %b; want explicit tag style preserved", got.Style, want.Style)
	}
}
