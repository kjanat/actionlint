package workflow

import (
	"go/ast"
	"go/types"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

const definitionPackage = "actionlint.kjanat.dev/v2/internal/model/workflows/definition"

// treeBuilders may modify decoded syntax trees: they build them before
// declaration.Publish takes ownership. internal/model/parsing builds the step
// projection of composite action metadata.
var treeBuilders = []string{
	definitionPackage,
	"actionlint.kjanat.dev/v2/internal/model/workflows/parser",
	"actionlint.kjanat.dev/v2/internal/model/parsing",
}

// TestProductionCannotModifyPublishedSyntax replaces the read-only views that
// consumers once received. Outside the tree builders, production code may not
// assign to fields of definition types reached through a pointer, slice or map,
// nor write into or delete from their slices and maps. Writes to a local copy
// of a definition value stay allowed.
func TestProductionCannotModifyPublishedSyntax(t *testing.T) {
	if testing.Short() {
		t.Skip("type-checks the whole module")
	}
	t.Parallel()
	config := &packages.Config{Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo, Dir: "../../../.."}
	loaded, err := packages.Load(config, "./internal/...", "./cmd/...")
	if err != nil {
		t.Fatal(err)
	}
	for _, write := range publishedSyntaxWrites(loaded) {
		t.Error(write)
	}
}

func TestPublishedSyntaxWritesFixture(t *testing.T) {
	if testing.Short() {
		t.Skip("type-checks the definition package")
	}
	t.Parallel()
	config := &packages.Config{Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo}
	loaded, err := packages.Load(config, "./testdata/immutability")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, write := range publishedSyntaxWrites(loaded) {
		got = append(got, write[strings.LastIndex(write, "writes"):])
	}
	want := []string{
		"writes published syntax field workflow.Step.Name",
		"writes published syntax field workflow.String.Value",
		"writes published syntax field workflow.Job.Steps",
		"writes published syntax field workflow.Workflow.Jobs",
		"writes published syntax field workflow.Workflow.Jobs",
		"writes published syntax field workflow.Step.Pos",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("writes = %q, want %q", got, want)
	}
}

func publishedSyntaxWrites(loaded []*packages.Package) []string {
	var writes []string
	for _, pkg := range loaded {
		if slices.Contains(treeBuilders, pkg.PkgPath) {
			continue
		}
		for _, file := range pkg.Syntax {
			ast.Inspect(file, func(node ast.Node) bool {
				var targets []ast.Expr
				switch statement := node.(type) {
				case *ast.AssignStmt:
					targets = statement.Lhs
				case *ast.IncDecStmt:
					targets = []ast.Expr{statement.X}
				case *ast.CallExpr:
					if builtin, ok := statement.Fun.(*ast.Ident); ok && (builtin.Name == "delete" || builtin.Name == "clear") && len(statement.Args) > 0 {
						targets = statement.Args[:1]
					}
				}
				for _, target := range targets {
					if field, ok := sharedDefinitionField(pkg.TypesInfo, target); ok {
						writes = append(writes, pkg.Fset.Position(target.Pos()).String()+" writes published syntax field "+field)
					}
				}
				return true
			})
		}
	}
	return writes
}

// sharedDefinitionField reports a write target that is a field of a definition
// type, or an element of a slice or map held in one, reached through a pointer,
// slice or map. A chain of value selections from a local variable is a copy.
func sharedDefinitionField(info *types.Info, target ast.Expr) (string, bool) {
	field, shared := "", false
	for {
		switch expression := target.(type) {
		case *ast.ParenExpr:
			target = expression.X
		case *ast.IndexExpr:
			shared = true
			target = expression.X
		case *ast.StarExpr:
			shared = true
			target = expression.X
		case *ast.SelectorExpr:
			selection, ok := info.Selections[expression]
			if !ok || selection.Kind() != types.FieldVal {
				return "", false
			}
			variable, ok := selection.Obj().(*types.Var)
			if ok && field == "" && variable.Pkg() != nil && variable.Pkg().Path() == definitionPackage {
				field = strings.TrimPrefix(types.TypeString(selection.Recv(), (*types.Package).Name), "*") + "." + variable.Name()
			}
			if selection.Indirect() {
				shared = true
			}
			if _, pointer := info.TypeOf(expression.X).Underlying().(*types.Pointer); pointer {
				shared = true
			}
			target = expression.X
		default:
			return field, field != "" && shared
		}
	}
}
