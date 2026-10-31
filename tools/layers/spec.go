package main

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// specFile is the committed layer table as written in layers.toml.
type specFile struct {
	StdReasons    map[string]string `toml:"std-reasons"`
	TestExternals []string          `toml:"test-externals"`
	Layers        []specLayer       `toml:"layer"`
	Exceptions    []specException   `toml:"exception"`
}

type specLayer struct {
	Name      string   `toml:"name"`
	Doc       string   `toml:"doc"`
	Packages  []string `toml:"packages"`
	Allow     []string `toml:"allow"`
	Isolated  bool     `toml:"isolated"`
	Externals []string `toml:"externals"`
	DenyStd   []string `toml:"deny-std"`
	Tests     []string `toml:"tests"`
	// TestExternals lists modules only this layer's tests may import.
	TestExternals []string `toml:"test-externals"`
}

type specException struct {
	From   string `toml:"from"`
	To     string `toml:"to"`
	Reason string `toml:"reason"`
	Tests  bool   `toml:"tests"`
}

// layer is a validated layer. Allowed layers always come earlier in the
// table, so the allowed-import relation between layers has no cycles.
type layer struct {
	index     int
	name      string
	doc       string
	patterns  []string
	allow     []*layer
	isolated  bool
	externals []string
	denyStd   []string
	tests     []*layer
	// testExternals are modules this layer's tests may import in addition
	// to the table-wide test-externals.
	testExternals []string
	members       []*modulePackage
}

// exception admits one import edge that the layers would reject. A test
// exception admits it for the package's test files only.
type exception struct {
	from, to string
	reason   string
	tests    bool
}

type table struct {
	layers        []*layer
	exceptions    []exception
	stdReasons    map[string]string
	testExternals []string
}

func (t *table) layer(name string) *layer {
	for _, l := range t.layers {
		if l.name == name {
			return l
		}
	}
	return nil
}

// parseSpec decodes and validates the table. Package placement is checked
// later against the measured graph.
func parseSpec(data []byte) (*table, error) {
	var file specFile
	decoder := toml.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&file); err != nil {
		return nil, fmt.Errorf("decode layer spec: %w", err)
	}
	t := &table{stdReasons: file.StdReasons, testExternals: file.TestExternals}
	var errs []error
	for i, s := range file.Layers {
		if s.Name == "" || strings.ContainsAny(s.Name, " ./") {
			errs = append(errs, fmt.Errorf("layer %d: name %q must be a non-empty word without spaces, dots or slashes", i+1, s.Name))
			continue
		}
		if t.layer(s.Name) != nil {
			errs = append(errs, fmt.Errorf("layer %s is declared twice", s.Name))
			continue
		}
		if s.Doc == "" {
			errs = append(errs, fmt.Errorf("layer %s has no doc", s.Name))
		}
		if len(s.Packages) == 0 {
			errs = append(errs, fmt.Errorf("layer %s has no packages", s.Name))
		}
		l := &layer{
			index: i, name: s.Name, doc: s.Doc, patterns: s.Packages, isolated: s.Isolated,
			externals: s.Externals, denyStd: s.DenyStd, testExternals: s.TestExternals,
		}
		resolve := func(field string, names []string) []*layer {
			var resolved []*layer
			for _, name := range names {
				target := t.layer(name)
				switch {
				case name == s.Name:
					errs = append(errs, fmt.Errorf("layer %s lists itself in %s", s.Name, field))
				case target == nil:
					errs = append(errs, fmt.Errorf("layer %s lists %s in %s, which is not a layer declared earlier in the table", s.Name, name, field))
				default:
					resolved = append(resolved, target)
				}
			}
			return resolved
		}
		l.allow = resolve("allow", s.Allow)
		for _, std := range s.DenyStd {
			if t.stdReasons[std] == "" {
				errs = append(errs, fmt.Errorf("layer %s denies %s, which has no entry in std-reasons", s.Name, std))
			}
		}
		t.layers = append(t.layers, l)
	}
	// Test files are outside the layer order: a test may import a harness
	// that sits above the package it tests.
	for _, s := range file.Layers {
		l := t.layer(s.Name)
		if l == nil {
			continue
		}
		for _, name := range s.Tests {
			if target := t.layer(name); target == nil {
				errs = append(errs, fmt.Errorf("layer %s lists %s in tests, which is not a layer", s.Name, name))
			} else {
				l.tests = append(l.tests, target)
			}
		}
	}
	for _, e := range file.Exceptions {
		if e.From == "" || e.To == "" || e.Reason == "" {
			errs = append(errs, fmt.Errorf("exception %s -> %s needs from, to and reason", e.From, e.To))
			continue
		}
		t.exceptions = append(t.exceptions, exception{from: e.From, to: e.To, reason: e.Reason, tests: e.Tests})
	}
	return t, errors.Join(errs...)
}

// matchPattern reports whether a module-relative package path matches a spec
// pattern. "." names the module root; a trailing "/..." also matches every
// package below the directory.
func matchPattern(pattern, rel string) bool {
	if pattern == "." {
		return rel == ""
	}
	if base, ok := strings.CutSuffix(pattern, "/..."); ok {
		return rel == base || strings.HasPrefix(rel, base+"/")
	}
	return rel == pattern
}

// place assigns every module package to exactly one layer.
func (t *table) place(g *graph) error {
	var errs []error
	used := map[*layer]map[string]bool{}
	for _, pkg := range g.sortedPackages() {
		var owners []*layer
		for _, l := range t.layers {
			for _, pattern := range l.patterns {
				if matchPattern(pattern, pkg.rel) {
					owners = append(owners, l)
					if used[l] == nil {
						used[l] = map[string]bool{}
					}
					used[l][pattern] = true
					break
				}
			}
		}
		switch len(owners) {
		case 0:
			errs = append(errs, fmt.Errorf("package %s is in no layer; add it to tools/layers/layers.toml", pkg.path))
		case 1:
			pkg.layer = owners[0]
			owners[0].members = append(owners[0].members, pkg)
		default:
			names := make([]string, len(owners))
			for i, owner := range owners {
				names[i] = owner.name
			}
			errs = append(errs, fmt.Errorf("package %s is in more than one layer: %s", pkg.path, strings.Join(names, ", ")))
		}
	}
	for _, l := range t.layers {
		for _, pattern := range l.patterns {
			if !used[l][pattern] {
				errs = append(errs, fmt.Errorf("layer %s: pattern %q matches no package", l.name, pattern))
			}
		}
	}
	for i, e := range t.exceptions {
		from, to := g.packages[g.importPath(e.from)], g.packages[g.importPath(e.to)]
		if from == nil || to == nil {
			errs = append(errs, fmt.Errorf("exception %s -> %s names a package that does not exist", e.from, e.to))
			continue
		}
		imports, allows := from.imports, (*layer).allows
		if e.tests {
			imports, allows = from.testImports, (*layer).testAllows
		}
		if !slices.Contains(imports, to.path) {
			errs = append(errs, fmt.Errorf("exception %s -> %s is unused: the package does not import it", e.from, e.to))
		}
		if from.layer != nil && to.layer != nil && allows(from.layer, to.layer) {
			errs = append(errs, fmt.Errorf("exception %s -> %s is redundant: layer %s already allows layer %s", e.from, e.to, from.layer.name, to.layer.name))
		}
		t.exceptions[i].from, t.exceptions[i].to = from.path, to.path
	}
	return errors.Join(errs...)
}

// allows reports whether non-test files of l may import packages of target.
func (l *layer) allows(target *layer) bool {
	return (target == l && !l.isolated) || slices.Contains(l.allow, target)
}

// testAllows reports whether test files of l may import packages of target.
func (l *layer) testAllows(target *layer) bool {
	return target == l || l.allows(target) || slices.Contains(l.tests, target)
}

func (l *layer) allowsExternal(module string) bool {
	return slices.Contains(l.externals, module)
}

func (l *layer) testAllowsExternal(t *table, module string) bool {
	return l.allowsExternal(module) || slices.Contains(l.testExternals, module) || slices.Contains(t.testExternals, module)
}

// exceptionTargets lists the exceptions that apply to pkg's non-test files,
// or with tests set, to its test files, which also get the non-test ones.
func (t *table) exceptionTargets(pkg *modulePackage, tests bool) []exception {
	var targets []exception
	for _, e := range t.exceptions {
		if e.from == pkg.path && (tests || !e.tests) {
			targets = append(targets, e)
		}
	}
	return targets
}

// admits reports whether an exception lets pkg import target.
func (t *table) admits(pkg *modulePackage, target string, tests bool) bool {
	return slices.ContainsFunc(t.exceptionTargets(pkg, tests), func(e exception) bool { return e.to == target })
}
