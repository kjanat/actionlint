package codec

import (
	"testing"

	upstream "github.com/kjanat/go-yaml/v4"
)

func TestCursorLeafMergeCountsAndIgnoredRecursiveField(t *testing.T) {
	const source = `base: &base {name: merged, enabled: true}
template: &template
  <<: *base
  self: *template
  name: explicit
items: *template
`
	type selected struct {
		Name    string `yaml:"name"`
		Enabled bool   `yaml:"enabled"`
	}
	var baseline struct {
		Items selected `yaml:"items"`
	}
	root, err := Parse([]byte(source))
	if err != nil {
		t.Fatal(err)
	}
	graph, err := toParser(root)
	if err != nil {
		t.Fatal(err)
	}
	counts := newCycleAccountingPlugin()
	if err := graph.root.Load(&baseline, upstream.WithV3Defaults(), upstream.WithPlugin(counts)); err != nil {
		t.Fatal(err)
	}
	if baseline.Items != (selected{Name: "explicit", Enabled: true}) {
		t.Fatalf("merge baseline = %+v", baseline.Items)
	}
	for _, enter := range []bool{false, true} {
		session, cursor, err := Open(root)
		if err != nil {
			t.Fatal(err)
		}
		defer session.Close()
		value, err := cursor.Enter()
		if err != nil {
			t.Fatal(err)
		}
		var got selected
		for key, field := range value.Pairs() {
			name, err := key.Enter()
			if err != nil {
				t.Fatal(err)
			}
			if name.Node().Value != "items" {
				continue
			}
			if enter {
				var entered Entered
				entered, err = field.Enter()
				if err != nil {
					t.Fatal(err)
				}
				err = entered.Decode(&got)
			} else {
				err = field.Decode(&got)
			}
			if err != nil {
				t.Fatalf("entered=%t: %v", enter, err)
			}
		}
		if got != baseline.Items {
			t.Errorf("entered=%t: value=%+v, want %+v", enter, got, baseline.Items)
		}
		if session.aliases != counts.aliases || session.constructions != counts.constructions {
			t.Errorf("entered=%t: counts=%d/%d, want %d/%d", enter, session.aliases, session.constructions, counts.aliases, counts.constructions)
		}
		session.Close()
	}
}
