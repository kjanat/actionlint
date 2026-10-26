package codec_test

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/yaml/codec"
	"actionlint.kjanat.dev/v2/internal/yaml/syntax"
	upstream "go.yaml.in/yaml/v4"
)

type optionDirect struct {
	Value any `yaml:"value"`
}

type optionShadow struct {
	Value any           `yaml:"value"`
	Hook  codecNodeHook `yaml:"hook"`
}

func duplicateKeyResult(decode func([]byte, any) error, target any) error {
	return decode([]byte("value: first\nvalue: second\n"), target)
}

func TestDuplicateKeysUseUpstreamDefaultsDirectAndShadow(t *testing.T) {
	var direct optionDirect
	gotDirect := duplicateKeyResult(codec.Unmarshal, &direct)
	var wantDirect optionDirect
	wantDirectErr := duplicateKeyResult(upstream.Unmarshal, &wantDirect)
	if gotDirect == nil || wantDirectErr == nil {
		t.Fatalf("duplicate key errors: codec=%v upstream=%v", gotDirect, wantDirectErr)
	}
	if gotDirect.Error() != wantDirectErr.Error() {
		t.Errorf("direct duplicate key error = %q, upstream %q", gotDirect, wantDirectErr)
	}

	var shadow optionShadow
	gotShadow := duplicateKeyResult(codec.Unmarshal, &shadow)
	var wantShadow optionShadow
	wantShadowErr := duplicateKeyResult(upstream.Unmarshal, &wantShadow)
	if gotShadow == nil || wantShadowErr == nil {
		t.Fatalf("shadow duplicate key errors: codec=%v upstream=%v", gotShadow, wantShadowErr)
	}
	if gotShadow.Error() != wantShadowErr.Error() {
		t.Errorf("shadow duplicate key error = %q, upstream %q", gotShadow, wantShadowErr)
	}
}

func aliasExpansionSource(depth, width int) []byte {
	var source strings.Builder
	fmt.Fprintf(&source, "base: &a0 [x]\n")
	for level := 1; level <= depth; level++ {
		fmt.Fprintf(&source, "a%d: &a%d [", level, level)
		for i := range width {
			if i > 0 {
				source.WriteString(", ")
			}
			fmt.Fprintf(&source, "*a%d", level-1)
		}
		source.WriteString("]\n")
	}
	fmt.Fprintf(&source, "value: *a%d\n", depth)
	return []byte(source.String())
}

func TestAliasExpansionUsesUpstreamDefaultsDirectAndShadow(t *testing.T) {
	data := aliasExpansionSource(5, 8)
	var direct optionDirect
	gotDirect := codec.Unmarshal(data, &direct)
	var wantDirect optionDirect
	wantDirectErr := upstream.Unmarshal(data, &wantDirect)
	if (gotDirect == nil) != (wantDirectErr == nil) {
		t.Errorf("direct alias expansion success mismatch: codec=%v upstream=%v", gotDirect, wantDirectErr)
	}
	if gotDirect != nil && gotDirect.Error() != wantDirectErr.Error() {
		t.Errorf("direct alias expansion error = %q, upstream %q", gotDirect, wantDirectErr)
	}
	if gotDirect == nil && !reflect.DeepEqual(direct, wantDirect) {
		t.Errorf("direct alias expansion result differs from upstream")
	}

	shadowData := append(append([]byte(nil), data...), []byte("hook: seen\n")...)
	var shadow optionShadow
	gotShadow := codec.Unmarshal(shadowData, &shadow)
	var wantShadow optionShadow
	wantShadowErr := upstream.Unmarshal(shadowData, &wantShadow)
	if (gotShadow == nil) != (wantShadowErr == nil) {
		t.Fatalf("shadow alias expansion success mismatch: codec=%v upstream=%v", gotShadow, wantShadowErr)
	}
	if gotShadow != nil && (!strings.Contains(gotShadow.Error(), "document contains excessive aliasing") || !strings.Contains(wantShadowErr.Error(), "document contains excessive aliasing")) {
		t.Errorf("shadow alias expansion errors do not report upstream's alias limit: codec=%q upstream=%q", gotShadow, wantShadowErr)
	}
	if gotShadow == nil && (!reflect.DeepEqual(shadow.Value, wantShadow.Value) || !shadow.Hook.Called) {
		t.Errorf("shadow alias expansion result differs from upstream or hook was not called")
	}
}

func TestSyntaxNodeCaptureKeepsAliasGraphUnexpanded(t *testing.T) {
	data := aliasExpansionSource(5, 8)
	var got syntax.Node
	if err := codec.Unmarshal(data, &got); err != nil {
		t.Fatalf("capture syntax graph: %v", err)
	}
	if got.Kind != syntax.DocumentNode || len(got.Content) != 1 {
		t.Fatalf("captured document root = kind %v, children %d", got.Kind, len(got.Content))
	}
	root := got.Content[0]
	value := mappingValue(root, "value")
	level := mappingValue(root, "a5")
	if value == nil || level == nil || value.Kind != syntax.AliasNode || level.Kind != syntax.SequenceNode {
		t.Fatalf("captured alias nodes missing: value=%#v level=%#v", value, level)
	}
	if value.Alias != level {
		t.Errorf("captured value alias target = %p, anchor node = %p", value.Alias, level)
	}
}
