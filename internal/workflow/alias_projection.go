package workflow

import (
	"fmt"
	"slices"
	"strings"

	"actionlint.kjanat.dev/v2/internal/yaml/codec"
	"actionlint.kjanat.dev/v2/internal/yaml/syntax"
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

// projectYAMLAliases builds a detached parser graph and alias-use provenance.
// Shared declarations remain shared within the projection; source nodes never change.
// root must be a complete, non-nil, structurally valid graph from codec.Parse.
func projectYAMLAliases(root *syntax.Node, uses []yamlAliasUse) (*syntax.Node, map[*syntax.Node]*syntax.Node) {
	copies := map[*syntax.Node]*syntax.Node{}
	var clone func(*syntax.Node) *syntax.Node
	clone = func(node *syntax.Node) *syntax.Node {
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
	var aliases map[*syntax.Node]*syntax.Node
	for _, use := range uses {
		original := use.parent.Content[use.index]
		resolved := new(*copies[original.Alias])
		copies[use.parent].Content[use.index] = resolved
		if aliases == nil {
			aliases = map[*syntax.Node]*syntax.Node{}
		}
		aliases[resolved] = original
	}
	return projected, aliases
}

// AdmitYAMLAliases applies the codec's resource limit to dialect-approved uses.
// It neither constructs semantic values nor changes the physical source graph.
func AdmitYAMLAliases(root *syntax.Node) error {
	uses := inspectYAMLAliases(root, func(*syntax.Node, YamlAliasDiagnostic, string) {})
	return admitYAMLAliasUses(root, uses)
}

func admitYAMLAliasUses(root *syntax.Node, uses []yamlAliasUse) error {
	if !codec.RequiresAdmission(len(uses)) {
		return nil
	}
	return codec.Admit(root, acceptedYAMLAliasUses(uses))
}

func acceptedYAMLAliasUses(uses []yamlAliasUse) func(*syntax.Node, int) bool {
	accepted := make(map[yamlAliasUse]bool, len(uses))
	for _, use := range uses {
		accepted[use] = true
	}
	return func(parent *syntax.Node, index int) bool {
		return accepted[yamlAliasUse{parent, index}]
	}
}

// ValidateYAMLAliases only reports diagnostics on a complete, non-nil, structurally valid
// source graph from codec.Parse. Content order defines declarations; shared concrete
// nodes are inspected once and alias diagnostics follow parent slots.
func ValidateYAMLAliases(root *syntax.Node, report func(n *syntax.Node, d YamlAliasDiagnostic, m string)) {
	inspectYAMLAliases(root, report)
}

type yamlAliasUse struct {
	parent *syntax.Node
	index  int
}

func inspectYAMLAliases(root *syntax.Node, report func(n *syntax.Node, d YamlAliasDiagnostic, m string)) []yamlAliasUse {
	type usage struct {
		used    bool
		defined bool
	}

	anchors := map[*syntax.Node]*usage{}
	visited := map[*syntax.Node]bool{}
	var declarations []*syntax.Node
	var uses []yamlAliasUse

	var resolve func(*syntax.Node) // For recursive call
	resolve = func(n *syntax.Node) {
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
			if c.Kind != syntax.AliasNode {
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
	resolve(root)

	for _, n := range declarations {
		if !anchors[n].used {
			report(n, YamlAliasDiagnosticUnusedAnchor, fmt.Sprintf("anchor %q is defined but not used", n.Anchor))
		}
	}
	return uses
}
