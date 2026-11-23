package registration

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func TestAllHoldsValidUniquelyNamedRules(t *testing.T) {
	t.Parallel()
	seen := map[string]bool{}
	for i, native := range All {
		if native == nil {
			t.Fatalf("All[%d] is nil", i)
		}
		if err := native.Validate(); err != nil {
			t.Error(err)
		}
		if seen[native.Name] {
			t.Errorf("rule %q is listed more than once", native.Name)
		}
		seen[native.Name] = true
	}
}

func TestAllListsEveryDefinedRule(t *testing.T) {
	t.Parallel()
	defined := map[string]int{}
	root := filepath.Join("..", "..")
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == "define" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(node ast.Node) bool {
			literal, ok := node.(*ast.CompositeLit)
			if !ok {
				return true
			}
			selector, ok := literal.Type.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != "Rule" {
				return true
			}
			if pkg, ok := selector.X.(*ast.Ident); !ok || pkg.Name != "define" {
				return true
			}
			for _, element := range literal.Elts {
				pair, ok := element.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				if key, ok := pair.Key.(*ast.Ident); ok && key.Name == "Name" {
					if value, ok := pair.Value.(*ast.BasicLit); ok {
						name, err := strconv.Unquote(value.Value)
						if err != nil {
							t.Fatal(err)
						}
						defined[name]++
					}
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	listed := map[string]int{}
	for _, native := range All {
		listed[native.Name]++
	}
	if !maps.Equal(defined, listed) {
		t.Fatalf("rules defined in source and rules in All differ:\n defined: %v\n listed:  %v", slices.Sorted(maps.Keys(defined)), slices.Sorted(maps.Keys(listed)))
	}
	if len(defined) == 0 {
		t.Fatal("found no rule definitions; the source scan is broken")
	}
}
