package tree

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/configuration/document"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func decodeValue(t *testing.T, doc document.Document) any { t.Helper(); return doc.Value }

func TestDecodeTOMLValues(t *testing.T) {
	t.Parallel()
	source := `# settings
title = "Hello"
count = 1_000
hex = 0xff
negative = -7
ratio = 2.5
big = 6.02e23
positive_infinity = +inf
negative_infinity = -inf
not_a_number = nan
enabled = true
date = 2024-01-02
quoted = "123"
empty = []

[server]
host = "localhost"
ports = [80, 443]
options = { retries = 3, verbose = false }

[server.tls]
enabled = false

[[items]]
name = "a"

[[items]]
name = "b"
tags.primary = "x"
`
	want := map[string]any{
		"title": "Hello", "count": json.Number("1000"), "hex": json.Number("255"), "negative": json.Number("-7"), "ratio": json.Number("2.5"), "big": json.Number("6.02e+23"),
		"positive_infinity": math.Inf(1), "negative_infinity": math.Inf(-1), "not_a_number": math.NaN(),
		"enabled": true, "date": "2024-01-02", "quoted": "123", "empty": []any{},
		"server": map[string]any{
			"host": "localhost", "ports": []any{json.Number("80"), json.Number("443")},
			"options": map[string]any{"retries": json.Number("3"), "verbose": false},
			"tls":     map[string]any{"enabled": false},
		},
		"items": []any{map[string]any{"name": "a"}, map[string]any{"name": "b", "tags": map[string]any{"primary": "x"}}},
	}
	node, err := Decode("actionlint.toml", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(want, decodeValue(t, node), cmpopts.EquateNaNs()); diff != "" {
		t.Fatal(diff)
	}
}

func TestDecodeTOMLPositions(t *testing.T) {
	t.Parallel()
	node, err := Decode("actionlint.toml", []byte("names = [\"é\", \"b\"]\nt = { \"é\" = 1, b = 2 }\n[server]\nhost = \"x\"\n  port = 8080\n"))
	if err != nil {
		t.Fatal(err)
	}
	positions := map[string]document.Location{
		"names key": node.Locations["/names"], "names value": node.ValueLocations["/names"], "names second": node.ValueLocations["/names/1"],
		"inline value": node.ValueLocations["/t"], "inline b key": node.Locations["/t/b"],
		"server key": node.Locations["/server"], "host key": node.Locations["/server/host"], "host value": node.ValueLocations["/server/host"],
		"port key": node.Locations["/server/port"], "port value": node.ValueLocations["/server/port"],
	}
	want := map[string][2]int{
		"names key": {1, 1}, "names value": {1, 9}, "names second": {1, 15}, "inline value": {2, 5}, "inline b key": {2, 16},
		"server key": {3, 2}, "host key": {4, 1}, "host value": {4, 8}, "port key": {5, 3}, "port value": {5, 10},
	}
	got := map[string][2]int{}
	for name, n := range positions {
		got[name] = [2]int{n.Line, n.Column}
	}

	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatal(diff)
	}
}

func TestDecodeTOMLErrors(t *testing.T) {
	t.Parallel()
	tests := []struct{ name, source, want string }{
		{"duplicate key", "a = 1\na = 2\n", `line 2, column 1: key "a" defined twice`},
		{"table twice", "[a]\n[a]\n", `line 2, column 2: table "a" defined twice`},
		{"header on dotted table", "a.b = 1\n[a]\n", `line 2, column 2: table "a" defined twice`},
		{"dotted keys into header table", "[a.b]\n[a]\nb.c = 1\n", `line 3, column 1: cannot extend table "b" with dotted keys`},
		{"header into inline table", "a = { x = 1 }\n[a.b]\n", `line 2, column 2: cannot extend inline table "a"`},
		{"array table on static array", "a = [1]\n[[a]]\n", `line 2, column 3: cannot append a table to "a"`},
		{"scalar as table", "a = 1\n[a.b]\n", `line 2, column 2: key "a" is not a table`},
		{"duplicate inline key", "a = { x = 1, x = 2 }\n", `line 1, column 14: key "x" defined twice`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := Decode("actionlint.toml", []byte(test.source))
			if err == nil || !strings.Contains(err.Error(), "actionlint.toml:") {
				t.Fatalf("got %v, want %q", err, test.want)
			}
		})
	}
	_, err := Decode("actionlint.toml", []byte("a = \n"))
	if err == nil || !strings.Contains(err.Error(), "actionlint.toml:") {
		t.Fatalf("syntax error: %v", err)
	}
}

func TestDecodeTOMLEmptyDocument(t *testing.T) {
	t.Parallel()
	for _, source := range []string{"", "# only a comment\n", "\n\n"} {
		node, err := Decode("actionlint.toml", []byte(source))
		if err != nil {
			t.Fatalf("%q: %v", source, err)
		}
		if value, ok := node.Value.(map[string]any); !ok || len(value) != 0 {
			t.Fatalf("%q: value=%v", source, node.Value)
		}
	}
}

func TestDecodeSelectsFormatByExtension(t *testing.T) {
	t.Parallel()
	want := map[string]any{"self-hosted-runner": map[string]any{"labels": []any{"gpu"}}}
	json := "{\n\t\"self-hosted-runner\": {\n\t\t\"labels\": [\"gpu\"]\n\t}\n}\n"
	for _, path := range []string{"actionlint.json", "actionlint.yaml", "actionlint.yml", "config"} {
		node, err := Decode(path, []byte(json))
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if diff := cmp.Diff(want, decodeValue(t, node)); diff != "" {
			t.Fatalf("%s: %s", path, diff)
		}
	}
	node, err := Decode("actionlint.TOML", []byte("[self-hosted-runner]\nlabels = [\"gpu\"]\n"))
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(want, decodeValue(t, node)); diff != "" {
		t.Fatal(diff)
	}
	if _, err := Decode("actionlint.yaml", []byte("a: 1\n---\nb: 2\n")); err == nil || err.Error() != "must contain one document" {
		t.Fatalf("multiple documents: %v", err)
	}
	if _, err := Decode("actionlint.toml", []byte("a = 1\na = 2\n")); err == nil {
		t.Fatalf("toml error: %v", err)
	}
}
