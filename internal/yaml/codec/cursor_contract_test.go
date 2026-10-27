package codec

import (
	"reflect"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/yaml/syntax"
)

func TestParseEmptySourcesHaveZeroOwnedRoot(t *testing.T) {
	for _, data := range [][]byte{nil, {}, []byte(" \n \n"), []byte("# comment\n")} {
		root, err := Parse(data)
		if err != nil || root == nil || !reflect.ValueOf(*root).IsZero() {
			t.Fatalf("empty source %q: root=%+v err=%v", data, root, err)
		}
		session, _, err := Open(root)
		if err != nil {
			t.Fatalf("empty source %q cannot open: %v", data, err)
		}
		session.Close()
	}
}

func TestEmptySourcesDoNotVisitSelectedFields(t *testing.T) {
	var roots []*syntax.Node
	for _, data := range []string{"", "# comment\n"} {
		root, err := Parse([]byte(data))
		if err != nil {
			t.Fatal(err)
		}
		roots = append(roots, root)
	}
	roots = append(roots, &syntax.Node{Kind: syntax.DocumentNode})
	for _, root := range roots {
		session, cursor, err := Open(root)
		if err != nil {
			t.Fatal(err)
		}
		var target struct{ Value Selected }
		err = cursor.Select(&target, func(*Selected, Cursor) error {
			t.Error("empty source visited a selected field")
			return nil
		})
		finished := session.Finish(err)
		session.Close()
		if finished != nil {
			t.Fatalf("empty source selection failed: %v", finished)
		}
	}
}

func TestCursorRejectsNodeAndSelectedDecodeTargetsBeforeConstruction(t *testing.T) {
	for _, tc := range []struct {
		target any
		want   string
	}{
		{&syntax.Node{Value: "unchanged"}, "YAML node fields require an explicit schema traversal"},
		{&struct{ Value Selected }{}, "YAML selections require a Select struct field"},
	} {
		cursor := errorCursor(t, []byte("value: input\n"))
		err := cursor.Decode(tc.target)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("target %T error=%v, want %q", tc.target, err, tc.want)
		}
		if cursor.session.constructions != 0 || cursor.session.aliases != 0 {
			t.Fatal("rejected target consumed construction budget")
		}
		if node, ok := tc.target.(*syntax.Node); ok && node.Value != "unchanged" {
			t.Fatalf("rejected node target changed: %+v", node)
		}
	}
}
