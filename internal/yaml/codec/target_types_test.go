package codec_test

import (
	"reflect"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/yaml/codec"
	"actionlint.kjanat.dev/v2/internal/yaml/syntax"
	upstream "go.yaml.in/yaml/v4"
)

type recursiveValue struct {
	Name     string            `yaml:"name"`
	Children []*recursiveValue `yaml:"children"`
}

func TestCursorPlainRecursiveTypeMatchesUpstream(t *testing.T) {
	const source = "name: root\nchildren:\n  - name: child\n    children:\n      - name: leaf\n"
	var got recursiveValue
	if err := decodeCursorSource([]byte(source), &got); err != nil {
		t.Fatalf("decodeCursorSource: %v", err)
	}
	var want recursiveValue
	if err := upstream.Unmarshal([]byte(source), &want); err != nil {
		t.Fatalf("upstream.Unmarshal baseline: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("codec value = %#v, upstream baseline = %#v", got, want)
	}
}

type ExportedMethodEmbed struct {
	Value string `yaml:"value"`
}

func (*ExportedMethodEmbed) Marker() {}

type exportedEmbeddedTarget struct {
	Prefix              string `yaml:"prefix"`
	ExportedMethodEmbed `yaml:",inline"`
	Hook                string `yaml:"hook"`
}

type upstreamExportedEmbeddedTarget struct {
	Prefix              string `yaml:"prefix"`
	ExportedMethodEmbed `yaml:",inline"`
	Hook                string `yaml:"hook"`
}

func TestCursorExportedEmbeddedMethodTypeMatchesUpstream(t *testing.T) {
	const source = "prefix: before\nvalue: embedded\nhook: called\n"
	var got exportedEmbeddedTarget
	if err := decodeCursorSource([]byte(source), &got); err != nil {
		t.Fatalf("decodeCursorSource: %v", err)
	}
	var want upstreamExportedEmbeddedTarget
	if err := upstream.Unmarshal([]byte(source), &want); err != nil {
		t.Fatalf("upstream.Unmarshal baseline: %v", err)
	}
	if got.Prefix != want.Prefix || got.Value != want.Value || got.Hook != want.Hook {
		t.Fatalf("codec embedded value = %#v, upstream baseline = %#v", got, want)
	}
}

type privateEmbedded struct {
	Value string `yaml:"value"`
}

type privateEmbeddedTarget struct {
	privateEmbedded `yaml:",inline"`
	Hook            string `yaml:"hook"`
}

type upstreamPrivateEmbeddedTarget struct {
	privateEmbedded `yaml:",inline"`
	Hook            string `yaml:"hook"`
}

func TestCursorUnexportedEmbeddedFieldMatchesUpstream(t *testing.T) {
	const source = "value: embedded\nhook: called\n"
	var got privateEmbeddedTarget
	if err := decodeCursorSource([]byte(source), &got); err != nil {
		t.Fatalf("decodeCursorSource: %v", err)
	}
	var want upstreamPrivateEmbeddedTarget
	if err := upstream.Unmarshal([]byte(source), &want); err != nil {
		t.Fatalf("upstream.Unmarshal baseline: %v", err)
	}
	if got.Value != want.Value || got.Hook != want.Hook {
		t.Fatalf("codec embedded value = %#v, upstream baseline = %#v", got, want)
	}
}

type rejectedYAMLHook struct{ Called bool }

func (h *rejectedYAMLHook) UnmarshalYAML(*upstream.Node) error { h.Called = true; return nil }

type recursiveHookTarget struct {
	Name     rejectedYAMLHook       `yaml:"name"`
	Children []*recursiveHookTarget `yaml:"children"`
}

func TestCursorRejectsRecursiveHookTargetWithoutCallingHook(t *testing.T) {
	var got recursiveHookTarget
	if err := decodeCursorSource([]byte("name: value\n"), &got); err == nil {
		t.Fatal("hook target accepted")
	}
	if got.Name.Called {
		t.Fatal("rejected target called hook")
	}
}

func TestSelectedNodeKeepsCallerGraphIdentity(t *testing.T) {
	const source = "node:\n  nested: value\nhook: seen\n"
	root, err := codec.Parse([]byte(source))
	if err != nil {
		t.Fatal(err)
	}
	want := mappingValue(root, "node")
	if want == nil {
		t.Fatal("missing node")
	}
	session, cursor, err := codec.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	var target struct {
		Node codec.Selected `yaml:"node"`
	}
	got := root
	err = cursor.Select(&target, func(_ *codec.Selected, value codec.Cursor) error { got = value.Node(); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("selected node=%p original=%p", got, want)
	}
}

type textScalar struct {
	Value string
	Calls int
}

func (v *textScalar) UnmarshalText(data []byte) error { v.Value = string(data); v.Calls++; return nil }

type textWithNode struct {
	Node   syntax.Node
	Value  string
	Called bool
}

func (v *textWithNode) UnmarshalText(data []byte) error {
	v.Value = string(data)
	v.Called = true
	return nil
}

func TestCursorTextUnmarshalerPolicy(t *testing.T) {
	const source = "decoded text\n"
	t.Run("plain representation dispatches", func(t *testing.T) {
		var got, want textScalar
		if err := decodeCursorSource([]byte(source), &got); err != nil {
			t.Fatal(err)
		}
		if err := upstream.Unmarshal([]byte(source), &want); err != nil {
			t.Fatal(err)
		}
		if got != want || got.Value != "decoded text" || got.Calls != 1 {
			t.Errorf("codec=%#v upstream=%#v", got, want)
		}
	})
	t.Run("node representation rejected before dispatch", func(t *testing.T) {
		// Cursor validates the complete representation even when upstream text dispatch bypasses fields.
		got := textWithNode{Value: "keep"}
		before := got
		err := decodeCursorSource([]byte(source), &got)
		if err == nil || !strings.Contains(err.Error(), "YAML node fields require an explicit schema traversal") {
			t.Fatalf("node-shaped text target error=%v", err)
		}
		if !reflect.DeepEqual(got, before) {
			t.Errorf("rejected target changed: %#v", got)
		}
		var baseline textWithNode
		if err := upstream.Unmarshal([]byte(source), &baseline); err != nil {
			t.Fatal(err)
		}
		if !baseline.Called || baseline.Value != "decoded text" {
			t.Fatal("upstream text dispatch control did not run")
		}
	})
}

type modernPointerHook struct{ calls *int }

func (h *modernPointerHook) UnmarshalYAML(*upstream.Node) error { (*h.calls)++; return nil }

type modernValueHook struct{ calls *int }

func (h modernValueHook) UnmarshalYAML(*upstream.Node) error { (*h.calls)++; return nil }

type legacyPointerHook struct{ calls *int }

func (h *legacyPointerHook) UnmarshalYAML(func(any) error) error { (*h.calls)++; return nil }

type legacyValueHook struct{ calls *int }

func (h legacyValueHook) UnmarshalYAML(func(any) error) error { (*h.calls)++; return nil }

func TestCursorAndSelectRejectYAMLHookSignaturesBeforeCalling(t *testing.T) {
	for _, tc := range []struct {
		name   string
		target func(*int) any
	}{
		{"modern pointer receiver", func(calls *int) any {
			return &struct {
				Hook modernPointerHook `yaml:"hook"`
			}{Hook: modernPointerHook{calls}}
		}},
		{"modern value receiver", func(calls *int) any {
			return &struct {
				Hook modernValueHook `yaml:"hook"`
			}{Hook: modernValueHook{calls}}
		}},
		{"legacy pointer receiver", func(calls *int) any {
			return &struct {
				Hook legacyPointerHook `yaml:"hook"`
			}{Hook: legacyPointerHook{calls}}
		}},
		{"legacy value receiver", func(calls *int) any {
			return &struct {
				Hook legacyValueHook `yaml:"hook"`
			}{Hook: legacyValueHook{calls}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, selected := range []bool{false, true} {
				root, err := codec.Parse([]byte("hook: value\n"))
				if err != nil {
					t.Fatal(err)
				}
				session, cursor, err := codec.Open(root)
				if err != nil {
					t.Fatal(err)
				}
				calls, visits := 0, 0
				target := tc.target(&calls)
				if selected {
					err = cursor.Select(target, func(*codec.Selected, codec.Cursor) error { visits++; return nil })
				} else {
					err = cursor.Decode(target)
				}
				session.Close()
				if err == nil || !strings.Contains(err.Error(), "YAML hooks require an explicit schema traversal") {
					t.Errorf("Select=%t error=%v", selected, err)
				}
				if calls != 0 || visits != 0 {
					t.Errorf("Select=%t hook calls=%d visits=%d want zero", selected, calls, visits)
				}
			}
		})
	}
}
