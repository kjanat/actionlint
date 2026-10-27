package codec

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	upstream "go.yaml.in/yaml/v4"
)

func TestSelectInheritedAliasBudgetPrefixesMatchUpstream(t *testing.T) {
	source := "payload: &payload [" + strings.TrimSuffix(strings.Repeat("value, ", 650), ", ") + "]\nitems:\n" + strings.Repeat("  - *payload\n", 3)
	root, err := Parse([]byte(source))
	if err != nil {
		t.Fatal(err)
	}
	graph, err := toParser(root)
	if err != nil {
		t.Fatal(err)
	}
	var baseline struct{ Items [][]string }
	counts := newSelectionCounts()
	wantErr := graph.root.Load(&baseline, upstream.WithV3Defaults(), upstream.WithPlugin(counts))
	want, ok := errors.AsType[*upstream.LoadError](wantErr)
	if !ok || !strings.Contains(want.Message, "excessive aliasing") {
		t.Fatalf("upstream alias limit=%v", wantErr)
	}
	for _, enterItem := range []bool{false, true} {
		session, cursor, err := Open(root)
		if err != nil {
			t.Fatal(err)
		}
		observed := newSelectionCounts()
		session.limits = observed
		var selected struct{ Items Selected }
		var completed [][]string
		err = cursor.Select(&selected, func(_ *Selected, occurrence Cursor) error {
			sequence, err := occurrence.Enter()
			if err != nil {
				return err
			}
			for item := range sequence.Items() {
				var value []string
				if enterItem {
					entered, enterErr := item.Enter()
					if enterErr != nil {
						return enterErr
					}
					err = entered.Decode(&value)
				} else {
					err = item.Decode(&value)
				}
				if err != nil {
					return err
				}
				completed = append(completed, value)
			}
			return nil
		})
		fatal, ok := FatalLoadError(session.Finish(err))
		if !ok || fatal.Message != want.Message || fatal.Mark.Index != want.Mark.Index || fatal.Mark.Line != want.Mark.Line || fatal.Mark.Column != want.Mark.Column || fatal.Error() != want.Error() {
			t.Fatalf("enter=%t limit=%v upstream=%v", enterItem, err, want)
		}
		if len(completed) != 1 || len(baseline.Items) != 3 || !reflect.DeepEqual(completed[0], baseline.Items[0]) {
			t.Fatalf("enter=%t completed prefix differs from upstream", enterItem)
		}
		if !reflect.DeepEqual(observed.prefixes, counts.prefixes) {
			t.Fatalf("enter=%t construction prefixes differ from upstream", enterItem)
		}
		session.Close()
	}
}
