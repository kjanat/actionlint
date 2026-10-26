package codec_test

import (
	"fmt"
	"reflect"
	"testing"

	"actionlint.kjanat.dev/v2/internal/yaml/codec"
	"actionlint.kjanat.dev/v2/internal/yaml/syntax"
	upstream "go.yaml.in/yaml/v4"
)

type recursiveValue struct {
	Name     string            `yaml:"name"`
	Children []*recursiveValue `yaml:"children"`
}

type recursiveValueWithHook struct {
	Name     codecNodeHook             `yaml:"name"`
	Children []*recursiveValueWithHook `yaml:"children"`
}

func callCodecWithoutPanic(t *testing.T, source string, target any) (err error) {
	t.Helper()
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("codec.Unmarshal panicked: %v", recovered)
		}
	}()
	return codec.Unmarshal([]byte(source), target)
}

func TestHookFreeRecursiveTypeMatchesUpstream(t *testing.T) {
	const source = "name: root\nchildren:\n  - name: child\n    children:\n      - name: leaf\n"
	var got recursiveValue
	if err := codec.Unmarshal([]byte(source), &got); err != nil {
		t.Fatalf("codec.Unmarshal: %v", err)
	}
	var want recursiveValue
	if err := upstream.Unmarshal([]byte(source), &want); err != nil {
		t.Fatalf("upstream.Unmarshal baseline: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("codec value = %#v, upstream baseline = %#v", got, want)
	}
}

func TestRecursiveTypeWithOwnedHookReturnsErrorWithoutPanic(t *testing.T) {
	var got recursiveValueWithHook
	err := callCodecWithoutPanic(t, "name: value\n", &got)
	if err == nil {
		t.Fatal("recursive type with owned hook decoded successfully, want explicit unsupported-recursion error")
	}
	if got.Name.Called {
		t.Error("recursive shadow error still called the owned hook")
	}
}

type ExportedMethodEmbed struct {
	Value string `yaml:"value"`
}

func (*ExportedMethodEmbed) Marker() {}

type exportedEmbeddedTarget struct {
	Prefix              string `yaml:"prefix"`
	ExportedMethodEmbed `yaml:",inline"`
	Hook                codecNodeHook `yaml:"hook"`
}

type upstreamExportedEmbeddedTarget struct {
	Prefix              string `yaml:"prefix"`
	ExportedMethodEmbed `yaml:",inline"`
	Hook                upstreamNodeHook `yaml:"hook"`
}

func TestExportedEmbeddedMethodTypeWithHookDoesNotPanic(t *testing.T) {
	const source = "prefix: before\nvalue: embedded\nhook: called\n"
	var got exportedEmbeddedTarget
	if err := callCodecWithoutPanic(t, source, &got); err != nil {
		t.Fatalf("codec.Unmarshal: %v", err)
	}
	var want upstreamExportedEmbeddedTarget
	if err := upstream.Unmarshal([]byte(source), &want); err != nil {
		t.Fatalf("upstream.Unmarshal baseline: %v", err)
	}
	if got.Prefix != want.Prefix || got.Value != want.Value || got.Hook.Value != want.Hook.Value || !got.Hook.Called {
		t.Fatalf("codec embedded value = %#v, upstream baseline = %#v", got, want)
	}
}

type privateEmbedded struct {
	Value string `yaml:"value"`
}

type privateEmbeddedTarget struct {
	privateEmbedded `yaml:",inline"`
	Hook            codecNodeHook `yaml:"hook"`
}

type upstreamPrivateEmbeddedTarget struct {
	privateEmbedded `yaml:",inline"`
	Hook            upstreamNodeHook `yaml:"hook"`
}

func TestUnexportedEmbeddedFieldWithHookDoesNotPanic(t *testing.T) {
	const source = "value: embedded\nhook: called\n"
	var got privateEmbeddedTarget
	if err := callCodecWithoutPanic(t, source, &got); err != nil {
		t.Fatalf("codec.Unmarshal: %v", err)
	}
	var want upstreamPrivateEmbeddedTarget
	if err := upstream.Unmarshal([]byte(source), &want); err != nil {
		t.Fatalf("upstream.Unmarshal baseline: %v", err)
	}
	if got.Value != want.Value || got.Hook.Value != want.Hook.Value || !got.Hook.Called {
		t.Fatalf("codec embedded value = %#v, upstream baseline = %#v", got, want)
	}
}

type upstreamDecoderWithNode struct {
	Node   syntax.Node
	Value  string
	Called bool
}

func (v *upstreamDecoderWithNode) UnmarshalYAML(node *upstream.Node) error {
	v.Called = true
	v.Value = node.Value
	v.Node = syntax.Node{Kind: syntax.ScalarNode, Tag: node.Tag, Value: node.Value, Line: node.Line, Column: node.Column}
	return nil
}

type legacyDecoderWithNode struct {
	Node   syntax.Node
	Value  string
	Called bool
}

func (v *legacyDecoderWithNode) UnmarshalYAML(construct func(any) error) error {
	var decoded string
	if err := construct(&decoded); err != nil {
		return err
	}
	v.Called = true
	v.Value = decoded
	v.Node = syntax.Node{Kind: syntax.ScalarNode, Tag: "!!str", Value: decoded}
	return nil
}

type textDecoderWithNode struct {
	Node   syntax.Node
	Value  string
	Called bool
}

func (v *textDecoderWithNode) UnmarshalText(source []byte) error {
	v.Called = true
	v.Value = string(source)
	v.Node = syntax.Node{Kind: syntax.ScalarNode, Tag: "!!str", Value: v.Value}
	return nil
}

func TestSyntaxNodeFieldDoesNotSuppressUpstreamDecoderDispatch(t *testing.T) {
	const source = "decoded text\n"
	t.Run("upstream Unmarshaler", func(t *testing.T) {
		var got upstreamDecoderWithNode
		if err := codec.Unmarshal([]byte(source), &got); err != nil {
			t.Fatalf("codec.Unmarshal: %v", err)
		}
		var want upstreamDecoderWithNode
		if err := upstream.Unmarshal([]byte(source), &want); err != nil {
			t.Fatalf("upstream.Unmarshal baseline: %v", err)
		}
		if !got.Called || got.Value != want.Value || got.Node.Value != want.Node.Value {
			t.Fatalf("codec result = %#v, upstream baseline = %#v", got, want)
		}
	})
	t.Run("legacy Unmarshaler", func(t *testing.T) {
		var got legacyDecoderWithNode
		if err := codec.Unmarshal([]byte(source), &got); err != nil {
			t.Fatalf("codec.Unmarshal: %v", err)
		}
		var want legacyDecoderWithNode
		if err := upstream.Unmarshal([]byte(source), &want); err != nil {
			t.Fatalf("upstream.Unmarshal baseline: %v", err)
		}
		if !got.Called || got.Value != want.Value || got.Node.Value != want.Node.Value {
			t.Fatalf("codec result = %#v, upstream baseline = %#v", got, want)
		}
	})
	t.Run("TextUnmarshaler", func(t *testing.T) {
		var got textDecoderWithNode
		if err := codec.Unmarshal([]byte(source), &got); err != nil {
			t.Fatalf("codec.Unmarshal: %v", err)
		}
		var want textDecoderWithNode
		if err := upstream.Unmarshal([]byte(source), &want); err != nil {
			t.Fatalf("upstream.Unmarshal baseline: %v", err)
		}
		if !got.Called || got.Value != want.Value || got.Node.Value != want.Node.Value {
			t.Fatalf("codec result = %#v, upstream baseline = %#v", got, want)
		}
	})
}

type nodePointerTarget struct {
	Node *syntax.Node  `yaml:"node"`
	Hook codecNodeHook `yaml:"hook"`
}

func ownedMappingValue(node *syntax.Node, key string) *syntax.Node {
	if node.Kind == syntax.DocumentNode && len(node.Content) != 0 {
		node = node.Content[0]
	}
	if node.Kind != syntax.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

func TestSyntaxNodePointerFieldKeepsCallerGraphIdentity(t *testing.T) {
	const source = "node:\n  nested: value\nhook: seen\n"
	var root syntax.Node
	if err := codec.Unmarshal([]byte(source), &root); err != nil {
		t.Fatalf("decode caller graph: %v", err)
	}
	want := ownedMappingValue(&root, "node")
	if want == nil {
		t.Fatal("source graph has no node value")
	}
	existing := &syntax.Node{Kind: syntax.ScalarNode, Tag: "!!str", Value: "keep"}
	before := *existing
	got := nodePointerTarget{Node: existing}
	if err := codec.Decode(&root, &got); err != nil {
		t.Fatalf("codec.Decode: %v", err)
	}
	if got.Node != want {
		t.Errorf("decoded *syntax.Node = %p, caller graph node = %p", got.Node, want)
	}
	if !reflect.DeepEqual(*existing, before) {
		t.Errorf("previous pointee mutated: got %#v, before %#v", *existing, before)
	}
}
