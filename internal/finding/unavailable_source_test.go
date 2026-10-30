package finding

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestUnavailableSourceProducerGate(t *testing.T) {
	root := filepath.Join("..", "..")
	// The process runner and the managed document route each produce the
	// marker once.
	producers := []string{
		filepath.Join(root, "internal", "tools", "backend", "sources.go"),
		filepath.Join(root, "internal", "tools", "managed", "sources.go"),
	}
	want := map[string][]string{"ReportedSourceUnavailable": producers}
	counts := map[string]int{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == ".git" || entry.Name() == ".worktrees" || entry.Name() == "testdata" || entry.Name() == "vendor" {
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
		declarations := map[*ast.Ident]bool{}
		for _, declaration := range file.Decls {
			if function, ok := declaration.(*ast.FuncDecl); ok {
				declarations[function.Name] = true
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			if name, ok := node.(*ast.Ident); ok && declarations[name] {
				return true
			}
			name, literal := unavailableSourceUse(node)
			if literal && filepath.Clean(path) != filepath.Clean(filepath.Join(root, "internal", "finding", "unavailable_source.go")) {
				t.Errorf("E7/M07 source marker literal escaped its owner: %s", path)
			}
			expected, tracked := want[name]
			if tracked {
				counts[name+"\x00"+filepath.Clean(path)]++
				if !slices.ContainsFunc(expected, func(producer string) bool { return filepath.Clean(path) == filepath.Clean(producer) }) {
					t.Errorf("E7/M07 temporary source marker escaped producer: %s in %s", name, path)
				}
				return false
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for factory, paths := range want {
		for _, path := range paths {
			if count := counts[factory+"\x00"+filepath.Clean(path)]; count != 1 {
				t.Errorf("E7/M07 producer %s has %d sites in %s, want exactly one until deletion", factory, count, path)
			}
		}
	}
}

func unavailableSourceUse(node ast.Node) (string, bool) {
	switch expression := node.(type) {
	case *ast.SelectorExpr:
		return expression.Sel.Name, false
	case *ast.CallExpr:
		switch name := expression.Fun.(type) {
		case *ast.Ident:
			return "", name.Name == "UnavailableSource"
		case *ast.SelectorExpr:
			return "", name.Sel.Name == "UnavailableSource"
		}
	case *ast.Ident:
		return expression.Name, false
	case *ast.TypeSpec:
		switch name := expression.Type.(type) {
		case *ast.Ident:
			return "", name.Name == "UnavailableSource"
		case *ast.SelectorExpr:
			return "", name.Sel.Name == "UnavailableSource"
		}
	case *ast.CompositeLit:
		switch name := expression.Type.(type) {
		case *ast.Ident:
			return "", name.Name == "UnavailableSource"
		case *ast.SelectorExpr:
			return "", name.Sel.Name == "UnavailableSource"
		}
	}
	return "", false
}

func TestUnavailableProducerGateDetectsUnqualifiedCallsAndLiterals(t *testing.T) {
	for _, expression := range []struct {
		source, factory string
		literal         bool
	}{
		{source: `ReportedSourceUnavailable("tool", "reported.sh", false)`, factory: "ReportedSourceUnavailable"},
		{source: `finding.ReportedSourceUnavailable("tool", "reported.sh", true)`, factory: "ReportedSourceUnavailable"},
		{source: `ReportedSourceUnavailable`, factory: "ReportedSourceUnavailable"},
		{source: `UnavailableSource{state: sourceNotCaptured}`, literal: true},
		{source: `finding.UnavailableSource{}`, literal: true},
		{source: `UnavailableSource(other)`, literal: true},
	} {
		node, err := parser.ParseExpr(expression.source)
		if err != nil {
			t.Fatal(err)
		}
		factories, literals := 0, 0
		ast.Inspect(node, func(node ast.Node) bool {
			name, literal := unavailableSourceUse(node)
			if name == expression.factory && name != "" {
				factories++
				return false
			}
			if literal {
				literals++
			}
			return true
		})
		if expression.factory != "" && factories != 1 || expression.literal && literals != 1 {
			t.Fatalf("producer gate missed %s: factories=%d literals=%d", expression.source, factories, literals)
		}
	}
	file, err := parser.ParseFile(token.NewFileSet(), "escape.go", `package finding; type Alias = UnavailableSource; func escaped() { _ = Alias{state: sourceNotCaptured} }`, 0)
	if err != nil {
		t.Fatal(err)
	}
	escapes := 0
	ast.Inspect(file, func(node ast.Node) bool {
		_, forbidden := unavailableSourceUse(node)
		if forbidden {
			escapes++
		}
		return true
	})
	if escapes == 0 {
		t.Fatal("producer gate missed a literal through a marker type alias")
	}
}

func TestUnavailableSourceStatesAndBinding(t *testing.T) {
	if (UnavailableSource{}).Present() || ReportedSourceUnavailable("tool", "", false).Present() || ReportedSourceUnavailable("", "reported.sh", false).Present() {
		t.Fatal("marker without a provider or an address became a reported source")
	}
	for _, marker := range []UnavailableSource{ReportedSourceUnavailable("tool", "reported.sh", false), ReportedSourceUnavailable("tool", "reported.sh", true)} {
		if !marker.Present() || marker.Provider() != "tool" || marker.Document() != "" || marker.Address() != "reported.sh" {
			t.Fatalf("producer fabricated source identity: %+v", marker)
		}
		if _, err := marker.BindDocument(""); err == nil {
			t.Fatal("empty canonical document accepted")
		}
		bound, err := marker.BindDocument("canonical/reported.sh")
		if err != nil || bound.Document() != "canonical/reported.sh" || bound.Reason() != marker.Reason() || marker.Document() != "" {
			t.Fatalf("binding changed unavailable evidence or parent marker: %+v %v", bound, err)
		}
	}
	if ReportedSourceUnavailable("tool", "reported.sh", false).Reason() != "not-captured" || ReportedSourceUnavailable("tool", "reported.sh", true).Reason() != "read-failed" {
		t.Fatal("reported-source reasons changed")
	}
}
