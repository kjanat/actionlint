// Package layering checks the import direction between internal layers.
package layering

import (
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

const module = "actionlint.kjanat.dev/v2/internal/"

// lowerLayers describe documents, read inputs and publish results. Rule
// implementations and the executor in internal/checking build on them, so
// production code in these layers must not import internal/checking.
var lowerLayers = []string{"model", "input", "results"}

// TestLowerLayersDoNotImportChecking keeps rule identity and other shared
// declarations below the checks that use them. Tests may still import
// internal/checking to exercise a lower package through the engine.
func TestLowerLayersDoNotImportChecking(t *testing.T) {
	if testing.Short() {
		t.Skip("lists every internal package")
	}
	t.Parallel()
	config := &packages.Config{Mode: packages.NeedName | packages.NeedImports, Dir: "../../../../.."}
	patterns := make([]string, 0, len(lowerLayers))
	for _, layer := range lowerLayers {
		patterns = append(patterns, "./internal/"+layer+"/...")
	}
	loaded, err := packages.Load(config, patterns...)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) == 0 {
		t.Fatal("no packages loaded")
	}
	for _, violation := range checkingImports(loaded) {
		t.Error(violation)
	}
}

func TestCheckingImportsReportsOnlyLowerLayers(t *testing.T) {
	t.Parallel()
	imports := func(paths ...string) map[string]*packages.Package {
		result := make(map[string]*packages.Package, len(paths))
		for _, path := range paths {
			result[path] = &packages.Package{PkgPath: path}
		}
		return result
	}
	loaded := []*packages.Package{
		{PkgPath: module + "input/configuration", Imports: imports(module+"model/rules", module+"checking/rules/security")},
		{PkgPath: module + "results", Imports: imports(module + "checking")},
		{PkgPath: module + "model/rules", Imports: imports(module + "checkingtools")},
		{PkgPath: module + "plugins/manifests", Imports: imports(module + "checking/inputs")},
		{PkgPath: module + "inputs", Imports: imports(module + "checking")},
	}
	got := checkingImports(loaded)
	want := []string{
		module + "input/configuration imports " + module + "checking/rules/security",
		module + "results imports " + module + "checking",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("violations = %q, want %q", got, want)
	}
}

func checkingImports(loaded []*packages.Package) []string {
	var violations []string
	for _, pkg := range loaded {
		for _, loadErr := range pkg.Errors {
			violations = append(violations, pkg.PkgPath+": "+loadErr.Msg)
		}
		if !inLowerLayer(pkg.PkgPath) {
			continue
		}
		for imported := range pkg.Imports {
			if imported == module+"checking" || strings.HasPrefix(imported, module+"checking/") {
				violations = append(violations, pkg.PkgPath+" imports "+imported)
			}
		}
	}
	slices.Sort(violations)
	return violations
}

func inLowerLayer(path string) bool {
	relative, found := strings.CutPrefix(path, module)
	if !found {
		return false
	}
	layer, _, _ := strings.Cut(relative, "/")
	return slices.Contains(lowerLayers, layer)
}
