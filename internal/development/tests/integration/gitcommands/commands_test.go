// Package gitcommands checks that Go code starts Git through gitcommand.
package gitcommands

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"testing"
)

const root = "../../../../.."

// Git exports repository variables to hooks, and gitcommand.Command clears them.
// Without that clearing, `git init` in a temporary directory under a worktree
// hook reinitializes the hook's repository and makes it bare.
const allowed = "internal/integrations/gitcommand/command.go"

func TestGitStartsThroughGitcommand(t *testing.T) {
	t.Parallel()
	files := token.NewFileSet()
	var found []string
	for _, directory := range []string{"internal", "plugins", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, directory), func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				switch entry.Name() {
				case "node_modules", "vendor", "target":
					return filepath.SkipDir
				}
				return nil
			}
			name, err := filepath.Rel(root, path)
			if err != nil || filepath.Ext(path) != ".go" || filepath.ToSlash(name) == allowed {
				return err
			}
			file, err := parser.ParseFile(files, path, nil, parser.SkipObjectResolution)
			if err != nil {
				return err
			}
			for _, position := range gitCommands(file) {
				found = append(found, filepath.ToSlash(name)+":"+strconv.Itoa(files.Position(position).Line))
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, location := range found {
		t.Errorf("%s starts Git with os/exec; use gitcommand.Command", location)
	}
}

func gitCommands(file *ast.File) []token.Pos {
	git := map[string]bool{}
	ast.Inspect(file, func(node ast.Node) bool {
		assign, ok := node.(*ast.AssignStmt)
		if !ok || len(assign.Rhs) != 1 {
			return true
		}
		lookup := execCall(assign.Rhs[0], "LookPath")
		if ident, ok := assign.Lhs[0].(*ast.Ident); ok && lookup != nil && len(lookup.Args) == 1 && gitLiteral(lookup.Args[0]) {
			git[ident.Name] = true
		}
		return true
	})
	var found []token.Pos
	ast.Inspect(file, func(node ast.Node) bool {
		expr, ok := node.(ast.Expr)
		if !ok {
			return true
		}
		call, program := execCall(expr, "Command"), 0
		if call == nil {
			call, program = execCall(expr, "CommandContext"), 1
		}
		if call == nil || len(call.Args) <= program {
			return true
		}
		switch argument := call.Args[program].(type) {
		case *ast.BasicLit:
			if gitLiteral(argument) {
				found = append(found, call.Pos())
			}
		case *ast.Ident:
			if git[argument.Name] {
				found = append(found, call.Pos())
			}
		}
		return true
	})
	return found
}

func execCall(expr ast.Expr, function string) *ast.CallExpr {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return nil
	}
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != function {
		return nil
	}
	if pkg, ok := selector.X.(*ast.Ident); !ok || pkg.Name != "exec" {
		return nil
	}
	return call
}

func gitLiteral(expr ast.Expr) bool {
	literal, ok := expr.(*ast.BasicLit)
	if !ok || literal.Kind != token.STRING {
		return false
	}
	value, err := strconv.Unquote(literal.Value)
	return err == nil && value == "git"
}
