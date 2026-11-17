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

// lowerLayers orders the layers below internal/externallinters from lowest to
// highest. Each may depend only on the layers before it:
//
//   - internal/base holds source text, byte captures, filesystem reads and
//     digests, which every other layer shares.
//   - internal/languages parses and evaluates the expression and YAML
//     languages that documents are written in.
//   - internal/model describes workflows, actions, rules and tool manifests
//     in those languages.
//   - internal/integrations reaches external services: HTTP and Git access,
//     network policy and accounting, host processes and the dependency cache.
//   - internal/input reads configuration, local files and remote metadata
//     into the model, through those integrations.
//   - internal/results defines findings, the analyzers that produce them and
//     the policies and reports that publish them.
var lowerLayers = []string{"base", "languages", "model", "integrations", "input", "results"}

// upperLayers are the layers above lowerLayers. internal/externallinters
// prepares scripts for external linters and runs them as host processes;
// built-in tools and tool provider plugins share it. Plugin hosting in
// internal/plugins and rule execution in internal/checking build on those
// layers and never on each other. internal/assembly loads the selected plugins
// and wires them into checking. Rendering in internal/output and the entry
// points in internal/cli and internal/lsp sit on top.
var upperLayers = []string{"externallinters", "checking", "plugins", "assembly", "output", "cli", "lsp"}

// higherLayers lists, for each layer, the layers its production code must not
// depend on, directly or through any other package.
var higherLayers = func() map[string][]string {
	layers := map[string][]string{
		"externallinters": {"checking", "plugins", "assembly", "output", "cli", "lsp"},
		"plugins":         {"checking", "assembly", "output", "cli", "lsp"},
		"checking":        {"plugins", "assembly", "output", "cli", "lsp"},
		"assembly":        {"output", "cli", "lsp"},
		"output":          {"externallinters", "checking", "plugins", "assembly", "cli", "lsp"},
	}
	for i, layer := range lowerLayers {
		layers[layer] = slices.Concat(lowerLayers[i+1:], upperLayers)
	}
	return layers
}()

// TestLowerLayersDoNotDependOnHigherLayers keeps rule identity, tool
// settings, plugin references, the checker contract and other shared
// declarations below the checks and plugin hosts that use them, and keeps
// plugin hosting and rule execution independent of each other. Tests may
// still import higher layers to exercise a lower package through the engine,
// so only production imports are followed.
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
	node(module + "output")
	node(module + "plugins/artifacts")
	node(module+"plugins/loader", module+"checking")
	node(module+"model/rule", module+"checkingtools")
	node(module+"input/configuration/schema", module+"plugins/loader")
	node(module + "input/files")
	node(module+"inputs", module+"checking")
	node(module+"languages/messages", module+"output")
	node(module+"checking/scripts", module+"plugins/artifacts")
	node(module + "base/files")
	node(module + "input/references")
	node(module+"languages/yaml", module+"base/files")
	node(module+"model/parsing", module+"languages/yaml", module+"input/references")
	node(module+"base/sourcetext", module+"model/parsing")
	node(module+"integrations/github", module+"base/files", module+"input/references")
	loaded := []*packages.Package{
		graph[module+"base/sourcetext"],
		graph[module+"languages/yaml"],
		graph[module+"model/parsing"],
		graph[module+"integrations/github"],
		node(module+"input/configuration", module+"model/rule", module+"checking/rules/security", module+"input/configuration/schema"),
		node(module+"input/workspace", module+"input/files", module+"plugins/artifacts"),
		node(module+"results", module+"checking"),
		graph[module+"model/rule"],
		graph[module+"plugins/loader"],
		graph[module+"inputs"],
		graph[module+"languages/messages"],
		graph[module+"checking/scripts"],
	}
	got := higherLayerDependencies(loaded)
	want := []string{
		module + "base/sourcetext depends on " + module + "model/parsing",
		module + "checking/scripts depends on " + module + "plugins/artifacts",
		module + "input/configuration depends on " + module + "checking/rules/security",
		module + "input/configuration depends on " + module + "plugins/loader through " + module + "input/configuration/schema",
		module + "input/workspace depends on " + module + "plugins/artifacts",
		module + "integrations/github depends on " + module + "input/references",
		module + "languages/messages depends on " + module + "output",
		module + "model/parsing depends on " + module + "input/references",
		module + "plugins/loader depends on " + module + "checking",
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
