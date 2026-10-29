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
			if obj == nil {
				return true
			}
			named, ok := types.Unalias(obj.Type()).(*types.Named)
			if !ok {
				return true
			}
			// An exported alias publishes its target's fields. Exported targets in
			// this package are checked under their own name.
			if obj.IsAlias() && (named.Obj().Pkg() != pass.Pkg || named.Obj().Exported()) {
				return true
			}
			fields, ok := named.Underlying().(*types.Struct)
			if !ok {
				return true
			}
			stateful := owned[named.Origin()]
			for field := range fields.Fields() {
				stateful = stateful || !field.Exported()
			}
			if stateful || possiblyOwned[named.Origin()] {
				// Alias fields are reported at the alias that publishes them.
				at := token.NoPos
				if obj.IsAlias() {
					at = decl.Name.Pos()
				}
				for _, field := range writableFields(pass.Pkg, named, fields, at) {
					if stateful {
						pass.Reportf(field.at, "ARC002: state-owning type %s exposes writable field %s; keep representation private and expose value operations", obj.Name(), field.name)
					} else {
						pass.Reportf(field.at, "ARC004: type %s exposes writable field %s and passes receiver storage to a call without a mutation summary; read-only behavior is not established", obj.Name(), field.name)
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
			effect := apiReadOnly
			if storage(resultType) {
				effect = mutatingAPI(pass, resultType, flows, map[types.Type]bool{})
			}
			exposed := mutable(resultType) || effect == apiMutating
			if origins[0] && exposed {
				pass.Reportf(obj.Pos(), "ARC003: %s result %d may expose mutable storage reachable from its receiver; return a detached value or an opaque value handle", obj.Name(), index+1)
			} else if (origins[0] || origins[-1]) && (exposed || effect == apiUnresolved) {
				pass.Reportf(obj.Pos(), "ARC004: %s result %d may alias receiver storage through a call without a source summary; detachment is not established", obj.Name(), index+1)
			}
		}
	}
	checkAPI(pass)
	return nil, nil
}

type writableField struct {
	name string
	at   token.Pos
}

// writableFields lists the exported fields callers can assign through owner,
// including fields promoted from unexported embedded structs. A promoted
// field is reported at the embedding that publishes it; a non-zero at
// overrides every position.
func writableFields(pkg *types.Package, owner *types.Named, fields *types.Struct, at token.Pos) []writableField {
	var out []writableField
	var visit func(*types.Struct, token.Pos, map[*types.Struct]bool)
	visit = func(fields *types.Struct, embeddedAt token.Pos, seen map[*types.Struct]bool) {
		if seen[fields] {
			return
		}
		seen[fields] = true
		for field := range fields.Fields() {
			field := field
			at := embeddedAt
			if at == token.NoPos {
				at = field.Pos()
			}
			if field.Exported() {
				// Shallower or ambiguous selectors hide deeper fields.
				if selected, _, _ := types.LookupFieldOrMethod(owner, true, pkg, field.Name()); selected == field {
					out = append(out, writableField{field.Name(), at})
				}
				continue
			}
			if !field.Embedded() {
				continue
			}
			embedded := types.Unalias(field.Type())
			if pointer, ok := embedded.(*types.Pointer); ok {
				embedded = types.Unalias(pointer.Elem())
			}
			if nested, ok := embedded.Underlying().(*types.Struct); ok {
				visit(nested, at, seen)
			}
		}
	}
	visit(fields, at, map[*types.Struct]bool{})
	return out
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
			for tparam := range params.TypeParams() {
				walk(tparam.Constraint())
			}
		}
		walk = func(t types.Type) {
			if t == nil || seen[t] {
				return
			}
			seen[t] = true
			switch t := t.(type) {
			case *types.Alias:
				for t0 := range t.TypeArgs().Types() {
					walk(t0)
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
				for t0 := range t.TypeArgs().Types() {
					walk(t0)
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
				for method := range methods.Methods() {
					method := method.Obj()
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
				for field := range t.Fields() {
					if field.Exported() {
						walk(field.Type())
					}
				}
			case *types.Signature:
				walk(t.Params())
				walk(t.Results())
				walkConstraints(t.TypeParams())
			case *types.Tuple:
				for v := range t.Variables() {
					walk(v.Type())
				}
			case *types.Interface:
				for method := range t.Methods() {
					walk(method.Type())
				}
				for etyp := range t.EmbeddedTypes() {
					walk(etyp)
				}
			case *types.TypeParam:
				walk(t.Constraint())
			case *types.Union:
				for term := range t.Terms() {
					walk(term.Type())
				}
			}
		}
		walk(obj.Type())
	}
}
