package codec

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/yaml/syntax"
	upstream "github.com/kjanat/go-yaml/v4"
	"github.com/kjanat/go-yaml/v4/plugin/limit"
)

type selectionSource struct {
	Picked Selected `yaml:"picked"`
}

type selectionCopySource struct {
	Copy Selected `yaml:"copy"`
}

type selectionNested struct {
	Self Selected `yaml:"self"`
}

type selectionCycleNode struct {
	Self *selectionCycleNode `yaml:"self"`
}

type selectionHookField struct{ Value string }

func (*selectionHookField) UnmarshalYAML(*syntax.Node) error { return nil }

type selectionInput struct {
	Name    string `yaml:"name"`
	Enabled bool   `yaml:"enabled"`
}

type selectionCounts struct {
	defaults               *limit.Plugin
	aliases, constructions int
	prefixes               [][2]int
}

func (p *selectionCounts) CheckDepth(depth int, ctx *upstream.DepthContext) error {
	return p.defaults.CheckDepth(depth, ctx)
}

func (p *selectionCounts) CheckAlias(aliases, constructions int) error {
	p.aliases, p.constructions = aliases, constructions
	p.prefixes = append(p.prefixes, [2]int{aliases, constructions})
	return p.defaults.CheckAlias(aliases, constructions)
}

func newSelectionCounts() *selectionCounts {
	return &selectionCounts{defaults: limit.New()}
}

func TestSelectPreservesAliasOccurrenceAndCycleContext(t *testing.T) {
	const source = "picked: &node {self: *node}\ncopy: *node\n"
	root, err := Parse([]byte(source))
	if err != nil {
		t.Fatal(err)
	}
	copyNode := root.Content[0].Content[3]
	if copyNode.Kind != syntax.AliasNode || copyNode.Alias != root.Content[0].Content[1] {
		t.Fatalf("parsed alias does not target original node: %#v", copyNode)
	}

	var baseline struct {
		Copy selectionCycleNode `yaml:"copy"`
	}
	wantErr := upstream.Unmarshal([]byte(source), &baseline)
	if wantErr == nil || !strings.Contains(wantErr.Error(), "contains itself") {
		t.Fatalf("upstream baseline error = %v, want recursive alias error", wantErr)
	}

	session, cursor, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	var selected selectionCopySource
	gotErr := cursor.Select(&selected, func(_ *Selected, occurrence Cursor) error {
		if occurrence.Node() != root.Content[0].Content[3] {
			return fmt.Errorf("selected node %p, want original alias occurrence %p", occurrence.Node(), copyNode)
		}
		entered, err := occurrence.Enter()
		if err != nil {
			return err
		}
		var nested selectionNested
		return entered.Select(&nested, func(_ *Selected, child Cursor) error {
			selectedSelf, err := child.Enter()
			if err != nil {
				return err
			}
			var recursive selectionCycleNode
			return selectedSelf.Decode(&recursive)
		})
	})
	gotLoad, gotOK := errors.AsType[*LoadError](gotErr)
	wantLoad, wantOK := errors.AsType[*upstream.LoadError](wantErr)
	if !gotOK || !wantOK {
		t.Fatalf("selection cycle error = %T %v; upstream = %T %v", gotErr, gotErr, wantErr, wantErr)
	}
	if gotLoad.Message() != wantLoad.Message || gotLoad.Mark().Line != wantLoad.Mark.Line || gotLoad.Mark().Column != wantLoad.Mark.Column || gotLoad.Error() != wantLoad.Error() {
		t.Fatalf("selection cycle error (%q, %+v), upstream (%q, %+v)", gotLoad.Error(), gotLoad.Mark(), wantLoad.Error(), wantLoad.Mark)
	}
	if root.Content[0].Content[3] != copyNode || copyNode.Alias != root.Content[0].Content[1] {
		t.Fatal("selection changed the caller's alias graph")
	}
}

func TestSelectAliasAndMergeCountsMatchPlainDecode(t *testing.T) {
	const source = `defaults: &defaults {name: shared, enabled: true}
items: &items
  - <<: *defaults
  - name: direct
    enabled: false
picked: *items
`
	var baseline struct {
		Picked []selectionInput `yaml:"picked"`
	}
	root, err := Parse([]byte(source))
	if err != nil {
		t.Fatal(err)
	}
	graph, err := toParser(root)
	if err != nil {
		t.Fatal(err)
	}
	counts := newSelectionCounts()
	if err := graph.root.Load(&baseline, upstream.WithV3Defaults(), upstream.WithPlugin(counts)); err != nil {
		t.Fatal(err)
	}
	if want := []selectionInput{{Name: "shared", Enabled: true}, {Name: "direct", Enabled: false}}; !reflect.DeepEqual(baseline.Picked, want) {
		t.Fatalf("upstream values = %#v, want %#v", baseline.Picked, want)
	}

	for _, enterItem := range []bool{false, true} {
		session, cursor, err := Open(root)
		if err != nil {
			t.Fatal(err)
		}
		observed := newSelectionCounts()
		session.limits = observed
		var target selectionSource
		var got []selectionInput
		err = cursor.Select(&target, func(_ *Selected, occurrence Cursor) error {
			if occurrence.Node().Kind != syntax.AliasNode {
				return fmt.Errorf("selected occurrence kind = %v, want alias", occurrence.Node().Kind)
			}
			entered, err := occurrence.Enter()
			if err != nil {
				return err
			}
			for item := range entered.Items() {
				var value selectionInput
				if enterItem {
					itemValue, enterErr := item.Enter()
					if enterErr != nil {
						return enterErr
					}
					err = itemValue.Decode(&value)
				} else {
					err = item.Decode(&value)
				}
				if err != nil {
					return err
				}
				got = append(got, value)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, baseline.Picked) {
			t.Fatalf("enter item=%t values = %#v, upstream = %#v", enterItem, got, baseline.Picked)
		}
		if session.aliases != counts.aliases || session.constructions != counts.constructions {
			t.Fatalf("enter item=%t counts %d/%d, upstream %d/%d", enterItem, session.aliases, session.constructions, counts.aliases, counts.constructions)
		}
		if !reflect.DeepEqual(observed.prefixes, counts.prefixes) {
			t.Fatalf("enter item=%t count prefixes = %v, upstream = %v", enterItem, observed.prefixes, counts.prefixes)
		}
		session.Close()
	}
}

func TestSelectSchemaErrorsAndClosedCursor(t *testing.T) {
	root, err := Parse([]byte("first: one\nsecond: two\n"))
	if err != nil {
		t.Fatal(err)
	}
	session, cursor, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	var target struct {
		First  Selected `yaml:"first"`
		Second Selected `yaml:"second"`
	}
	var selected Cursor
	got := cursor.Select(&target, func(_ *Selected, occurrence Cursor) error {
		selected = occurrence
		return errors.New("invalid selected value")
	})
	if got == nil || !strings.Contains(got.Error(), "invalid selected value") || !strings.Contains(got.Error(), "line 1") || !strings.Contains(got.Error(), "line 2") {
		t.Fatalf("joined selection errors = %v; want both source locations", got)
	}
	session.Close()
	if err := cursor.Select(&target, func(*Selected, Cursor) error { return nil }); err == nil || !strings.Contains(err.Error(), "decoder is not active") {
		t.Fatalf("select after Close error = %v, want inactive session", err)
	}
	if _, err := selected.Enter(); err == nil || !strings.Contains(err.Error(), "decoder is not active") {
		t.Fatalf("selected cursor after Close error = %v, want inactive session", err)
	}
}

func TestSelectAliasBudgetMatchesUpstreamAndStopsLaterVisitor(t *testing.T) {
	data := []byte("payload: &payload [" + strings.TrimSuffix(strings.Repeat("value, ", 650), ", ") + "]\nitems:\n  - *payload\n  - *payload\n  - *payload\nafter: reached\n")
	var baseline struct {
		Items [][]string `yaml:"items"`
	}
	wantErr := upstream.Unmarshal(data, &baseline)
	if wantErr == nil || !strings.Contains(wantErr.Error(), "excessive aliasing") {
		t.Fatalf("upstream error = %v, want alias budget error", wantErr)
	}
	root, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	session, cursor, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	var target struct {
		Items Selected `yaml:"items"`
		After Selected `yaml:"after"`
	}
	var completed [][]string
	afterVisited := false
	gotErr := cursor.Select(&target, func(field *Selected, occurrence Cursor) error {
		if field == &target.After {
			afterVisited = true
			return nil
		}
		items, err := occurrence.Enter()
		if err != nil {
			return err
		}
		for item := range items.Items() {
			var value []string
			if err := item.Decode(&value); err != nil {
				return err
			}
			completed = append(completed, value)
		}
		return nil
	})
	gotLoad, gotOK := errors.AsType[*LoadError](gotErr)
	wantLoad, wantOK := errors.AsType[*upstream.LoadError](wantErr)
	if !gotOK || !wantOK {
		t.Fatalf("selection error = %T %v; upstream = %T %v", gotErr, gotErr, wantErr, wantErr)
	}
	if gotLoad.Message() != wantLoad.Message || gotLoad.Mark().Index != wantLoad.Mark.Index || gotLoad.Mark().Line != wantLoad.Mark.Line || gotLoad.Mark().Column != wantLoad.Mark.Column || gotLoad.Error() != wantLoad.Error() {
		t.Fatalf("selection limit=%v, upstream=%v", gotLoad, wantLoad)
	}
	if !strings.Contains(gotErr.Error(), "document contains excessive aliasing") {
		t.Errorf("selection error = %q, want alias-budget failure", gotLoad.Error())
	}
	if afterVisited {
		t.Error("selection visited a later field after the alias budget failed")
	}
	if len(completed) == 0 || len(completed) >= len(baseline.Items) {
		t.Errorf("completed items = %d, upstream partial items = %d; want stop before all items", len(completed), len(baseline.Items))
	}
}

func TestSelectRejectsInvalidTargetAndVisitor(t *testing.T) {
	root, err := Parse([]byte("value: ok\n"))
	if err != nil {
		t.Fatal(err)
	}
	session, cursor, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	var target struct {
		Value Selected `yaml:"value"`
	}
	for _, tc := range []struct {
		name string
		call func() error
	}{
		{"nil target", func() error { return cursor.Select(nil, func(*Selected, Cursor) error { return nil }) }},
		{"nil visitor", func() error { return cursor.Select(&target, nil) }},
		{"hook field", func() error {
			return cursor.Select(&struct {
				Value selectionHookField `yaml:"value"`
			}{}, func(*Selected, Cursor) error { return nil })
		}},
		{"node field", func() error {
			return cursor.Select(&struct {
				Value *syntax.Node `yaml:"value"`
			}{}, func(*Selected, Cursor) error { return nil })
		}},
		{"nested marker", func() error {
			return cursor.Select(&struct {
				Value []Selected `yaml:"value"`
			}{}, func(*Selected, Cursor) error { return nil })
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); err == nil {
				t.Fatal("Select succeeded for invalid call")
			}
		})
	}
}
