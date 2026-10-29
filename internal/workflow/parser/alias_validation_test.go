package parser

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/yaml/codec"
	"actionlint.kjanat.dev/v2/internal/yaml/syntax"
)

func TestUnusedYAMLAnchorsReportInDeclarationOrder(t *testing.T) {
	const source = "first: &zeta {child: &alpha value}\nused: &used value\ncopy: *used\nlast: &omega value\n"
	root, err := codec.Parse([]byte(source))
	if err != nil {
		t.Fatal(err)
	}
	for range 32 {
		var names []string
		ValidateYAMLAliases(root, func(node *syntax.Node, kind YamlAliasDiagnostic, message string) {
			if kind != YamlAliasDiagnosticUnusedAnchor {
				t.Fatalf("unexpected alias diagnostic: %s", message)
			}
			names = append(names, node.Anchor)
		})
		if !slices.Equal(names, []string{"zeta", "alpha", "omega"}) {
			t.Fatalf("unused anchors out of declaration order: %v", names)
		}
	}
}

func TestYAMLAliasInspectionVisitsSharedDeclarationsOnce(t *testing.T) {
	for _, source := range []string{
		"anchor: &shared value\nuse: *shared\ncopy: []\n",
		"anchor: &shared value\nnested: {ref: *shared}\ncopy: []\n",
	} {
		for _, resolve := range []bool{false, true} {
			root, err := codec.Parse([]byte(source))
			if err != nil {
				t.Fatal(err)
			}
			mapping := root.Content[0]
			shared := mapping.Content[1]
			if strings.Contains(source, "nested:") {
				shared = mapping.Content[3]
			}
			mapping.Content[5].Content = []*syntax.Node{shared}
			session, _, err := codec.Open(root)
			if err != nil {
				t.Fatalf("shared source graph rejected: %v", err)
			}
			session.Close()
			report := func(node *syntax.Node, kind YamlAliasDiagnostic, message string) {
				t.Errorf("resolve=%t: shared declaration produced diagnostic: %s", resolve, message)
			}
			if resolve {
				before := snapshotAliasTestGraph(root)
				projected, aliases := projectAliasTestGraph(root, report)
				if len(aliases) != 1 {
					t.Fatalf("resolved aliases=%d, want one physical source alias", len(aliases))
				}
				projectedMapping := projected.Content[0]
				projectedShared := projectedMapping.Content[1]
				originalAlias := mapping.Content[3]
				projectedAlias := projectedMapping.Content[3]
				if strings.Contains(source, "nested:") {
					projectedShared = projectedMapping.Content[3]
					originalAlias = mapping.Content[3].Content[1]
					projectedAlias = projectedShared.Content[1]
				}
				if projectedMapping.Content[5].Content[0] != projectedShared {
					t.Fatal("projection lost shared concrete declaration")
				}
				if aliases[projectedAlias] != originalAlias {
					t.Fatal("projection lost original source alias provenance")
				}
				assertAliasTestProjection(t, projected, before)
			} else {
				ValidateYAMLAliases(root, report)
			}
		}
	}
}

func TestYAMLAliasInspectionRejectsForwardSharedSource(t *testing.T) {
	for _, resolve := range []bool{false, true} {
		root, err := codec.Parse([]byte("root: &a {nested: {ref: *a}}\nfirst: []\n"))
		if err != nil {
			t.Fatal(err)
		}
		mapping := root.Content[0]
		anchor := mapping.Content[1]
		nested := anchor.Content[1]
		alias := nested.Content[1]
		first := mapping.Content[3]
		first.Content = []*syntax.Node{nested}
		mapping.Content = []*syntax.Node{mapping.Content[2], first, mapping.Content[0], anchor}
		var kinds []YamlAliasDiagnostic
		report := func(node *syntax.Node, kind YamlAliasDiagnostic, message string) {
			kinds = append(kinds, kind)
			if kind == yamlAliasDiagnosticUndeclared && (node != alias || message != `alias "a" refers to an anchor not yet declared`) {
				t.Errorf("forward alias diagnostic lost source: %s", message)
			}
		}
		if resolve {
			before := snapshotAliasTestGraph(root)
			projected, aliases := projectAliasTestGraph(root, report)
			if len(aliases) != 0 {
				t.Fatal("forward alias was projected")
			}
			projectedMapping := projected.Content[0]
			projectedAnchor := projectedMapping.Content[3]
			projectedNested := projectedAnchor.Content[1]
			projectedAlias := projectedNested.Content[1]
			if projectedMapping.Content[1].Content[0] != projectedNested || projectedAlias.Kind != syntax.AliasNode || projectedAlias.Alias != projectedAnchor {
				t.Fatal("projection lost unresolved shared forward alias graph")
			}
			assertAliasTestProjection(t, projected, before)
		} else {
			ValidateYAMLAliases(root, report)
		}
		if !slices.Equal(kinds, []YamlAliasDiagnostic{yamlAliasDiagnosticUndeclared, YamlAliasDiagnosticUnusedAnchor}) || nested.Content[1] != alias {
			t.Fatalf("forward source diagnostics=%v, alias=%+v", kinds, nested.Content[1])
		}
	}
}

func TestYAMLAliasInspectionKeepsSharedRecursiveAliasUnresolved(t *testing.T) {
	for _, resolve := range []bool{false, true} {
		root, err := codec.Parse([]byte("root: &a {nested: {ref: *a}}\ncopy: []\n"))
		if err != nil {
			t.Fatal(err)
		}
		mapping := root.Content[0]
		anchor := mapping.Content[1]
		nested := anchor.Content[1]
		alias := nested.Content[1]
		mapping.Content[3].Content = []*syntax.Node{nested}
		count := 0
		report := func(node *syntax.Node, kind YamlAliasDiagnostic, message string) {
			count++
			if kind != yamlAliasDiagnosticRecursive || node != alias {
				t.Errorf("recursive source diagnostic=%s kind=%v node=%p want %p", message, kind, node, alias)
			}
		}
		if resolve {
			before := snapshotAliasTestGraph(root)
			projected, aliases := projectAliasTestGraph(root, report)
			if len(aliases) != 0 {
				t.Fatal("recursive alias was projected")
			}
			projectedMapping := projected.Content[0]
			projectedAnchor := projectedMapping.Content[1]
			projectedNested := projectedAnchor.Content[1]
			projectedAlias := projectedNested.Content[1]
			if projectedMapping.Content[3].Content[0] != projectedNested || projectedAlias.Kind != syntax.AliasNode || projectedAlias.Alias != projectedAnchor {
				t.Fatal("projection lost unresolved shared recursive alias graph")
			}
			assertAliasTestProjection(t, projected, before)
			session, _, err := codec.Open(projected)
			if err != nil {
				t.Fatalf("projection introduced Content cycle: %v", err)
			}
			session.Close()
		} else {
			ValidateYAMLAliases(root, report)
		}
		if count != 1 || nested.Content[1] != alias || alias.Alias != anchor {
			t.Fatalf("recursive source changed or diagnostics repeated: count=%d", count)
		}
		session, _, err := codec.Open(root)
		if err != nil {
			t.Fatalf("alias projection introduced a Content cycle: %v", err)
		}
		session.Close()
	}
}

func TestSharedAliasDiagnosticsAndProjectionFollowParentSlots(t *testing.T) {
	for _, sharedBacking := range []bool{false, true} {
		for _, resolve := range []bool{false, true} {
			root, err := codec.Parse([]byte("anchor: &invalid+name value\nfirst: [*invalid+name]\nsecond: []\n"))
			if err != nil {
				t.Fatal(err)
			}
			mapping := root.Content[0]
			first, second := mapping.Content[3], mapping.Content[5]
			originalContent := first.Content
			alias := first.Content[0]
			second.Content = []*syntax.Node{alias}
			if sharedBacking {
				second.Content = first.Content
			}
			anchorReports, aliasReports := 0, 0
			report := func(node *syntax.Node, kind YamlAliasDiagnostic, message string) {
				if kind != yamlAliasDiagnosticInvalidName {
					t.Errorf("unexpected diagnostic: %s", message)
				}
				switch node {
				case alias:
					aliasReports++
				case mapping.Content[1]:
					anchorReports++
				default:
					t.Error("diagnostic lost original node")
				}
			}
			if resolve {
				before := snapshotAliasTestGraph(root)
				projected, aliases := projectAliasTestGraph(root, report)
				projectedMapping := projected.Content[0]
				projectedFirst, projectedSecond := projectedMapping.Content[3], projectedMapping.Content[5]
				if len(aliases) != 2 || projectedFirst.Content[0] == projectedSecond.Content[0] || aliases[projectedFirst.Content[0]] != alias || aliases[projectedSecond.Content[0]] != alias {
					t.Fatalf("parent projections lost reverse source identity: %+v", aliases)
				}
				if projectedFirst.Content[0].Value != "value" || projectedSecond.Content[0].Value != "value" {
					t.Fatal("shared alias projection lost target value")
				}
				if first.Content[0] != alias || second.Content[0] != alias || originalContent[0] != alias || alias.Kind != syntax.AliasNode || alias.Alias != mapping.Content[1] {
					t.Fatal("projection mutated original backing or alias source")
				}
				assertAliasTestProjection(t, projected, before)
			} else {
				ValidateYAMLAliases(root, report)
				if first.Content[0] != alias || second.Content[0] != alias {
					t.Fatal("validation changed alias source")
				}
			}
			if anchorReports != 1 || aliasReports != 2 {
				t.Fatalf("diagnostics do not follow parent slots: anchor=%d alias=%d", anchorReports, aliasReports)
			}
		}
	}
}

func TestWorkflowParserRecordsUndeclaredAliasDiagnostic(t *testing.T) {
	const source = "anchor: &a value\nalias: *a\n"
	root, err := codec.Parse([]byte(source))
	if err != nil {
		t.Fatal(err)
	}
	mapping := root.Content[0]
	alias, anchor := mapping.Content[3], mapping.Content[1]
	mapping.Content = []*syntax.Node{mapping.Content[2], alias, mapping.Content[0], anchor}
	before := snapshotAliasTestGraph(root)
	_, diagnostics := parseDocument([]byte(source), root)
	if len(diagnostics) < 2 {
		t.Fatalf("missing alias diagnostics: %+v", diagnostics)
	}
	for i, want := range []struct {
		node    *syntax.Node
		message string
	}{
		{alias, `alias "a" refers to an anchor not yet declared`},
		{anchor, `anchor "a" is defined but not used`},
	} {
		got := diagnostics[i]
		if got.Message != want.message || got.Line != want.node.Line || got.Column != want.node.Column || got.Kind != "syntax-check" {
			t.Errorf("alias diagnostic %d=%+v want %q at %d:%d", i, got, want.message, want.node.Line, want.node.Column)
		}
	}
	assertAliasTestSourceUnchanged(t, before)

	// Raw-value parsing is a separate leaf-parser contract, with fresh error state.
	parser := NewSourceParser([]byte(source))
	projected := projectAliasTestInput(parser, root)
	if len(parser.aliases) != 0 {
		t.Fatal("parser projected rejected alias")
	}
	projectedAlias := projected.Content[0].Content[1]
	if projectedAlias.Kind != syntax.AliasNode || projectedAlias.Alias != projected.Content[0].Content[3] {
		t.Fatal("rejected alias lost projected graph target")
	}
	assertAliasTestProjection(t, projected, before)
	leaf := NewSourceParser([]byte(source))
	if value := leaf.ParseRawYAMLValue(projectedAlias); value != nil || len(leaf.Errors) != 1 || leaf.Errors[0].Message != "unexpected alias node on parsing value in matrix row" || leaf.Errors[0].Line != alias.Line || leaf.Errors[0].Column != alias.Column {
		t.Fatalf("raw value followed unresolved alias: value=%+v errors=%+v", value, leaf.Errors)
	}
}

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
		before := snapshotAliasTestGraph(root)
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
		projected, _ := projectAliasTestGraph(root, func(n *syntax.Node, kind YamlAliasDiagnostic, message string) {
			want = append(want, diagnostic{n.Line, n.Column, kind, message})
		})
		assertAliasTestProjection(t, projected, before)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("validation diagnostics = %+v, resolution diagnostics = %+v", got, want)
		}
	}
}

func snapshotAliasTestGraph(root *syntax.Node) map[*syntax.Node]syntax.Node {
	result := map[*syntax.Node]syntax.Node{}
	var visit func(*syntax.Node)
	visit = func(node *syntax.Node) {
		if node == nil {
			return
		}
		if _, seen := result[node]; seen {
			return
		}
		snapshot := *node
		snapshot.Content = slices.Clone(node.Content)
		result[node] = snapshot
		for _, child := range node.Content {
			visit(child)
		}
		visit(node.Alias)
	}
	visit(root)
	return result
}

func assertAliasTestProjection(t *testing.T, projected *syntax.Node, originals map[*syntax.Node]syntax.Node) {
	t.Helper()
	assertAliasTestSourceUnchanged(t, originals)
	originalSlots := map[**syntax.Node]bool{}
	for node := range originals {
		for i := range node.Content {
			originalSlots[&node.Content[i]] = true
		}
	}
	seen := map[*syntax.Node]bool{}
	var visit func(*syntax.Node)
	visit = func(node *syntax.Node) {
		if node == nil || seen[node] {
			return
		}
		seen[node] = true
		if _, source := originals[node]; source {
			t.Fatalf("projection retained source pointer at %d:%d", node.Line, node.Column)
		}
		for i := range node.Content {
			if originalSlots[&node.Content[i]] {
				t.Fatalf("projection retained source Content backing at %d:%d", node.Line, node.Column)
			}
		}
		for _, child := range node.Content {
			visit(child)
		}
		visit(node.Alias)
	}
	visit(projected)
}

func projectAliasTestGraph(root *syntax.Node, report func(*syntax.Node, YamlAliasDiagnostic, string)) (*syntax.Node, map[*syntax.Node]*syntax.Node) {
	return projectYAMLAliases(root, inspectYAMLAliases(root, report))
}

// Leaf-parser fixtures opt into projection; production construction belongs to parseDocument.
func projectAliasTestInput(parser *Parser, root *syntax.Node) *syntax.Node {
	projected, aliases := projectAliasTestGraph(root, func(node *syntax.Node, _ YamlAliasDiagnostic, message string) { parser.error(node, message) })
	parser.aliases = aliases
	return projected
}

func assertAliasTestSourceUnchanged(t *testing.T, originals map[*syntax.Node]syntax.Node) {
	t.Helper()
	for node, before := range originals {
		if node.Alias != before.Alias || !slices.Equal(node.Content, before.Content) || !reflect.DeepEqual(*node, before) {
			t.Fatalf("projection mutated source at %d:%d", before.Line, before.Column)
		}
	}
}
