package tree

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestDecodeJSONCommentsAndTrailingCommas(t *testing.T) {
	t.Parallel()
	for _, extension := range []string{"json", "jsonc"} {
		t.Run(extension, func(t *testing.T) {
			t.Parallel()
			source := "{\n  // settings\n  \"value\": [true,], /* comment */\n  \"text\": \"https://example.test/*literal*/\",\n}\n"
			node, err := Decode("actionlint."+extension, []byte(source))
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(map[string]any{"value": []any{true}, "text": "https://example.test/*literal*/"}, decodeValue(t, node)); diff != "" {
				t.Fatal(diff)
			}
			key, value := node.Locations["/value"], node.ValueLocations["/value"]
			if key.Line != 3 || key.Column != 3 || value.Line != 3 || value.Column != 12 {
				t.Fatalf("source positions changed: key %d:%d, value %d:%d", key.Line, key.Column, value.Line, value.Column)
			}
			for _, invalid := range []string{`{"value": }`, `{"a": 1 "b": 2}`, `{} garbage`, `{} /* unfinished`} {
				if _, err := Decode("actionlint."+extension, []byte(invalid)); err == nil {
					t.Errorf("accepted invalid JSON: %s", invalid)
				}
			}
			if _, err := Decode("actionlint."+extension, []byte("// empty\n")); err != nil {
				t.Fatal(err)
			}
			if _, err := Decode("actionlint."+strings.ToUpper(extension), []byte(source)); err != nil {
				t.Fatal(err)
			}
		})
	}
}
