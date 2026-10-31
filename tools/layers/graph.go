package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// targets are the platforms whose build files make up the graph. A file or
// import that exists on any of them is part of the table.
var targets = []struct{ goos, goarch string }{
	{"linux", "amd64"}, {"windows", "amd64"}, {"darwin", "arm64"}, {"js", "wasm"},
}

// modulePackage is a package of the main module with its files and imports
// merged across targets.
type modulePackage struct {
	path        string
	rel         string // directory relative to the module root, "" for the root
	dir         string // absolute directory with forward slashes
	files       []string
	testFiles   []string
	imports     []string
	testImports []string
	layer       *layer
}

// testOnly reports whether the package has no non-test files.
func (p *modulePackage) testOnly() bool {
	return len(p.files) == 0
}

type graph struct {
	module   string
	root     string
	goroot   string
	packages map[string]*modulePackage
	// deps holds the non-test imports of every listed package, including
	// standard and external packages, for transitive checks.
	deps     map[string][]string
	standard map[string]bool
	moduleOf map[string]string
}

// importPath turns a module-relative path from the spec into an import path.
func (g *graph) importPath(rel string) string {
	if rel == "." || rel == "" {
		return g.module
	}
	return g.module + "/" + rel
}

func (g *graph) sortedPackages() []*modulePackage {
	packages := make([]*modulePackage, 0, len(g.packages))
	for _, pkg := range g.packages {
		packages = append(packages, pkg)
	}
	slices.SortFunc(packages, func(a, b *modulePackage) int { return strings.Compare(a.path, b.path) })
	return packages
}

func (g *graph) isModule(importPath string) bool {
	return importPath == g.module || strings.HasPrefix(importPath, g.module+"/")
}

type listedPackage struct {
	ImportPath   string
	Dir          string
	Standard     bool
	ForTest      string
	Module       *struct{ Path string }
	GoFiles      []string
	CgoFiles     []string
	TestGoFiles  []string
	XTestGoFiles []string
	Imports      []string
	TestImports  []string
	XTestImports []string
	Error        *struct{ Err string }
}

const listFields = "ImportPath,Dir,Standard,ForTest,Module,GoFiles,CgoFiles,TestGoFiles,XTestGoFiles,Imports,TestImports,XTestImports,Error"

func goOutput(ctx context.Context, env []string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, "go", args...)
	command.Env = env
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("go %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return output, nil
}

func targetEnvironment(goos, goarch string) []string {
	var env []string
	for _, variable := range os.Environ() {
		key, _, _ := strings.Cut(variable, "=")
		if !strings.EqualFold(key, "GOOS") && !strings.EqualFold(key, "GOARCH") {
			env = append(env, variable)
		}
	}
	return append(env, "GOOS="+goos, "GOARCH="+goarch)
}

// loadGraph lists the module and its dependencies, test dependencies
// included, once per target.
func loadGraph(ctx context.Context) (*graph, error) {
	moduleInfo, err := goOutput(ctx, os.Environ(), "list", "-m", "-f", "{{.Path}}\n{{.Dir}}")
	if err != nil {
		return nil, err
	}
	module, root, _ := strings.Cut(strings.TrimSpace(string(moduleInfo)), "\n")
	goroot, err := goOutput(ctx, os.Environ(), "env", "GOROOT")
	if err != nil {
		return nil, err
	}
	g := &graph{
		module: module, root: filepath.ToSlash(strings.TrimSpace(root)), goroot: strings.TrimSpace(string(goroot)),
		packages: map[string]*modulePackage{}, deps: map[string][]string{},
		standard: map[string]bool{}, moduleOf: map[string]string{},
	}
	var errs []error
	for _, target := range targets {
		output, err := goOutput(ctx, targetEnvironment(target.goos, target.goarch), "list", "-e", "-deps", "-test", "-json="+listFields, "./...")
		if err != nil {
			return nil, err
		}
		decoder := json.NewDecoder(bytes.NewReader(output))
		for {
			var listed listedPackage
			if err := decoder.Decode(&listed); errors.Is(err, io.EOF) {
				break
			} else if err != nil {
				return nil, fmt.Errorf("decode go list output: %w", err)
			}
			// Skip test variants and generated test mains; the plain package
			// carries the test imports.
			if listed.ForTest != "" || strings.HasSuffix(listed.ImportPath, ".test") {
				continue
			}
			g.deps[listed.ImportPath] = mergeSorted(g.deps[listed.ImportPath], listed.Imports)
			switch {
			case listed.Standard:
				g.standard[listed.ImportPath] = true
				continue
			case listed.Module != nil && listed.Module.Path != module:
				g.moduleOf[listed.ImportPath] = listed.Module.Path
				continue
			case listed.Module == nil:
				errs = append(errs, fmt.Errorf("%s/%s: package %s has no module", target.goos, target.goarch, listed.ImportPath))
				continue
			}
			if listed.Error != nil {
				errs = append(errs, fmt.Errorf("%s/%s: %s: %s", target.goos, target.goarch, listed.ImportPath, listed.Error.Err))
				continue
			}
			g.addModulePackage(listed)
		}
	}
	return g, errors.Join(errs...)
}

func (g *graph) addModulePackage(listed listedPackage) {
	pkg := g.packages[listed.ImportPath]
	if pkg == nil {
		dir := filepath.ToSlash(listed.Dir)
		rel := strings.TrimPrefix(strings.TrimPrefix(dir, g.root), "/")
		pkg = &modulePackage{path: listed.ImportPath, rel: rel, dir: dir}
		g.packages[listed.ImportPath] = pkg
	}
	pkg.files = mergeSorted(pkg.files, listed.GoFiles, listed.CgoFiles)
	pkg.testFiles = mergeSorted(pkg.testFiles, listed.TestGoFiles, listed.XTestGoFiles)
	pkg.imports = mergeSorted(pkg.imports, listed.Imports)
	var testImports []string
	for _, imported := range slices.Concat(listed.TestImports, listed.XTestImports) {
		if imported != listed.ImportPath {
			testImports = append(testImports, imported)
		}
	}
	pkg.testImports = mergeSorted(pkg.testImports, testImports)
}

func mergeSorted(base []string, more ...[]string) []string {
	merged := slices.Concat(append([][]string{base}, more...)...)
	slices.Sort(merged)
	return slices.Compact(merged)
}

// external returns the module that provides a non-module, non-standard import.
func (g *graph) external(importPath string) (string, bool) {
	module, ok := g.moduleOf[importPath]
	return module, ok
}

// filePath returns the absolute slash path depguard sees for a file.
func (p *modulePackage) filePath(name string) string {
	return path.Join(p.dir, name)
}
