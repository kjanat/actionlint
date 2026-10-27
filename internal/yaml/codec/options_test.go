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

type optionAdditionalField struct {
	Value any            `yaml:"value"`
	Hook  codec.Selected `yaml:"hook"`
}

func duplicateKeyResult(decode func([]byte, any) error, target any) error {
	return decode([]byte("value: first\nvalue: second\n"), target)
}

func TestDuplicateKeysUseUpstreamDefaultsDirectAndSelect(t *testing.T) {
	var direct, wantDirect optionDirect
	gotErr := duplicateKeyResult(decodeCursorSource, &direct)
	wantErr := duplicateKeyResult(upstream.Unmarshal, &wantDirect)
	assertConstructErrorsMatchUpstream(t, gotErr, wantErr)
	for _, tc := range []struct {
		name, source string
		visits       int
	}{
		{"duplicate before selected field", "value: first\nvalue: second\nhook: seen\n", 0},
		{"valid selected control", "value: first\nhook: seen\n", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, err := codec.Parse([]byte(tc.source))
			if err != nil {
				t.Fatal(err)
			}
			session, cursor, err := codec.Open(root)
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			var got optionAdditionalField
			visits := 0
			err = cursor.Select(&got, func(field *codec.Selected, value codec.Cursor) error {
				visits++
				if field != &got.Hook {
					t.Error("wrong selected field")
				}
				var text string
				if err := value.Decode(&text); err != nil {
					return err
				}
				if text != "seen" {
					t.Errorf("selected hook=%q", text)
				}
				return nil
			})
			err = session.Finish(err)
			var want struct {
				Value any    `yaml:"value"`
				Hook  string `yaml:"hook"`
			}
			wantErr := upstream.Unmarshal([]byte(tc.source), &want)
			if tc.visits == 0 {
				assertConstructErrorsMatchUpstream(t, err, wantErr)
			} else if err != nil || wantErr != nil {
				t.Fatalf("codec=%v upstream=%v", err, wantErr)
			}
			if visits != tc.visits {
				t.Errorf("visitor calls=%d want %d", visits, tc.visits)
			}
			if !reflect.DeepEqual(got.Value, want.Value) {
				t.Errorf("partial value=%#v upstream=%#v", got.Value, want.Value)
			}
		})
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

func TestAliasExpansionUsesUpstreamDefaultsDirectAndSelect(t *testing.T) {
	data := aliasExpansionSource(5, 8)
	var direct, wantDirect optionDirect
	gotErr := decodeCursorSource(data, &direct)
	wantErr := upstream.Unmarshal(data, &wantDirect)
	if gotErr == nil || wantErr == nil {
		t.Fatalf("excessive aliases codec=%v upstream=%v", gotErr, wantErr)
	}
	assertCursorAliasErrorMatchesUpstream(t, gotErr, wantErr)
	for _, tc := range []struct {
		name                 string
		depth, width, visits int
	}{
		{"fatal before selected field", 5, 8, 0}, {"valid selected control", 1, 2, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := append(aliasExpansionSource(tc.depth, tc.width), []byte("hook: seen\n")...)
			root, err := codec.Parse(data)
			if err != nil {
				t.Fatal(err)
			}
			session, cursor, err := codec.Open(root)
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			var got optionAdditionalField
			visits := 0
			err = cursor.Select(&got, func(field *codec.Selected, value codec.Cursor) error {
				visits++
				if field != &got.Hook {
					t.Error("wrong selected field")
				}
				var text string
				if err := value.Decode(&text); err != nil {
					return err
				}
				if text != "seen" {
					t.Errorf("selected hook=%q", text)
				}
				return nil
			})
			err = session.Finish(err)
			var want struct {
				Value any    `yaml:"value"`
				Hook  string `yaml:"hook"`
			}
			wantErr := upstream.Unmarshal(data, &want)
			if tc.visits == 0 {
				if wantErr == nil || !strings.Contains(wantErr.Error(), "excessive aliasing") {
					t.Fatalf("upstream=%v want alias limit", wantErr)
				}
				assertCursorAliasErrorMatchesUpstream(t, err, wantErr)
			} else {
				if err != nil || wantErr != nil {
					t.Fatalf("codec=%v upstream=%v", err, wantErr)
				}
				if !reflect.DeepEqual(got.Value, want.Value) {
					t.Error("successful Select values differ")
				}
			}
			if visits != tc.visits {
				t.Errorf("visitor calls=%d want %d", visits, tc.visits)
			}
		})
	}
}

func TestSyntaxNodeCaptureKeepsAliasGraphUnexpanded(t *testing.T) {
	data := aliasExpansionSource(5, 8)
	got, err := codec.Parse(data)
	if err != nil {
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
