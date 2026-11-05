package tree

import (
	"math"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/kjanat/go-yaml/v4"
)

func decodeValue(t *testing.T, node *yaml.Node) any {
	t.Helper()
	var value any
	if err := node.Decode(&value); err != nil {
		t.Fatal(err)
	}
	return value
}

func TestDecodeTOMLValues(t *testing.T) {
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
		"title": "Hello", "count": 1000, "hex": 255, "negative": -7, "ratio": 2.5, "big": 6.02e23,
		"positive_infinity": math.Inf(1), "negative_infinity": math.Inf(-1), "not_a_number": math.NaN(),
		"enabled": true, "date": "2024-01-02", "quoted": "123", "empty": []any{},
		"server": map[string]any{
			"host": "localhost", "ports": []any{80, 443},
			"options": map[string]any{"retries": 3, "verbose": false},
			"tls":     map[string]any{"enabled": false},
		},
		"items": []any{map[string]any{"name": "a"}, map[string]any{"name": "b", "tags": map[string]any{"primary": "x"}}},
	}
	node, err := decodeTOML([]byte(source))
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(want, decodeValue(t, node), cmpopts.EquateNaNs()); diff != "" {
		t.Fatal(diff)
	}
}

func TestDecodeTOMLPositions(t *testing.T) {
	node, err := decodeTOML([]byte("names = [\"é\", \"b\"]\nt = { \"é\" = 1, b = 2 }\n[server]\nhost = \"x\"\n  port = 8080\n"))
	if err != nil {
		t.Fatal(err)
	}
	names := node.Content[1]
	inline := node.Content[3]
	server := node.Content[5]
	positions := map[string]*yaml.Node{
		"names key": node.Content[0], "names value": names, "names second": names.Content[1],
		"inline value": inline, "inline b key": inline.Content[2],
		"server key": node.Content[4], "host key": server.Content[0], "host value": server.Content[1],
		"port key": server.Content[2], "port value": server.Content[3],
	}
	want := map[string][2]int{
		"names key": {1, 1}, "names value": {1, 10}, "names second": {1, 15},
		"inline value": {2, 5}, "inline b key": {2, 16},
		"server key": {3, 2}, "host key": {4, 1}, "host value": {4, 8},
		"port key": {5, 3}, "port value": {5, 10},
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
			_, err := decodeTOML([]byte(test.source))
			if err == nil || err.Error() != test.want {
				t.Fatalf("got %v, want %q", err, test.want)
			}
		})
	}
	_, err := decodeTOML([]byte("a = \n"))
	if err == nil || !strings.HasPrefix(err.Error(), "line ") {
		t.Fatalf("syntax error: %v", err)
	}
}

func TestDecodeTOMLEmptyDocument(t *testing.T) {
	for _, source := range []string{"", "# only a comment\n", "\n\n"} {
		node, err := decodeTOML([]byte(source))
		if err != nil {
			t.Fatalf("%q: %v", source, err)
		}
		if node.Kind != yaml.MappingNode || len(node.Content) != 0 {
			t.Fatalf("%q: kind=%v content=%d", source, node.Kind, len(node.Content))
		}
	}
}

func TestDecodeSelectsFormatByExtension(t *testing.T) {
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
	if _, err := Decode("actionlint.toml", []byte("a = 1\na = 2\n")); err == nil || !strings.Contains(err.Error(), "defined twice") {
		t.Fatalf("toml error: %v", err)
	}
}
