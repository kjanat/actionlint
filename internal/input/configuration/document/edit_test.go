package document

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func orderedFields(entry any) ([][2]string, error) {
	object, _ := entry.(map[string]any)
	var fields [][2]string
	for _, key := range []string{"name", "registry"} {
		if value, ok := object[key].(string); ok {
			fields = append(fields, [2]string{key, value})
		}
	}
	return fields, nil
}

func TestEditorSetListKeepsOtherBytes(t *testing.T) {
	t.Parallel()
	custom := map[string]any{"registry": "https://example.test/", "name": "custom"}
	for _, test := range []struct {
		path, source string
		entries      []any
		want         string
	}{
		{"x.toml", "# head\nitems = []\r\nother = 1 # kept\r\n", []any{"plain", custom}, "# head\nitems = [\"plain\", { name = \"custom\", registry = \"https://example.test/\" }]\r\nother = 1 # kept\r\n"},
		{"x.toml", "other = 1\n\n[[items]]\nname = 'old'\n", []any{custom}, "other = 1\n\n[[items]]\nname = \"custom\"\nregistry = \"https://example.test/\"\n"},
		{"x.yaml", "items: [old]\n# note\nother: 1 # kept\n", []any{"plain", custom}, "items:\n    - plain\n    - name: custom\n      registry: https://example.test/\n# note\nother: 1 # kept\n"},
		{"x.jsonc", "{\n  // note\n  \"items\": [],\n  \"other\": 1,\n}\n", []any{"plain", custom}, "{\n  // note\n  \"items\": [\"plain\",{\"name\":\"custom\",\"registry\":\"https://example.test/\"}],\n  \"other\": 1,\n}\n"},
	} {
		editor, err := NewEditor(test.path, []byte(test.source))
		if err != nil {
			t.Fatal(err)
		}
		got, err := editor.SetList("items", test.entries, orderedFields)
		if err != nil {
			t.Fatalf("%s: %v", test.path, err)
		}
		if string(got) != test.want {
			t.Errorf("%s:\ngot  %q\nwant %q", test.path, got, test.want)
		}
	}
}

func TestEditorRewriteTOMLReplacesOnlyMembers(t *testing.T) {
	t.Parallel()
	source := "#:schema ./schema.json\nfiles = ['a'] # kept\n\n# old setting\nold = true\n"
	editor, err := NewEditor("x.toml", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	value := map[string]any{"files": []any{"a"}, "new": json.Number("1")}
	got, err := editor.Rewrite(value, []string{"old", "new"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	text := string(got)
	if !strings.HasPrefix(text, "#:schema ./schema.json\nfiles = ['a'] # kept\n") || strings.Count(text, "# old setting") != 1 || strings.Contains(text, "old = true") {
		t.Fatalf("unexpected rewrite:\n%s", text)
	}
	reparsed, err := Parse("x.toml", got)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(reparsed.Value(), value) {
		t.Fatalf("values %#v", reparsed.Value())
	}
}
