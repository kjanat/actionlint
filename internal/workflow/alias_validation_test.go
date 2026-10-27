package workflow

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/yaml/codec"
	"actionlint.kjanat.dev/v2/internal/yaml/syntax"
)

func TestValidateYAMLAliasesPreservesSource(t *testing.T) {
	for _, tc := range []struct {
		source   string
		messages []string
	}{
		{"base: &base {nested: &nested [value]}\nfirst: *base\nsecond: *base\nchild: *nested\n", nil},
		{"base: &base {self: *base}\ncopy: *base\n", []string{`recursive alias "base"`}},
		{"base: &invalid+name value\ncopy: *invalid+name\nunused: &unused value\n", []string{`anchor name "invalid+name"`, `alias name "invalid+name"`, `anchor "unused" is defined but not used`}},
	} {
		root, err := codec.Parse([]byte(tc.source))
		if err != nil {
			t.Fatal(err)
		}
		before := map[*syntax.Node]syntax.Node{}
		var record func(*syntax.Node)
		record = func(n *syntax.Node) {
			if n == nil {
				return
			}
			if _, seen := before[n]; seen {
				return
			}
			snapshot := *n
			snapshot.Content = slices.Clone(n.Content)
			before[n] = snapshot
			for _, child := range n.Content {
				record(child)
			}
			record(n.Alias)
		}
		record(root)
		type diagnostic struct {
			line, column int
			kind         YamlAliasDiagnostic
			message      string
		}
		var got, want []diagnostic
		ValidateYAMLAliases(root, func(n *syntax.Node, kind YamlAliasDiagnostic, message string) {
			got = append(got, diagnostic{n.Line, n.Column, kind, message})
		})
		if len(got) != len(tc.messages) {
			t.Fatalf("diagnostics = %+v, want %v", got, tc.messages)
		}
		for i, message := range tc.messages {
			if !strings.Contains(got[i].message, message) || got[i].line < 1 || got[i].column < 1 {
				t.Fatalf("diagnostic = %+v, want located %q", got[i], message)
			}
		}
		for node, original := range before {
			if node.Alias != original.Alias || !slices.Equal(node.Content, original.Content) || !reflect.DeepEqual(*node, original) {
				t.Fatalf("validation changed source node at %d:%d", original.Line, original.Column)
			}
		}
		ResolveYAMLAliases(root, func(n *syntax.Node, kind YamlAliasDiagnostic, message string) {
			want = append(want, diagnostic{n.Line, n.Column, kind, message})
		})
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("validation diagnostics = %+v, resolution diagnostics = %+v", got, want)
		}
	}
}
