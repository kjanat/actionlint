package codec

import (
	"errors"
	"testing"
)

func TestCursorOperationsRejectForeignChildAfterSourceMutation(t *testing.T) {
	for _, operation := range []string{"decode", "enter", "select"} {
		t.Run(operation, func(t *testing.T) {
			cursor := errorCursor(t, []byte("value: original\n"))
			entered, err := cursor.Enter()
			if err != nil {
				t.Fatal(err)
			}
			foreign, err := Parse([]byte("other: foreign\n"))
			if err != nil {
				t.Fatal(err)
			}
			cursor.node.Content[0].Content[1] = foreign.Content[0].Content[1]
			for _, child := range entered.Pairs() {
				constructions, aliases := cursor.session.constructions, cursor.session.aliases
				value := "unchanged"
				switch operation {
				case "decode":
					err = child.Decode(&value)
				case "enter":
					_, err = child.Enter()
				case "select":
					var target struct{ Value Selected }
					err = child.Select(&target, func(*Selected, Cursor) error {
						t.Fatal("foreign source reached visitor")
						return nil
					})
				}
				graphErr, ok := errors.AsType[*GraphError](err)
				if !ok || graphErr.Reason != "node belongs to another source graph" {
					t.Fatalf("foreign child error=%v", err)
				}
				if value != "unchanged" || cursor.session.constructions != constructions || cursor.session.aliases != aliases {
					t.Fatal("foreign child changed target or consumed budget")
				}
				var selected struct{ Value Selected }
				outer := cursor.Select(&selected, func(*Selected, Cursor) error { return err })
				if cause, ok := errors.AsType[*GraphError](outer); !ok || cause != graphErr {
					t.Fatalf("Select lost foreign-graph cause: %v", outer)
				}
				if cause, ok := errors.AsType[*GraphError](cursor.session.Finish(outer)); !ok || cause != graphErr {
					t.Fatalf("Finish lost foreign-graph cause: %v", outer)
				}
			}
		})
	}
}

func TestCursorRejectsForeignGraphBeforeConstruction(t *testing.T) {
	root, err := Parse([]byte("value: original\n"))
	if err != nil {
		t.Fatal(err)
	}
	session, cursor, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	foreign, err := Parse([]byte("other: foreign\n"))
	if err != nil {
		t.Fatal(err)
	}
	node := foreign.Content[0].Content[1]
	foreignCursor := Cursor{session: session, node: node}
	value := "unchanged"
	err = foreignCursor.Decode(&value)
	graphErr, ok := errors.AsType[*GraphError](err)
	if !ok || graphErr.Reason != "node belongs to another source graph" || graphErr.Mark != (Mark{Line: node.Line, Column: node.Column}) {
		t.Fatalf("foreign cursor error = %v", err)
	}
	if value != "unchanged" || session.constructions != 0 || session.aliases != 0 {
		t.Fatalf("foreign cursor changed target or budget: value=%q constructions=%d aliases=%d", value, session.constructions, session.aliases)
	}
	var valid map[string]string
	if err := cursor.Decode(&valid); err != nil || valid["value"] != "original" {
		t.Fatalf("foreign cursor poisoned valid traversal: %v, %v", valid, err)
	}
}
