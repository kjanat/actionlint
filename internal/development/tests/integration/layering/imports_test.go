// Package layering checks the dependency direction between internal layers.
package layering

import (
	"maps"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

const module = "actionlint.kjanat.dev/v2/internal/"

// higherLayers lists, for each lower layer, the layers its production code
// must not depend on, directly or through any other package. Lower layers
// describe documents, read inputs and publish results. Rule implementations
// and the executor in internal/checking, and the plugin hosts, registries and
// manifests in internal/plugins, build on them.
var higherLayers = map[string][]string{
	"model":   {"checking", "plugins"},
	"input":   {"checking", "plugins"},
	"results": {"checking", "plugins"},
}

// TestLowerLayersDoNotDependOnHigherLayers keeps rule identity, tool
// settings, plugin references and other shared declarations below the
// checks and plugin hosts that use them. Tests may still import
// internal/checking or internal/plugins to exercise a lower package through
// the engine, so only production imports are followed.
func TestLowerLayersDoNotDependOnHigherLayers(t *testing.T) {
	if testing.Short() {
		t.Skip("lists every internal package and its dependencies")
	}
	t.Parallel()
	config := &packages.Config{Mode: packages.NeedName | packages.NeedImports | packages.NeedDeps, Dir: "../../../../.."}
	patterns := make([]string, 0, len(higherLayers))
	for _, layer := range slices.Sorted(maps.Keys(higherLayers)) {
		patterns = append(patterns, "./internal/"+layer+"/...")
	}
	loaded, err := packages.Load(config, patterns...)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) == 0 {
		t.Fatal("no packages loaded")
	}
	for _, violation := range higherLayerDependencies(loaded) {
		t.Error(violation)
	}
}

func TestHigherLayerDependenciesFollowTransitiveImports(t *testing.T) {
	t.Parallel()
	graph := map[string]*packages.Package{}
	node := func(path string, imports ...string) *packages.Package {
		pkg := &packages.Package{PkgPath: path, Imports: map[string]*packages.Package{}}
		for _, imported := range imports {
			pkg.Imports[imported] = graph[imported]
		}
		graph[path] = pkg
		return pkg
	}
	node(module + "checking/rules/security")
	node(module + "checking")
	node(module + "checkingtools")
	node(module + "plugins/artifacts")
	node(module+"plugins/manifests", module+"checking")
	node(module+"model/rules", module+"checkingtools")
	node(module+"input/configuration/schema", module+"plugins/manifests")
	node(module + "input/files")
	node(module+"inputs", module+"checking")
	loaded := []*packages.Package{
		node(module+"input/configuration", module+"model/rules", module+"checking/rules/security", module+"input/configuration/schema"),
		node(module+"input/workspace", module+"input/files", module+"plugins/artifacts"),
		node(module+"results", module+"checking"),
		graph[module+"model/rules"],
		graph[module+"plugins/manifests"],
		graph[module+"inputs"],
	}
	got := higherLayerDependencies(loaded)
	want := []string{
		module + "input/configuration depends on " + module + "checking/rules/security",
		module + "input/configuration depends on " + module + "plugins/manifests through " + module + "input/configuration/schema",
		module + "input/workspace depends on " + module + "plugins/artifacts",
		module + "results depends on " + module + "checking",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("violations = %q, want %q", got, want)
	}
}

// higherLayerDependencies reports each package of loaded in a lower layer
// that reaches a package of a higher layer through production imports. It
// names the first higher-layer package on each import path and the lower
// packages the path passes through, and stops following the path there.
func higherLayerDependencies(loaded []*packages.Package) []string {
	var violations []string
	for _, pkg := range loaded {
		for _, loadErr := range pkg.Errors {
			violations = append(violations, pkg.PkgPath+": "+loadErr.Msg)
		}
		forbidden := higherLayers[layerOf(pkg.PkgPath)]
		if len(forbidden) == 0 {
			continue
		}
		// via records the importer through which each package was first
		// reached, so a violation can name the path that leads to it.
		via := map[string]string{pkg.PkgPath: ""}
		queue := []*packages.Package{pkg}
		for len(queue) > 0 {
			current := queue[0]
			queue = queue[1:]
			for _, path := range slices.Sorted(maps.Keys(current.Imports)) {
				if _, seen := via[path]; seen {
					continue
				}
				via[path] = current.PkgPath
				if slices.Contains(forbidden, layerOf(path)) {
					violations = append(violations, describeDependency(pkg.PkgPath, path, via))
					continue
				}
				if imported := current.Imports[path]; imported != nil {
					queue = append(queue, imported)
				}
			}
		}
	}
	slices.Sort(violations)
	return violations
}

// describeDependency names how root reaches target, listing the packages
// between them in import order.
func describeDependency(root, target string, via map[string]string) string {
	var between []string
	for importer := via[target]; importer != root; importer = via[importer] {
		between = append(between, importer)
	}
	slices.Reverse(between)
	description := root + " depends on " + target
	if len(between) != 0 {
		description += " through " + strings.Join(between, ", ")
	}
	return description
}

// layerOf returns the first path element below internal/, or "" for a
// package outside this module's internal tree.
func layerOf(path string) string {
	relative, found := strings.CutPrefix(path, module)
	if !found {
		return ""
	}
	layer, _, _ := strings.Cut(relative, "/")
	return layer
}
