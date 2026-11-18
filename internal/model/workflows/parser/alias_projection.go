package parser

import (
	"fmt"
	"slices"
	"strings"

	"actionlint.kjanat.dev/v2/internal/languages/yaml/codec"
)

// YamlAliasDiagnostic classifies physical-source alias problems. Rejected uses
// remain aliases in the parser projection; unused anchors do not prevent projection.
type YamlAliasDiagnostic int

const (
	yamlAliasDiagnosticRecursive YamlAliasDiagnostic = iota
	YamlAliasDiagnosticUnusedAnchor
	yamlAliasDiagnosticInvalidName
	yamlAliasDiagnosticUndeclared
)

// A nil node is a broken internal graph, not a YAML diagnostic. Panic
// explicitly instead of relying on an OS fault from a nil dereference.
func requireYAMLAliasNode(node *codec.Node) {
	if node == nil {
		panic("parser: nil node in YAML alias graph")
	}
}

// projectYAMLAliases builds a detached parser graph and alias-use provenance.
// Shared declarations remain shared within the projection; source nodes never change.
// root must be a complete, non-nil, structurally valid graph from codec.Parse.
func projectYAMLAliases(root *codec.Node, uses []yamlAliasUse) (*codec.Node, map[*codec.Node]*codec.Node) {
	copies := map[*codec.Node]*codec.Node{}
	var clone func(*codec.Node) *codec.Node
	clone = func(node *codec.Node) *codec.Node {
		requireYAMLAliasNode(node)
		if existing, found := copies[node]; found {
			return existing
		}
		result := new(*node)
		copies[node] = result
		result.Content = slices.Clone(node.Content)
		for index, child := range node.Content {
			result.Content[index] = clone(child)
		}
		if node.Alias != nil {
			result.Alias = clone(node.Alias)
		}
		return result
	}
	projected := clone(root)
	var aliases map[*codec.Node]*codec.Node
	for _, use := range uses {
		original := use.parent.Content[use.index]
		resolved := new(*copies[original.Alias])
		copies[use.parent].Content[use.index] = resolved
		if aliases == nil {
			aliases = map[*codec.Node]*codec.Node{}
		}
		aliases[resolved] = original
	}
	return projected, aliases
}

// AdmitYAMLAliases applies the codec's resource limit to dialect-approved uses.
// It neither constructs semantic values nor changes the physical source graph.
func AdmitYAMLAliases(root *codec.Node) error {
	uses := inspectYAMLAliases(root, func(*codec.Node, YamlAliasDiagnostic, string) {})
	return admitYAMLAliasUses(root, uses)
}

func admitYAMLAliasUses(root *codec.Node, uses []yamlAliasUse) error {
	if !codec.RequiresAdmission(len(uses)) {
		return nil
	}
	return codec.Admit(root, acceptedYAMLAliasUses(uses))
}

func acceptedYAMLAliasUses(uses []yamlAliasUse) func(*codec.Node, int) bool {
	accepted := make(map[yamlAliasUse]bool, len(uses))
	for _, use := range uses {
		accepted[use] = true
	}
	return func(parent *codec.Node, index int) bool {
		return accepted[yamlAliasUse{parent, index}]
	}
}

// ValidateYAMLAliases only reports diagnostics on a complete, non-nil, structurally valid
// source graph from codec.Parse. Content order defines declarations; shared concrete
// nodes are inspected once and alias diagnostics follow parent slots.
func ValidateYAMLAliases(root *codec.Node, report func(n *codec.Node, d YamlAliasDiagnostic, m string)) {
	inspectYAMLAliases(root, report)
}

type yamlAliasUse struct {
	parent *codec.Node
	index  int
}

func inspectYAMLAliases(root *codec.Node, report func(n *codec.Node, d YamlAliasDiagnostic, m string)) []yamlAliasUse {
	type usage struct {
		used    bool
		defined bool
	}

	anchors := map[*codec.Node]*usage{}
	visited := map[*codec.Node]bool{}
	var declarations []*codec.Node
	var uses []yamlAliasUse

	var resolve func(*codec.Node) // For recursive call
	resolve = func(n *codec.Node) {
		if visited[n] {
			return
		}
		visited[n] = true
		var u *usage
		if len(n.Anchor) != 0 {
			if strings.ContainsRune(n.Anchor, '+') {
				report(n, yamlAliasDiagnosticInvalidName, fmt.Sprintf("anchor name %q contains '+' which is not allowed by GitHub Actions", n.Anchor))
			}
			u = &usage{}
			declarations = append(declarations, n)
			anchors[n] = u
		}
		for i, c := range n.Content {
			requireYAMLAliasNode(c)
			if c.Kind != codec.AliasNode {
				resolve(c)
				continue
			}
			if strings.ContainsRune(c.Value, '+') {
				report(c, yamlAliasDiagnosticInvalidName, fmt.Sprintf("alias name %q contains '+' which is not allowed by GitHub Actions", c.Value))
			}
			if u, ok := anchors[c.Alias]; ok {
				u.used = true
				if u.defined {
					uses = append(uses, yamlAliasUse{n, i})
				} else {
					// Don't resolve the recursive alias because it causes stack overflow on parsing the tree as
					// `RawYAMLValue`. (#610)
					report(c, yamlAliasDiagnosticRecursive, fmt.Sprintf("recursive alias %q is found. anchor was declared at line:%d, column:%d", c.Alias.Anchor, c.Alias.Line, c.Alias.Column))
				}
			} else {
				report(c, yamlAliasDiagnosticUndeclared, fmt.Sprintf("alias %q refers to an anchor not yet declared", c.Value))
			}
		}
		if u != nil {
			u.defined = true
		}
	}
	requireYAMLAliasNode(root)
	resolve(root)

	for _, n := range declarations {
		if !anchors[n].used {
			report(n, YamlAliasDiagnosticUnusedAnchor, fmt.Sprintf("anchor %q is defined but not used", n.Anchor))
		}
	}
	return uses
}
