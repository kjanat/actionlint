package codec

import (
	"errors"
	"reflect"
	"testing"

	upstream "go.yaml.in/yaml/v4"
)

type cycleFields struct {
	X int            `yaml:"x"`
	C map[string]any `yaml:"c"`
}

func cyclePartialTarget(mode string, initial map[string]any) (any, func() map[string]any) {
	switch mode {
	case "nested-entered":
		target := &struct{ Items cycleFields }{Items: cycleFields{C: initial}}
		return target, func() map[string]any { return target.Items.C }
	case "repeated-alias":
		target := &struct {
			Items struct {
				X int
				C cycleFields
			}
		}{}
		target.Items.C.C = initial
		return target, func() map[string]any { return target.Items.C.C }
	default:
		target := &struct{ Items map[string]any }{Items: initial}
		return target, func() map[string]any { return target.Items }
	}
}

func cyclePartialField(t *testing.T, parent Entered, selected string) Cursor {
	t.Helper()
	for key, value := range parent.Pairs() {
		name, err := key.Enter()
		if err != nil {
			t.Fatal(err)
		}
		if name.Node().Value == selected {
			return value
		}
		if name.Node().Value == "x" {
			var scalar int
			if err := value.Decode(&scalar); err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Fatalf("missing field %q", selected)
	return Cursor{}
}

func TestCursorCyclePartialValuesAndCountsAtSelectedField(t *testing.T) {
	const source = "a: &a {x: 1, c: *a}\nitems: *a\n"
	root, err := Parse([]byte(source))
	if err != nil {
		t.Fatal(err)
	}
	graph, err := toParser(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"cursor", "entered", "nested-entered", "repeated-alias"} {
		for _, prefilled := range []bool{false, true} {
			name := mode + "/empty"
			if prefilled {
				name = mode + "/prefilled"
			}
			t.Run(name, func(t *testing.T) {
				initial := func() map[string]any {
					if prefilled {
						return map[string]any{"existing": "unchanged"}
					}
					return nil
				}
				baseline, partial := cyclePartialTarget(mode, initial())
				counts := newCycleAccountingPlugin()
				wantErr := graph.root.Load(baseline, upstream.WithV3Defaults(), upstream.WithPlugin(counts))
				wantLoad, wantOK := errors.AsType[*upstream.LoadError](wantErr)
				if !wantOK || wantLoad.Stage != upstream.ComposerStage {
					t.Fatalf("baseline = %v, want cycle error", wantErr)
				}
				session, cursor, err := Open(root)
				if err != nil {
					t.Fatal(err)
				}
				defer session.Close()
				entered, err := cursor.Enter()
				if err != nil {
					t.Fatal(err)
				}
				cursor = cyclePartialField(t, entered, "items")
				if mode != "cursor" {
					entered, err = cursor.Enter()
					if err != nil {
						t.Fatal(err)
					}
				}
				if mode == "nested-entered" || mode == "repeated-alias" {
					cursor = cyclePartialField(t, entered, "c")
					entered, err = cursor.Enter()
					if err != nil {
						t.Fatal(err)
					}
				}
				if mode == "repeated-alias" {
					cursor = cyclePartialField(t, entered, "c")
				}
				got := initial()
				if mode == "cursor" || mode == "repeated-alias" {
					err = cursor.Decode(&got)
				} else {
					err = entered.Decode(&got)
				}
				gotLoad, gotOK := errors.AsType[*LoadError](err)
				if !gotOK || gotLoad.Message != wantLoad.Message || gotLoad.Mark.Line != wantLoad.Mark.Line || gotLoad.Mark.Column != wantLoad.Mark.Column || err.Error() != wantErr.Error() {
					t.Errorf("cursor error = %v, want %v", err, wantErr)
				}
				if !reflect.DeepEqual(got, partial()) {
					t.Errorf("selected partial = %#v, want %#v", got, partial())
				}
				if session.aliases != counts.aliases || session.constructions != counts.constructions {
					t.Errorf("counts = %d/%d, want %d/%d", session.aliases, session.constructions, counts.aliases, counts.constructions)
				}
			})
		}
	}
}
