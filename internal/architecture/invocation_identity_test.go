package architecture_test

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestOnlyInvocationOwnerCreatesOccurrenceIDs(t *testing.T) {
	const owner = "actionlint.kjanat.dev/v2/internal/workflow/invocation"
	output, err := exec.CommandContext(t.Context(), "go", "list", "-json", "../../...").Output()
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(strings.NewReader(string(output)))
	for decoder.More() {
		var pkg struct {
			Dir, ImportPath   string
			GoFiles, CgoFiles []string
		}
		if err := decoder.Decode(&pkg); err != nil {
			t.Fatal(err)
		}
		if pkg.ImportPath == owner {
			continue
		}
		for _, name := range append(pkg.GoFiles, pkg.CgoFiles...) {
			positions := token.NewFileSet()
			file, err := parser.ParseFile(positions, filepath.Join(pkg.Dir, name), nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			aliases := map[string]bool{}
			for _, imported := range file.Imports {
				path, err := strconv.Unquote(imported.Path.Value)
				if err != nil {
					t.Fatal(err)
				}
				if path != owner {
					continue
				}
				alias := "invocation"
				if imported.Name != nil {
					alias = imported.Name.Name
				}
				if alias == "." {
					t.Errorf("%s dot-imports the invocation owner", positions.Position(imported.Pos()))
				}
				aliases[alias] = true
			}
			ast.Inspect(file, func(node ast.Node) bool {
				selector, ok := node.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				base, ok := selector.X.(*ast.Ident)
				if !ok || !aliases[base.Name] {
					return true
				}
				switch selector.Sel.Name {
				case "NewRootID", "NewCallID", "NewScriptID":
					t.Errorf("%s bypasses occurrence registration through %s", positions.Position(selector.Pos()), selector.Sel.Name)
				}
				return true
			})
		}
	}
}
