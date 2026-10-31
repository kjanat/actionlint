package main

import (
	"fmt"
	"slices"
	"strings"
)

// checkGraph returns every measured import that the table rejects.
func checkGraph(t *table, g *graph) []string {
	var violations []string
	for _, pkg := range g.sortedPackages() {
		l := pkg.layer
		for _, imported := range pkg.imports {
			switch {
			case g.isModule(imported):
				target := g.packages[imported]
				if target == nil || target.layer == nil {
					violations = append(violations, fmt.Sprintf("%s imports %s, which is in no layer", pkg.path, imported))
				} else if !l.allows(target.layer) && !t.admits(pkg, imported, false) {
					violations = append(violations, fmt.Sprintf("%s (%s) imports %s (%s)", pkg.path, l.name, imported, target.layer.name))
				}
			case g.standard[imported]:
				if denied := deniedStd(l, imported); denied != "" {
					violations = append(violations, fmt.Sprintf("%s (%s) imports %s, which the layer denies through %s", pkg.path, l.name, imported, denied))
				}
			default:
				module, ok := g.external(imported)
				if !ok {
					violations = append(violations, fmt.Sprintf("%s imports %s, which go list did not resolve", pkg.path, imported))
				} else if !l.allowsExternal(module) {
					violations = append(violations, fmt.Sprintf("%s (%s) imports %s from module %s, which the layer does not list", pkg.path, l.name, imported, module))
				}
			}
		}
		for _, imported := range pkg.testImports {
			switch {
			case g.isModule(imported):
				target := g.packages[imported]
				if target == nil || target.layer == nil {
					violations = append(violations, fmt.Sprintf("tests of %s import %s, which is in no layer", pkg.path, imported))
				} else if !l.testAllows(target.layer) && !t.admits(pkg, imported, true) {
					violations = append(violations, fmt.Sprintf("tests of %s (%s) import %s (%s)", pkg.path, l.name, imported, target.layer.name))
				}
			case g.standard[imported]:
			default:
				module, ok := g.external(imported)
				if !ok {
					violations = append(violations, fmt.Sprintf("tests of %s import %s, which go list did not resolve", pkg.path, imported))
				} else if !l.testAllowsExternal(t, module) {
					violations = append(violations, fmt.Sprintf("tests of %s (%s) import %s from module %s, which no externals or test-externals list names", pkg.path, l.name, imported, module))
				}
			}
		}
	}
	return append(violations, checkStdDenials(t, g)...)
}

// deniedStd returns the deny-std entry of l that covers a standard import.
func deniedStd(l *layer, imported string) string {
	for _, denied := range l.denyStd {
		if imported == denied || strings.HasPrefix(imported, denied+"/") {
			return denied
		}
	}
	return ""
}

// checkStdDenials keeps standard-library denials transitive. A layer that
// denies a package may only reach layers that deny it too, and no member may
// reach it through an external module either.
func checkStdDenials(t *table, g *graph) []string {
	var violations []string
	for _, l := range t.layers {
		reachable := slices.Clone(l.allow)
		for _, member := range l.members {
			for _, e := range t.exceptionTargets(member, false) {
				reachable = append(reachable, g.packages[e.to].layer)
			}
		}
		for _, denied := range l.denyStd {
			for _, target := range reachable {
				if !slices.Contains(target.denyStd, denied) {
					violations = append(violations, fmt.Sprintf("layer %s denies %s but may import layer %s, which does not", l.name, denied, target.name))
				}
			}
			for _, member := range l.members {
				if chain := g.pathTo(member.path, denied); chain != nil {
					violations = append(violations, fmt.Sprintf("%s (%s) reaches denied %s through %s", member.path, l.name, denied, strings.Join(chain, " -> ")))
				}
			}
		}
	}
	return violations
}

// pathTo returns the shortest non-test import chain from start to the
// standard package denied or a package below it, or nil.
func (g *graph) pathTo(start, denied string) []string {
	previous := map[string]string{start: ""}
	queue := []string{start}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if current == denied || strings.HasPrefix(current, denied+"/") {
			var chain []string
			for at := current; at != ""; at = previous[at] {
				chain = append(chain, at)
			}
			slices.Reverse(chain)
			return chain
		}
		for _, next := range g.deps[current] {
			if _, seen := previous[next]; !seen {
				previous[next] = current
				queue = append(queue, next)
			}
		}
	}
	return nil
}
