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

// treeWriters may modify decoded syntax trees: they build them before publication.
var treeWriters = []string{
	definitionPackage,
	"actionlint.kjanat.dev/v2/internal/model/workflows/parser",
}

// TestProductionCannotModifyPublishedSyntax replaces the read-only views that
// consumers once received. Only tree builders may assign to fields of
// definition types or write into their slices and maps.
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
	for _, pkg := range loaded {
		if slices.Contains(treeWriters, pkg.PkgPath) {
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
					if field, ok := definitionField(pkg.TypesInfo, target); ok {
						t.Errorf("%s writes published syntax field %s", pkg.Fset.Position(target.Pos()), field)
					}
				}
				return true
			})
		}
	}
}

// definitionField reports a write target that is a field of a definition type,
// or an element of a slice or map held in such a field.
func definitionField(info *types.Info, target ast.Expr) (string, bool) {
	for {
		switch expression := target.(type) {
		case *ast.ParenExpr:
			target = expression.X
		case *ast.IndexExpr:
			target = expression.X
		case *ast.StarExpr:
			target = expression.X
		case *ast.SelectorExpr:
			selection, ok := info.Selections[expression]
			if !ok || selection.Kind() != types.FieldVal {
				return "", false
			}
			field, ok := selection.Obj().(*types.Var)
			if !ok || field.Pkg() == nil || field.Pkg().Path() != definitionPackage {
				return "", false
			}
			return strings.TrimPrefix(types.TypeString(selection.Recv(), (*types.Package).Name), "*") + "." + field.Name(), true
		default:
			return "", false
		}
	}
}
