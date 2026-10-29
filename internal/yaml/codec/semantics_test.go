package codec_test

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	"actionlint.kjanat.dev/v2/internal/yaml/codec"
	"actionlint.kjanat.dev/v2/internal/yaml/syntax"
	upstream "github.com/kjanat/go-yaml/v4"
)

func decodeCursorSource(data []byte, target any) error {
	root, err := codec.Parse(data)
	if err != nil {
		return err
	}
	session, cursor, err := codec.Open(root)
	if err != nil {
		return err
	}
	defer session.Close()
	return session.Finish(cursor.Decode(target))
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

type plainPrefilled struct {
	Keep  string         `yaml:"keep"`
	Items map[string]int `yaml:"items"`
	Value *string        `yaml:"value"`
	Hook  string         `yaml:"hook"`
}

func TestCursorDecodeMatchesUpstreamForPrefilledTargets(t *testing.T) {
	for _, selected := range []bool{false, true} {
		t.Run(fmt.Sprintf("Select=%t", selected), func(t *testing.T) {
			const source = "hook: seen\nitems: {new: 2}\nvalue: null\n"
			oldValue, baselineValue := "old", "old"
			got := plainPrefilled{Keep: "preserve", Items: map[string]int{"old": 1}, Value: &oldValue}
			want := plainPrefilled{Keep: "preserve", Items: map[string]int{"old": 1}, Value: &baselineValue}
			if err := decodePrefilledSource(t, []byte(source), &got, selected); err != nil {
				t.Fatal(err)
			}
			if err := upstream.Unmarshal([]byte(source), &want); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) || got.Keep != "preserve" || got.Value != nil || !reflect.DeepEqual(got.Items, map[string]int{"old": 1, "new": 2}) || got.Hook != "seen" {
				t.Fatalf("got %#v, upstream %#v", got, want)
			}
		})
	}
}

func TestCursorEnterKeepsCallerAliasTargetIdentity(t *testing.T) {
	const source = "root:\n  anchor: &payload\n    value: ok\n  item: *payload\n"
	root, err := codec.Parse([]byte(source))
	if err != nil {
		t.Fatal(err)
	}
	rootNode := mappingValue(root, "root")
	anchor := mappingValue(rootNode, "anchor")
	alias := mappingValue(rootNode, "item")
	if anchor == nil || alias == nil || alias.Kind != syntax.AliasNode || alias.Alias != anchor {
		t.Fatalf("invalid alias graph: anchor=%p alias=%#v", anchor, alias)
	}
	session, cursor, err := codec.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	var selected struct {
		Root codec.Selected `yaml:"root"`
	}
	outerVisits := 0
	innerVisits := map[string]int{}
	err = cursor.Select(&selected, func(_ *codec.Selected, occurrence codec.Cursor) error {
		outerVisits++
		var inner struct {
			Anchor codec.Selected `yaml:"anchor"`
			Item   codec.Selected `yaml:"item"`
		}
		return occurrence.Select(&inner, func(field *codec.Selected, value codec.Cursor) error {
			want := anchor
			switch field {
			case &inner.Anchor:
				innerVisits["anchor"]++
			case &inner.Item:
				innerVisits["item"]++
				want = alias
			default:
				t.Error("unknown selected field")
			}
			if value.Node() != want {
				t.Errorf("occurrence node=%p want original %p", value.Node(), want)
			}
			entered, err := value.Enter()
			if err != nil {
				return err
			}
			if entered.Node() != anchor {
				t.Errorf("entered node=%p want anchor %p", entered.Node(), anchor)
			}
			return nil
		})
	})
	if err := session.Finish(err); err != nil {
		t.Fatal(err)
	}
	if outerVisits != 1 || innerVisits["anchor"] != 1 || innerVisits["item"] != 1 {
		t.Errorf("outer visits=%d inner=%v want exactly root/anchor/item", outerVisits, innerVisits)
	}
}

func TestCursorCollectionErrorsKeepMarksAndPartialValues(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		target       func() any
	}{
		{"slice", "items:\n  - first\n  - 3\n  - second\n  - 4\n", func() any {
			return &struct {
				Items []int `yaml:"items"`
			}{}
		}},
		{"map", "items:\n  first: one\n  good: 3\n  second: two\n  last: 4\n", func() any {
			return &struct {
				Items map[string]int `yaml:"items"`
			}{}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, want := tc.target(), tc.target()
			gotErr := decodeCursorSource([]byte(tc.source), got)
			wantErr := upstream.Unmarshal([]byte(tc.source), want)
			assertConstructErrorsMatchUpstream(t, gotErr, wantErr)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("partial values=%#v upstream=%#v", got, want)
			}
		})
	}
}

func assertConstructErrorsMatchUpstream(t *testing.T, got, want error) {
	t.Helper()
	decoded, gotOK := errors.AsType[*codec.DecodeError](got)
	baseline, wantOK := errors.AsType[*upstream.LoadErrors](want)
	if !gotOK || !wantOK {
		t.Fatalf("errors codec=%T %v upstream=%T %v", got, got, want, want)
	}
	if got.Error() != want.Error() {
		t.Errorf("error=%q upstream=%q", got.Error(), want.Error())
	}
	if len(decoded.Errors) != len(baseline.Errors) {
		t.Fatalf("errors=%d upstream=%d", len(decoded.Errors), len(baseline.Errors))
	}
	for i, item := range decoded.Errors {
		other := baseline.Errors[i]
		if item.Message != other.Message || item.Mark.Line != other.Mark.Line || item.Mark.Column != other.Mark.Column {
			t.Errorf("diagnostic %d=%+v upstream=%+v", i, item, other)
		}
	}
}

func TestCursorRootNullKeepsPrefilledValue(t *testing.T) {
	type value struct{ Value string }
	got, want := value{"preserve"}, value{"preserve"}
	if err := decodeCursorSource([]byte("null\n"), &got); err != nil {
		t.Fatal(err)
	}
	if err := upstream.Unmarshal([]byte("null\n"), &want); err != nil {
		t.Fatal(err)
	}
	if got != want || got.Value != "preserve" {
		t.Fatalf("got %#v upstream %#v", got, want)
	}
}

func TestCursorDecodePreservesAndClearsPrefilledPointer(t *testing.T) {
	type nested struct {
		Name  string `yaml:"name"`
		Value string `yaml:"hook"`
	}
	type target struct {
		Nested *nested `yaml:"nested"`
		Hook   string  `yaml:"hook"`
	}
	for _, tc := range []struct {
		name, source string
		present      bool
	}{
		{"absent pointer preserves identity", "hook: outer\n", true},
		{"explicit null clears pointer", "nested: null\nhook: outer\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, selected := range []bool{false, true} {
				t.Run(fmt.Sprintf("Select=%t", selected), func(t *testing.T) {
					original, baseline := &nested{"keep", "inner"}, &nested{"keep", "inner"}
					got, want := target{Nested: original}, target{Nested: baseline}
					if err := decodePrefilledSource(t, []byte(tc.source), &got, selected); err != nil {
						t.Fatal(err)
					}
					if err := upstream.Unmarshal([]byte(tc.source), &want); err != nil {
						t.Fatal(err)
					}
					if !reflect.DeepEqual(got, want) {
						t.Errorf("got=%#v upstream=%#v", got, want)
					}
					if tc.present {
						if got.Nested != original || got.Nested == nil || got.Nested.Name != "keep" || got.Nested.Value != "inner" {
							t.Errorf("absent pointer=%#v want original %p and state", got.Nested, original)
						}
						if want.Nested != baseline || want.Nested == nil || want.Nested.Name != "keep" {
							t.Error("upstream absent pointer behavior changed")
						}
					} else if got.Nested != nil || want.Nested != nil {
						t.Errorf("null pointers codec=%#v upstream=%#v want nil", got.Nested, want.Nested)
					}
					if got.Hook != "outer" {
						t.Errorf("decoded hook field=%q", got.Hook)
					}
				})
			}
		})
	}
}

func TestParseRecursiveAliasKeepsGraphAndCursorDecodeFails(t *testing.T) {
	root, err := codec.Parse([]byte("&recursive [*recursive]\n"))
	if err != nil {
		t.Fatal(err)
	}
	if root.Kind != syntax.DocumentNode || len(root.Content) != 1 {
		t.Fatalf("root=%#v", root)
	}
	sequence := root.Content[0]
	if sequence.Kind != syntax.SequenceNode || len(sequence.Content) != 1 || sequence.Content[0].Alias != sequence {
		t.Fatalf("recursive graph=%#v", sequence)
	}
	session, cursor, err := codec.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	var value, baseline []any
	gotErr := cursor.Decode(&value)
	wantErr := upstream.Unmarshal([]byte("&recursive [*recursive]\n"), &baseline)
	compareCycleFailure(t, gotErr, wantErr)
	if !reflect.DeepEqual(value, baseline) {
		t.Errorf("partial value=%#v upstream=%#v", value, baseline)
	}
	if root.Content[0] != sequence || sequence.Content[0].Kind != syntax.AliasNode || sequence.Content[0].Alias != sequence {
		t.Fatal("decode changed source graph")
	}
}

func TestCursorPrefilledMapMergesNewValues(t *testing.T) {
	type target struct {
		Items map[string]string `yaml:"items"`
	}
	got, want := target{map[string]string{"old": "keep"}}, target{map[string]string{"old": "keep"}}
	const source = "items: {new: fresh}\n"
	if err := decodeCursorSource([]byte(source), &got); err != nil {
		t.Fatal(err)
	}
	if err := upstream.Unmarshal([]byte(source), &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(got.Items, map[string]string{"old": "keep", "new": "fresh"}) {
		t.Fatalf("got=%#v upstream=%#v", got, want)
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
	gotRoot, err := codec.Parse([]byte(source))
	if err != nil {
		t.Fatalf("codec.Parse fixture: %v", err)
	}
	got := mappingValue(gotRoot, "value")
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

func TestCursorOpensAliasOccurrenceDirectly(t *testing.T) {
	root, err := codec.Parse([]byte("anchor: &payload value\nitem: *payload\n"))
	if err != nil {
		t.Fatal(err)
	}
	anchor, alias := mappingValue(root, "anchor"), mappingValue(root, "item")
	if anchor == nil || alias == nil || alias.Alias != anchor {
		t.Fatal("invalid fixture")
	}
	session, cursor, err := codec.Open(alias)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if cursor.Node() != alias {
		t.Fatal("direct cursor lost alias occurrence")
	}
	entered, err := cursor.Enter()
	if err != nil {
		t.Fatal(err)
	}
	if entered.Node() != anchor {
		t.Errorf("entered=%p anchor=%p", entered.Node(), anchor)
	}
	var value string
	if err := entered.Decode(&value); err != nil {
		t.Fatal(err)
	}
	if value != "value" {
		t.Errorf("decoded alias=%q", value)
	}
	if alias.Alias != anchor || alias.Kind != syntax.AliasNode {
		t.Fatal("alias changed")
	}
}

func TestParseCommentsMatchUpstreamThroughSelectedBoundary(t *testing.T) {
	const source = "# document head\nroot:\n  # value head\n  value: text # value line\n  # value foot\n\n# root foot\n"
	root, err := codec.Parse([]byte(source))
	if err != nil {
		t.Fatal(err)
	}
	var baseline upstream.Node
	if err := upstream.Unmarshal([]byte(source), &baseline); err != nil {
		t.Fatal(err)
	}
	var compare func(*syntax.Node, *upstream.Node)
	commentKinds := map[string]int{}
	compare = func(got *syntax.Node, want *upstream.Node) {
		if got.HeadComment != want.HeadComment || got.LineComment != want.LineComment || got.FootComment != want.FootComment || got.Tag != want.Tag || got.Line != want.Line || got.Column != want.Column {
			t.Errorf("node=%#v upstream=%#v", got, want)
		}
		if want.HeadComment != "" {
			commentKinds["head"]++
		}
		if want.LineComment != "" {
			commentKinds["line"]++
		}
		if want.FootComment != "" {
			commentKinds["foot"]++
		}
		if len(got.Content) != len(want.Content) {
			t.Fatalf("content=%d upstream=%d", len(got.Content), len(want.Content))
		}
		for i, child := range got.Content {
			compare(child, want.Content[i])
		}
	}
	compare(root, &baseline)
	for _, kind := range []string{"head", "line", "foot"} {
		if commentKinds[kind] == 0 {
			t.Fatalf("fixture has no %s comment", kind)
		}
	}
	session, cursor, err := codec.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	var target struct {
		Root codec.Selected `yaml:"root"`
	}
	visits := 0
	err = cursor.Select(&target, func(_ *codec.Selected, value codec.Cursor) error {
		visits++
		entered, err := value.Enter()
		if err != nil {
			return err
		}
		if entered.Node() != mappingValue(root, "root") {
			t.Error("selected mapping lost identity")
		}
		compare(entered.Node(), upstreamMappingValue(&baseline, "root"))
		return nil
	})
	if err := session.Finish(err); err != nil {
		t.Fatal(err)
	}
	if visits != 1 {
		t.Errorf("selected visits=%d want 1", visits)
	}
}
