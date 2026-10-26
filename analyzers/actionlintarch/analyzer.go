// Package actionlintarch checks actionlint's dependency and value ownership rules.
package actionlintarch

import (
	"go/ast"
	"go/token"
	"go/types"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/tools/go/analysis"
)

var Analyzer = &analysis.Analyzer{Name: "actionlintarch", Doc: "enforce package, semantic, identity and cancellation ownership", Run: run, FactTypes: []analysis.Fact{new(dependencyFact)}}

func report(pass *analysis.Pass, pos token.Pos, code, message string) {
	pass.Report(analysis.Diagnostic{Pos: pos, Category: code, Message: code + ": " + message})
}

type dependencyPath struct {
	Target string
	Chain  []string
}
type dependencyFact struct{ Paths []dependencyPath }

func (*dependencyFact) AFact()         {}
func (*dependencyFact) String() string { return "project dependency paths" }

// Dependency facts record the imports written in each source file.
func dependencies(pass *analysis.Pass) {
	paths := map[string][]string{}
	for _, file := range pass.Files {
		if strings.HasSuffix(pass.Fset.Position(file.Pos()).Filename, "_test.go") {
			continue
		}
		for _, spec := range file.Imports {
			path, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				continue
			}
			paths[path] = []string{path}
			if !within(path, module) {
				continue
			}
			for _, pkg := range pass.Pkg.Imports() {
				if pkg.Path() == path {
					var fact dependencyFact
					if pass.ImportPackageFact(pkg, &fact) {
						for _, entry := range fact.Paths {
							target, chain := entry.Target, entry.Chain
							candidate := append([]string{path}, chain...)
							if old, ok := paths[target]; !ok || strings.Join(candidate, " -> ") < strings.Join(old, " -> ") {
								paths[target] = candidate
							}
						}
					}
				}
			}
		}
	}
	if len(paths) > 0 {
		keys := make([]string, 0, len(paths))
		for key := range paths {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		entries := make([]dependencyPath, 0, len(keys))
		for _, key := range keys {
			entries = append(entries, dependencyPath{Target: key, Chain: paths[key]})
		}
		pass.ExportPackageFact(&dependencyFact{Paths: entries})
	}
}

func run(pass *analysis.Pass) (any, error) {
	// The go tool synthesizes a main package for each test binary.
	if pass.Pkg.Name() == "main" && strings.HasSuffix(pass.Pkg.Path(), ".test") {
		return nil, nil
	}
	path := strings.TrimSuffix(pass.Pkg.Path(), "_test")
	if !within(path, module) || within(path, module+"/analyzers") {
		return nil, nil
	}
	dependencies(pass)
	for _, file := range pass.Files {
		// Behavioral fixtures may deliberately construct invalid values. Test imports
		// still participate in package boundary checks below.
		test := strings.HasSuffix(pass.Fset.Position(file.Pos()).Filename, "_test.go")
		checkImports(pass, file, path, test)
		if test {
			continue
		}
		ast.Inspect(file, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.CompositeLit:
				if len(n.Elts) > 0 {
					checkConstruction(pass, path, n, n)
				}
			case *ast.Ident:
				// Check references, including functions assigned to variables.
				if fn, ok := pass.TypesInfo.Uses[n].(*types.Func); ok && fn.Pkg() != nil && fn.Pkg().Path() == "context" && (fn.Name() == "Background" || fn.Name() == "TODO") && !contextBoundary(path) {
					report(pass, n.Pos(), "ALA105", "context."+fn.Name()+" discards caller cancellation; receive and propagate the analysis context")
				}
			case *ast.AssignStmt:
				for _, lhs := range n.Lhs {
					checkMutation(pass, path, lhs)
				}
			case *ast.IncDecStmt:
				checkMutation(pass, path, n.X)
			case *ast.TypeSpec:
				if path == internal("engine") {
					switch n.Name.Name {
					case "Linter", "LinterOptions", "AnalysisSession", "AnalysisOptions", "AnalysisRequest":
						report(pass, n.Pos(), "ALA104", "retired engine facade "+n.Name.Name+" must not be recreated")
					}
				}
				if path == internal("tools/execution") && n.Name.Name == "Scope" {
					if s, ok := n.Type.(*ast.StructType); ok {
						for _, f := range s.Fields.List {
							for _, name := range f.Names {
								switch name.Name {
								case "Document", "Caller", "Call", "Depth":
									report(pass, name.Pos(), "ALA104", "Scope."+name.Name+" duplicates invocation ownership; consume the canonical invocation model")
								}
							}
						}
					}
				}
			case *ast.FuncDecl:
				if path == internal("engine") && n.Recv == nil && n.Name.Name == "NewLinter" {
					report(pass, n.Pos(), "ALA104", "retired engine.NewLinter facade must not be recreated")
				}
			}
			return true
		})
	}
	return nil, nil
}

func checkMutation(pass *analysis.Pass, path string, expr ast.Expr) {
	for {
		parens, ok := expr.(*ast.ParenExpr)
		if !ok {
			break
		}
		expr = parens.X
	}
	if field, ok := expr.(*ast.SelectorExpr); ok {
		checkConstruction(pass, path, field, field.X)
	}
}

func checkConstruction(pass *analysis.Pass, path string, n ast.Node, e ast.Expr) {
	t := pass.TypesInfo.TypeOf(e)
	if t == nil {
		return
	}
	t = types.Unalias(t)
	if p, ok := t.(*types.Pointer); ok {
		t = types.Unalias(p.Elem())
	}
	named, ok := t.(*types.Named)
	if !ok || named.Obj().Pkg() == nil {
		return
	}
	key := named.Obj().Pkg().Path() + "." + named.Obj().Name()
	owners, restricted := constructors[key]
	if !restricted {
		return
	}
	for _, owner := range owners {
		if path == owner {
			return
		}
	}
	report(pass, n.Pos(), "ALA103", "construction or mutation of "+key+" outside its owner; consume the canonical value (owners: "+strings.Join(owners, ", ")+")")
}

func checkImports(pass *analysis.Pass, file *ast.File, path string, test bool) {
	for _, spec := range file.Imports {
		imported, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		var pkg *types.Package
		for _, candidate := range pass.Pkg.Imports() {
			if candidate.Path() == imported {
				pkg = candidate
				break
			}
		}
		if pkg == nil {
			continue
		}
		chains := map[string][]string{imported: {imported}}
		if within(imported, module) {
			var fact dependencyFact
			if pass.ImportPackageFact(pkg, &fact) {
				for _, entry := range fact.Paths {
					target, chain := entry.Target, entry.Chain
					chains[target] = append([]string{imported}, chain...)
				}
			}
		}
		targets := make([]string, 0, len(chains))
		for target := range chains {
			targets = append(targets, target)
		}
		sort.Strings(targets)
		for _, target := range targets {
			chain := chains[target]
			domains := make([]string, 0, len(dependencyBans))
			for domain := range dependencyBans {
				domains = append(domains, domain)
			}
			sort.Strings(domains)
			for _, domain := range domains {
				if within(path, internal(domain)) {
					for _, forbidden := range dependencyBans[domain] {
						if within(target, internal(forbidden)) {
							report(pass, spec.Pos(), "ALA101", "forbidden dependency: "+strings.Join(append([]string{path}, chain...), " -> "))
						}
					}
				}
			}
			if !test && pure(path) && forbiddenEffect(target) {
				report(pass, spec.Pos(), "ALA102", "host/parser dependency in portable domain: "+strings.Join(append([]string{path}, chain...), " -> "))
			}
		}
	}
}
