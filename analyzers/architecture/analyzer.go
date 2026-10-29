// Package architecture checks representation ownership and public API boundaries
// from Go types and SSA. It contains no repository layout or protected-type list.
package architecture

import (
	"go/ast"
	"go/token"
	"go/types"
	"path/filepath"
	"runtime"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/buildssa"
)

var Analyzer = &analysis.Analyzer{
	Name:      "architecture",
	Doc:       "check public API boundaries, state encapsulation and borrowed mutable storage",
	Requires:  []*analysis.Analyzer{buildssa.Analyzer},
	FactTypes: []analysis.Fact{new(flowFact)},
	Run:       run,
}

func run(pass *analysis.Pass) (any, error) {
	// Drivers can schedule a types-only package without source files. There
	// is no body to summarize, and some drivers cannot attach facts to it.
	if len(pass.Files) == 0 {
		return nil, nil
	}
	if strings.HasPrefix(pass.Fset.Position(pass.Files[0].Pos()).Filename, filepath.Join(runtime.GOROOT(), "src")+string(filepath.Separator)) {
		return nil, nil
	}
	program := pass.ResultOf[buildssa.Analyzer].(*buildssa.SSA)
	flows := solve(pass, program.SrcFuncs)
	owned := map[*types.Named]bool{}
	possiblyOwned := map[*types.Named]bool{}
	for _, fn := range program.SrcFuncs {
		if fn.Signature.Recv() == nil || isTest(pass, fn.Pos()) {
			continue
		}
		named := namedType(fn.Signature.Recv().Type())
		if named != nil && flows[fn].Writes[0] {
			owned[named.Origin()] = true
		}
		if named != nil && flows[fn].Writes[-1] {
			possiblyOwned[named.Origin()] = true
		}
	}
	for _, file := range pass.Files {
		if strings.HasSuffix(pass.Fset.Position(file.Pos()).Filename, "_test.go") {
			continue
		}
		ast.Inspect(file, func(node ast.Node) bool {
			decl, ok := node.(*ast.TypeSpec)
			if !ok || !decl.Name.IsExported() {
				return true
			}
			obj, _ := pass.TypesInfo.Defs[decl.Name].(*types.TypeName)
			if obj == nil || obj.IsAlias() {
				return true
			}
			named, ok := obj.Type().(*types.Named)
			if !ok {
				return true
			}
			fields, ok := named.Underlying().(*types.Struct)
			if !ok {
				return true
			}
			stateful := owned[named.Origin()]
			for i := range fields.NumFields() {
				stateful = stateful || !fields.Field(i).Exported()
			}
			if stateful || possiblyOwned[named.Origin()] {
				for i := range fields.NumFields() {
					field := fields.Field(i)
					if field.Exported() {
						if stateful {
							pass.Reportf(field.Pos(), "ARC002: state-owning type %s exposes writable field %s; keep representation private and expose value operations", obj.Name(), field.Name())
						} else {
							pass.Reportf(field.Pos(), "ARC004: type %s exposes writable field %s and passes receiver storage to a call without a mutation summary; read-only behavior is not established", obj.Name(), field.Name())
						}
					}
				}
			}
			return true
		})
	}
	for _, fn := range program.SrcFuncs {
		obj, _ := fn.Object().(*types.Func)
		if obj == nil || !obj.Exported() || fn.Signature.Recv() == nil || isTest(pass, obj.Pos()) {
			continue
		}
		for index, origins := range flows[fn].Results {
			resultType := fn.Signature.Results().At(index).Type()
			exposed := mutable(resultType) || storage(resultType) && mutatingAPI(pass, resultType, flows, map[types.Type]bool{})
			if origins[0] && exposed {
				pass.Reportf(obj.Pos(), "ARC003: %s result %d may expose mutable storage reachable from its receiver; return a detached value or an opaque value handle", obj.Name(), index+1)
			} else if origins[-1] && exposed {
				pass.Reportf(obj.Pos(), "ARC004: %s result %d may alias receiver storage through a call without a source summary; detachment is not established", obj.Name(), index+1)
			}
		}
	}
	checkAPI(pass)
	return nil, nil
}

func isTest(pass *analysis.Pass, pos token.Pos) bool {
	return strings.HasSuffix(pass.Fset.Position(pos).Filename, "_test.go")
}

func namedType(t types.Type) *types.Named {
	t = types.Unalias(t)
	if p, ok := t.(*types.Pointer); ok {
		t = types.Unalias(p.Elem())
	}
	named, _ := t.(*types.Named)
	return named
}

func internalPackage(path string) bool {
	return path == "internal" || strings.HasPrefix(path, "internal/") || strings.Contains(path, "/internal/") || strings.HasSuffix(path, "/internal")
}

// checkAPI walks exported signatures, fields and constraints. Imports and
// private fields may use internal types.
func checkAPI(pass *analysis.Pass) {
	if internalPackage(pass.Pkg.Path()) || pass.Pkg.Name() == "main" {
		return
	}
	for _, name := range pass.Pkg.Scope().Names() {
		obj := pass.Pkg.Scope().Lookup(name)
		if !obj.Exported() || isTest(pass, obj.Pos()) {
			continue
		}
		seen := map[types.Type]bool{}
		reported := map[*types.TypeName]bool{}
		var walk func(types.Type)
		walkConstraints := func(params *types.TypeParamList) {
			for i := range params.Len() {
				walk(params.At(i).Constraint())
			}
		}
		walk = func(t types.Type) {
			if t == nil || seen[t] {
				return
			}
			seen[t] = true
			switch t := t.(type) {
			case *types.Alias:
				for i := 0; i < t.TypeArgs().Len(); i++ {
					walk(t.TypeArgs().At(i))
				}
				if t.Obj().Pkg() != pass.Pkg && t.Obj().Exported() && !internalPackage(t.Obj().Pkg().Path()) {
					return
				}
				walkConstraints(t.TypeParams())
				walk(types.Unalias(t))
			case *types.Named:
				decl := t.Obj()
				if decl.Pkg() != nil && internalPackage(decl.Pkg().Path()) && !reported[decl] {
					reported[decl] = true
					pass.Reportf(obj.Pos(), "ARC001: public API %s exposes internal type %s.%s; define the public value contract independently", obj.Name(), decl.Pkg().Path(), decl.Name())
					return
				}
				for i := 0; i < t.TypeArgs().Len(); i++ {
					walk(t.TypeArgs().At(i))
				}
				// Another package's public contract is reported when that
				// package is analyzed.
				if decl.Pkg() != pass.Pkg {
					return
				}
				walkConstraints(t.TypeParams())
				walk(t.Underlying())
				// The pointer method set includes methods promoted from unexported
				// embedded types. Promoted dependency methods belong to their owner.
				methods := types.NewMethodSet(types.NewPointer(t))
				for i := range methods.Len() {
					method := methods.At(i).Obj()
					if method.Exported() && method.Pkg() == pass.Pkg {
						walk(method.Type())
					}
				}
			case *types.Pointer:
				walk(t.Elem())
			case *types.Slice:
				walk(t.Elem())
			case *types.Array:
				walk(t.Elem())
			case *types.Map:
				walk(t.Key())
				walk(t.Elem())
			case *types.Chan:
				walk(t.Elem())
			case *types.Struct:
				for i := range t.NumFields() {
					if t.Field(i).Exported() {
						walk(t.Field(i).Type())
					}
				}
			case *types.Signature:
				walk(t.Params())
				walk(t.Results())
				walkConstraints(t.TypeParams())
			case *types.Tuple:
				for i := range t.Len() {
					walk(t.At(i).Type())
				}
			case *types.Interface:
				for i := range t.NumMethods() {
					walk(t.Method(i).Type())
				}
				for i := range t.NumEmbeddeds() {
					walk(t.EmbeddedType(i))
				}
			case *types.TypeParam:
				walk(t.Constraint())
			case *types.Union:
				for i := range t.Len() {
					walk(t.Term(i).Type())
				}
			}
		}
		walk(obj.Type())
	}
}
