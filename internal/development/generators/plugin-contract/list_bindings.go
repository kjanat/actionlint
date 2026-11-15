package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"strings"

	"golang.org/x/tools/go/ast/astutil"
)

// shareListBindings retains each generated element codec and shares list storage.
func shareListBindings(data []byte) ([]byte, error) {
	set := token.NewFileSet()
	file, err := parser.ParseFile(set, "bindings.go", data, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	changed := false
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Body == nil {
			continue
		}
		name := function.Name.Name
		reading := strings.HasPrefix(name, "toGo") || strings.HasPrefix(name, "lift")
		writing := strings.HasPrefix(name, "fromGo") || strings.HasPrefix(name, "lower")
		if !strings.Contains(name, "List") || (!reading && !writing) {
			continue
		}
		var loop *ast.ForStmt
		var allocation *ast.CallExpr
		ast.Inspect(function.Body, func(node ast.Node) bool {
			if value, ok := node.(*ast.ForStmt); ok {
				loop = value
				return false
			}
			if call, ok := node.(*ast.CallExpr); ok {
				if selected, ok := call.Fun.(*ast.SelectorExpr); ok && selected.Sel.Name == "Realloc" {
					allocation = call
				}
			}
			return true
		})
		if loop == nil || len(loop.Body.List) != 1 {
			continue
		}
		check, ok := loop.Body.List[0].(*ast.IfStmt)
		if !ok {
			continue
		}
		assignment, ok := check.Init.(*ast.AssignStmt)
		if !ok || len(assignment.Rhs) != 1 {
			continue
		}
		call, ok := assignment.Rhs[0].(*ast.CallExpr)
		if !ok {
			continue
		}
		var stride ast.Expr
		for i, arg := range call.Args {
			if offset, ok := arg.(*ast.BinaryExpr); ok && offset.Op == token.ADD {
				if product, ok := offset.Y.(*ast.BinaryExpr); ok && product.Op == token.MUL {
					if index, ok := product.X.(*ast.Ident); ok && index.Name == "i_" {
						stride = product.Y
						call.Args[i] = ast.NewIdent("offset")
					}
				}
			}
			if index, ok := arg.(*ast.IndexExpr); ok {
				if collection, ok := index.X.(*ast.Ident); ok && collection.Name == "v" {
					call.Args[i] = ast.NewIdent("value")
				}
			}
		}
		if stride == nil {
			continue
		}
		text := func(node ast.Node) string {
			var output bytes.Buffer
			_ = printer.Fprint(&output, set, node)
			return output.String()
		}
		var element ast.Expr
		if reading {
			if list, ok := function.Type.Results.List[0].Type.(*ast.ArrayType); ok {
				element = list.Elt
			}
		} else {
			last := function.Type.Params.List[len(function.Type.Params.List)-1]
			if list, ok := last.Type.(*ast.ArrayType); ok {
				element = list.Elt
			}
		}
		if element == nil {
			return nil, fmt.Errorf("list codec %s has no element type", name)
		}
		memory := "cc"
		if strings.HasPrefix(name, "lift") || strings.HasPrefix(name, "lower") {
			memory = "callee"
		}
		var body string
		if reading {
			arguments := "stack, " + text(stride)
			method := "ReadFlat"
			if strings.Contains(name, "MemList") {
				method = "ReadMemory"
				arguments = fmt.Sprintf("%s.Memory(), ptr, %s, %q", memory, text(stride), name)
			}
			body = fmt.Sprintf("return canonical.%s(%s, func(offset uint32) (%s, error) { return %s })", method, arguments, text(element), text(call))
		} else {
			if allocation == nil || len(allocation.Args) != 5 {
				return nil, fmt.Errorf("list codec %s has no allocation", name)
			}
			method, target := "WriteFlat", "stack"
			if strings.Contains(name, "MemList") {
				method, target = "WriteMemory", "ptr"
			}
			body = fmt.Sprintf("return canonical.%s(ctx, %s, %s, v, %s, %s, %q, func(offset uint32, value %s) error { return %s })", method, memory, target, text(allocation.Args[3]), text(stride), name, text(element), text(call))
		}
		if reading || strings.HasPrefix(name, "lower") {
			body = "lstT_ := ty.(wacogo.TypeList); " + body
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), "body.go", "package body; func replacement(){"+body+"}", 0)
		if err != nil {
			return nil, fmt.Errorf("share list codec %s: %w", name, err)
		}
		function.Body = parsed.Decls[0].(*ast.FuncDecl).Body
		changed = true
	}
	if !changed {
		return data, nil
	}
	astutil.AddImport(set, file, "actionlint.kjanat.dev/v2/internal/plugins/protocol/canonical")
	var output bytes.Buffer
	if err := printer.Fprint(&output, set, file); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}
