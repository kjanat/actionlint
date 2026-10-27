package codec_test

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	"actionlint.kjanat.dev/v2/internal/yaml/codec"
	"actionlint.kjanat.dev/v2/internal/yaml/syntax"
	upstream "go.yaml.in/yaml/v4"
)

const cursorCycleSource = "a: &a {c: *a}\nitems: *a\n"

type cursorCycleRoot struct {
	Items map[string]any `yaml:"items"`
}

func parseCycleSource(t *testing.T, source string) *syntax.Node {
	t.Helper()
	root, err := codec.Parse([]byte(source))
	if err != nil {
		t.Fatalf("parse YAML: %v", err)
	}
	return root
}

func cursorValue(t *testing.T, entered codec.Entered, name string) (codec.Cursor, error) {
	t.Helper()
	for key, value := range entered.Pairs() {
		resolved, err := key.Enter()
		if err != nil {
			return codec.Cursor{}, err
		}
		if resolved.Node().Value == name {
			return value, nil
		}
	}
	return codec.Cursor{}, fmt.Errorf("mapping key %q not found", name)
}

func runCursorCycle(t *testing.T, mode string) (map[string]any, error) {
	t.Helper()
	root := parseCycleSource(t, cursorCycleSource)
	session, rootCursor, err := codec.Open(root)
	if err != nil {
		return nil, err
	}
	defer session.Close()
	rootValue, err := rootCursor.Enter()
	if err != nil {
		return nil, session.Finish(err)
	}
	items, err := cursorValue(t, rootValue, "items")
	if err != nil {
		return nil, session.Finish(err)
	}
	var got map[string]any
	switch mode {
	case "cursor-decode":
		err = items.Decode(&got)
	case "entered-decode":
		var selected codec.Entered
		selected, err = items.Enter()
		if err == nil {
			err = selected.Decode(&got)
		}
	case "nested-cursor-decode":
		var selected codec.Entered
		selected, err = items.Enter()
		if err == nil {
			var child codec.Cursor
			child, err = cursorValue(t, selected, "c")
			if err == nil {
				var nested codec.Entered
				nested, err = child.Enter()
				if err == nil {
					var repeated codec.Cursor
					repeated, err = cursorValue(t, nested, "c")
					if err == nil {
						err = repeated.Decode(&got)
					}
				}
			}
		}
	default:
		t.Fatalf("unknown cursor mode %q", mode)
	}
	return got, session.Finish(err)
}

func compareCycleFailure(t *testing.T, got, want error) {
	t.Helper()
	if got == nil || want == nil {
		t.Fatalf("cursor error = %v, upstream error = %v; want both to fail", got, want)
	}
	gotLoad, gotOK := errors.AsType[*codec.LoadError](got)
	wantLoad, wantOK := errors.AsType[*upstream.LoadError](want)
	if !gotOK || !wantOK {
		t.Fatalf("cursor error = %T %v, upstream error = %T %v", got, got, want, want)
	}
	if gotLoad.Message != wantLoad.Message || gotLoad.Mark.Line != wantLoad.Mark.Line || gotLoad.Mark.Column != wantLoad.Mark.Column || got.Error() != want.Error() {
		t.Errorf("cycle failure differs:\n cursor: %q at %+v\n upstream: %q at %+v", got.Error(), gotLoad.Mark, want.Error(), wantLoad.Mark)
	}
}

func TestCursorCycleDecodeRetainsAliasAncestorAndPartialTarget(t *testing.T) {
	var baseline cursorCycleRoot
	wantErr := upstream.Unmarshal([]byte(cursorCycleSource), &baseline)
	if wantErr == nil {
		t.Fatal("upstream decode succeeded for a selected recursive alias")
	}
	for _, mode := range []string{"cursor-decode", "entered-decode", "nested-cursor-decode"} {
		t.Run(mode, func(t *testing.T) {
			want := baseline.Items
			failure := wantErr
			if mode == "nested-cursor-decode" {
				var nested struct {
					Items struct {
						C struct {
							C map[string]any `yaml:"c"`
						} `yaml:"c"`
					} `yaml:"items"`
				}
				failure = upstream.Unmarshal([]byte(cursorCycleSource), &nested)
				if failure == nil {
					t.Fatal("upstream accepted the selected recursive field")
				}
				want = nested.Items.C.C
			}
			got, gotErr := runCursorCycle(t, mode)
			compareCycleFailure(t, gotErr, failure)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("partial target = %#v, upstream partial target = %#v", got, want)
			}
		})
	}
}

func TestCursorCycleInUnselectedValueIsNotExpanded(t *testing.T) {
	const source = "cycle: &cycle {child: *cycle}\nitems: [ok]\n"
	type selected struct {
		Items []string `yaml:"items"`
	}
	var baseline selected
	if err := upstream.Unmarshal([]byte(source), &baseline); err != nil {
		t.Fatalf("upstream decode of selected field: %v", err)
	}
	root := parseCycleSource(t, source)
	session, rootCursor, err := codec.Open(root)
	if err != nil {
		t.Fatalf("open cursor: %v", err)
	}
	defer session.Close()
	rootValue, err := rootCursor.Enter()
	if err != nil {
		t.Fatalf("enter root: %v", err)
	}
	items, err := cursorValue(t, rootValue, "items")
	if err != nil {
		t.Fatalf("select items: %v", err)
	}
	var got []string
	if err := items.Decode(&got); err != nil {
		t.Fatalf("decode selected items while recursive sibling is ignored: %v", err)
	}
	if err := session.Finish(nil); err != nil {
		t.Fatalf("finish selected traversal: %v", err)
	}
	if !reflect.DeepEqual(got, baseline.Items) {
		t.Fatalf("selected values = %#v, upstream = %#v", got, baseline.Items)
	}
}
