package architecture_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Migration scaffolding must not become a second implementation of the host API.
func TestEngineHasNoMigrationFacades(t *testing.T) {
	files, err := os.ReadDir(filepath.Join("..", "engine"))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		name := file.Name()
		if file.IsDir() || !strings.HasSuffix(name, ".go") {
			continue
		}
		if strings.HasPrefix(name, "legacy_") {
			t.Errorf("migration implementation remains in engine: %s", name)
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), filepath.Join("..", "engine", name), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, declaration := range parsed.Decls {
			if function, ok := declaration.(*ast.FuncDecl); ok && function.Recv == nil && function.Name.Name == "NewLinter" {
				t.Errorf("%s recreates the removed Linter facade", name)
			}
			if declaration, ok := declaration.(*ast.GenDecl); ok {
				for _, specification := range declaration.Specs {
					if typ, ok := specification.(*ast.TypeSpec); ok {
						switch typ.Name.Name {
						case "Linter", "LinterOptions", "AnalysisSession", "AnalysisOptions", "AnalysisRequest":
							t.Errorf("%s recreates migration type %s", name, typ.Name.Name)
						}
					}
				}
			}
		}
	}
}
