package main

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path"
	"slices"
	"sort"
	"strings"
)

// rule is one generated depguard rule. Strict rules admit only the allow
// list minus the deny list; the catch-all rule only denies.
type rule struct {
	name    string
	comment []string
	files   []string
	strict  bool
	allow   []string
	deny    []denial
	// owners are the packages whose non-test or test files the rule is
	// written for, used to verify the file globs.
	owners []*modulePackage
	tests  bool
}

type denial struct {
	pkg, desc string
}

const catchAllName = "layers-unplaced"

// buildRules turns the placed table into depguard rules: one strict rule per
// layer for non-test files, one per layer for test files, separate rules for
// each package with exceptions, and a catch-all for unplaced packages.
func buildRules(t *table, g *graph) []rule {
	var rules []rule
	for _, l := range t.layers {
		var plain, plainTests, excepted []*modulePackage
		for _, member := range l.members {
			if !member.testOnly() && len(t.exceptionTargets(member, false)) == 0 {
				plain = append(plain, member)
			}
			if len(member.testFiles) > 0 && len(t.exceptionTargets(member, true)) == 0 {
				plainTests = append(plainTests, member)
			}
			if len(t.exceptionTargets(member, true)) > 0 {
				excepted = append(excepted, member)
			}
		}
		if len(plain) > 0 {
			rules = append(rules, layerRule("layer-"+l.name, layerComment(l), l, plain, nil, false, t, g))
		}
		if len(plainTests) > 0 {
			rules = append(rules, layerRule("layer-"+l.name+"-tests", nil, l, plainTests, nil, true, t, g))
		}
		for _, member := range excepted {
			name := "layer-" + l.name + "--" + slug(member.rel)
			for _, tests := range []bool{false, true} {
				extra := t.exceptionTargets(member, tests)
				if len(extra) == 0 || tests && len(member.testFiles) == 0 || !tests && member.testOnly() {
					continue
				}
				var comment []string
				for _, e := range extra {
					if e.tests == tests {
						comment = append(comment, wrapComment(fmt.Sprintf("Exception in layer %s: %s imports %s. %s",
							l.name, strings.TrimPrefix(member.path, g.module+"/"), strings.TrimPrefix(e.to, g.module+"/"), e.reason))...)
					}
				}
				ruleName := name
				if tests {
					ruleName += "-tests"
				}
				rules = append(rules, layerRule(ruleName, comment, l, []*modulePackage{member}, extra, tests, t, g))
			}
		}
	}
	return append(rules, catchAll(g))
}

func layerComment(l *layer) []string {
	comment := wrapComment(fmt.Sprintf("Layer %s: %s", l.name, l.doc))
	var allowed []string
	if !l.isolated {
		allowed = append(allowed, "itself")
	}
	for _, target := range l.allow {
		allowed = append(allowed, target.name)
	}
	if len(allowed) == 0 {
		return append(comment, "# Imports no module package.")
	}
	return append(comment, "# Imports: "+strings.Join(allowed, ", ")+".")
}

func wrapComment(text string) []string {
	var lines []string
	line := "#"
	for word := range strings.FieldsSeq(text) {
		if len(line)+1+len(word) > 100 && line != "#" {
			lines = append(lines, line)
			line = "#"
		}
		line += " " + word
	}
	return append(lines, line)
}

func slug(rel string) string {
	if rel == "" {
		return "root"
	}
	return strings.ReplaceAll(rel, "/", "-")
}

func layerRule(name string, comment []string, l *layer, owners []*modulePackage, extra []exception, tests bool, t *table, g *graph) rule {
	r := rule{name: name, comment: comment, strict: true, owners: owners, tests: tests}
	for _, owner := range owners {
		r.files = append(r.files, packageGlobs(owner, tests)...)
	}
	r.files = append(r.files, negations(r.files, owners, tests, g)...)
	if !tests {
		r.files = append(r.files, "!$test")
	}
	allowed := map[string]bool{}
	for _, pkg := range g.sortedPackages() {
		if pkg.testOnly() {
			continue
		}
		if tests && l.testAllows(pkg.layer) || !tests && l.allows(pkg.layer) {
			allowed[pkg.path] = true
		}
	}
	for _, e := range extra {
		allowed[e.to] = true
	}
	r.allow = []string{"$gostd"}
	for _, pkg := range slices.Sorted(maps.Keys(allowed)) {
		r.allow = append(r.allow, pkg+"$")
	}
	externals := slices.Clone(l.externals)
	if tests {
		externals = slices.Concat(externals, l.testExternals, t.testExternals)
	}
	slices.Sort(externals)
	for _, module := range slices.Compact(externals) {
		r.allow = append(r.allow, module+"$", module+"/")
	}
	if !tests {
		for _, denied := range l.denyStd {
			r.deny = append(r.deny, denial{pkg: denied, desc: t.stdReasons[denied]})
		}
	}
	return r
}

// packageGlobs matches a package's files by directory. The root package has
// no directory below the module, so its files are named one by one.
func packageGlobs(pkg *modulePackage, tests bool) []string {
	if pkg.rel == "" {
		files := pkg.files
		if tests {
			files = pkg.testFiles
		}
		globs := make([]string, 0, len(files))
		for _, file := range files {
			globs = append(globs, "**/"+file)
		}
		return globs
	}
	if tests {
		return []string{"**/" + pkg.rel + "/*_test.go"}
	}
	return []string{"**/" + pkg.rel + "/*.go"}
}

// negations excludes the packages whose directories end with the same path
// as an owner's directory, such as internal/config for the public config.
func negations(globs []string, owners []*modulePackage, tests bool, g *graph) []string {
	var excluded []string
	for _, pkg := range g.sortedPackages() {
		if slices.Contains(owners, pkg) {
			continue
		}
		files := pkg.files
		if tests {
			files = pkg.testFiles
		}
		for _, file := range files {
			if matchesAny(globs, nil, pkg.filePath(file)) {
				for _, negation := range packageGlobs(pkg, tests) {
					excluded = append(excluded, "!"+negation)
				}
				break
			}
		}
	}
	return slices.Compact(excluded)
}

func catchAll(g *graph) rule {
	r := rule{
		name: catchAllName,
		comment: []string{
			"# Files of a package that no layer holds may not import module packages.",
			"# Add the package to a layer in tools/layers/layers.toml.",
			"#",
			"# depguard matches globs against absolute paths, so a glob cannot start at the",
			"# module root. A new package whose directory ends with a placed package's path",
			"# (internal/x/config ends with config) or that has a file named like a root",
			"# package file (generate.go) falls under that package's rule instead of this one.",
			"# `go run ./tools/layers -check`, which `make lint` runs, reports every package",
			"# that is in no layer.",
		},
		files: []string{"$all"},
		deny:  []denial{{pkg: g.module, desc: "the package is in no layer; add it to tools/layers/layers.toml"}},
	}
	for _, pkg := range g.sortedPackages() {
		for _, glob := range packageGlobs(pkg, false) {
			r.files = append(r.files, "!"+glob)
		}
		if pkg.rel == "" {
			for _, glob := range packageGlobs(pkg, true) {
				r.files = append(r.files, "!"+glob)
			}
		}
	}
	return r
}

// The functions below reproduce depguard v2 matching so the generator can
// prove the emitted rules mean what the table says.

// expandFile mirrors depguard's $all and $test expansion.
func expandFile(pattern string) string {
	switch pattern {
	case "$all":
		return "**/*.go"
	case "$test":
		return "**/*_test.go"
	}
	return pattern
}

// globMatch implements the glob forms the generator emits: "**/" followed
// by an optional directory and a final element that may contain "*".
func globMatch(pattern, file string) bool {
	rest, ok := strings.CutPrefix(expandFile(pattern), "**/")
	if !ok {
		panic("unsupported glob " + pattern)
	}
	dir, base := path.Split(rest)
	fileDir, fileBase := path.Split(file)
	if matched, err := path.Match(base, fileBase); err != nil || !matched {
		return false
	}
	return dir == "" || strings.HasSuffix("/"+fileDir, "/"+dir)
}

func matchesAny(files, negations []string, file string) bool {
	return slices.ContainsFunc(files, func(p string) bool { return globMatch(p, file) }) &&
		!slices.ContainsFunc(negations, func(p string) bool { return globMatch(p, file) })
}

type compiledRule struct {
	rule
	positive, negative  []string
	allowList, denyList []string
}

func compile(r rule, gostd []string) compiledRule {
	c := compiledRule{rule: r}
	for _, file := range r.files {
		if negated, ok := strings.CutPrefix(file, "!"); ok {
			c.negative = append(c.negative, negated)
		} else {
			c.positive = append(c.positive, file)
		}
	}
	for _, entry := range r.allow {
		if entry == "$gostd" {
			c.allowList = append(c.allowList, gostd...)
		} else {
			c.allowList = append(c.allowList, entry)
		}
	}
	for _, d := range r.deny {
		c.denyList = append(c.denyList, d.pkg)
	}
	sort.Strings(c.allowList)
	sort.Strings(c.denyList)
	return c
}

func (c compiledRule) matches(file string) bool {
	return (len(c.positive) == 0 || matchesAny(c.positive, nil, file)) && !matchesAny(c.negative, nil, file)
}

// inPrefixList is depguard's strInPrefixList: it checks only the greatest
// entry that sorts at or before the import.
func inPrefixList(imported string, list []string) (bool, int) {
	i := sort.Search(len(list), func(i int) bool {
		return strings.TrimRight(list[i], "$") > imported
	}) - 1
	if i == -1 {
		return false, i
	}
	entry := list[i]
	if exact, ok := strings.CutSuffix(entry, "$"); ok {
		return imported == exact, i
	}
	if !strings.ContainsAny(entry, "./") && strings.ContainsRune(imported, '.') {
		return false, i
	}
	return strings.HasPrefix(imported, entry), i
}

func (c compiledRule) allows(imported string) bool {
	inAllowed, a := inPrefixList(imported, c.allowList)
	inDenied, d := inPrefixList(imported, c.denyList)
	if c.strict {
		return inAllowed && (!inDenied || len(c.allowList[a]) > len(c.denyList[d]))
	}
	return (len(c.allowList) == 0 || inAllowed) && !inDenied
}

// gostdPrefixes mirrors depguard's $gostd: every directory under GOROOT/src.
func gostdPrefixes(goroot string) ([]string, error) {
	entries, err := os.ReadDir(path.Join(goroot, "src"))
	if err != nil {
		return nil, err
	}
	var prefixes []string
	for _, entry := range entries {
		if entry.IsDir() {
			prefixes = append(prefixes, entry.Name())
		}
	}
	return prefixes, nil
}

// verifyRules runs the depguard model over every module file. Each file must
// fall under exactly one layer rule, and for every importable module package
// the rules must agree with the table. Measured standard and external
// imports must pass.
func verifyRules(t *table, g *graph, rules []rule) error {
	gostd, err := gostdPrefixes(g.goroot)
	if err != nil {
		return err
	}
	compiled := make([]compiledRule, len(rules))
	for i, r := range rules {
		compiled[i] = compile(r, gostd)
	}
	var errs []error
	importable := slices.DeleteFunc(g.sortedPackages(), (*modulePackage).testOnly)
	for _, pkg := range g.sortedPackages() {
		for _, tests := range []bool{false, true} {
			files := pkg.files
			if tests {
				files = pkg.testFiles
			}
			for _, file := range files {
				var applied []compiledRule
				var names []string
				for _, c := range compiled {
					if c.matches(pkg.filePath(file)) {
						applied = append(applied, c)
						names = append(names, c.name)
					}
				}
				if len(applied) != 1 || !slices.Contains(applied[0].owners, pkg) || applied[0].tests != tests {
					errs = append(errs, fmt.Errorf("%s: generated rules %v apply, want exactly the rule for its layer", pkg.filePath(file), names))
					continue
				}
				allowed := func(imported string) bool {
					return !slices.ContainsFunc(applied, func(c compiledRule) bool { return !c.allows(imported) })
				}
				for _, target := range importable {
					want := pkg.layer.allows(target.layer) || t.admits(pkg, target.path, tests)
					if tests {
						want = want || pkg.layer.testAllows(target.layer)
					}
					if got := allowed(target.path); got != want {
						errs = append(errs, fmt.Errorf("%s: depguard would %s %s, the table says %s", pkg.filePath(file), verdict(got), target.path, verdict(want)))
					}
				}
				imports := pkg.imports
				if tests {
					imports = pkg.testImports
				}
				for _, imported := range imports {
					if !g.isModule(imported) && !allowed(imported) {
						errs = append(errs, fmt.Errorf("%s: depguard would reject the measured import %s", pkg.filePath(file), imported))
					}
				}
			}
		}
	}
	return errors.Join(errs...)
}

func verdict(allowed bool) string {
	if allowed {
		return "allow"
	}
	return "deny"
}
