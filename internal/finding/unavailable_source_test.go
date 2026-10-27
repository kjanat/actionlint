package finding

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnavailableSourceProducerGate(t *testing.T) {
	root := filepath.Join("..", "..")
	want := map[string]string{
		"NativeShellcheckUnavailable": filepath.Join(root, "internal", "tools", "shellcheck", "shellcheck_sources.go"),
		"PluginShellcheckUnavailable": filepath.Join(root, "internal", "tools", "shellcheck", "plugin_sources.go"),
	}
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
		ast.Inspect(file, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			expected, tracked := want[selector.Sel.Name]
			if tracked {
				counts[selector.Sel.Name]++
				if filepath.Clean(path) != filepath.Clean(expected) {
					t.Errorf("E7/M07 temporary source marker escaped producer: %s in %s", selector.Sel.Name, path)
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for factory := range want {
		if counts[factory] != 1 {
			t.Errorf("E7/M07 producer %s has %d sites, want exactly one until deletion", factory, counts[factory])
		}
	}
}

func TestUnavailableSourceStatesAndBinding(t *testing.T) {
	if (UnavailableSource{}).Present() || NativeShellcheckUnavailable("", false).Present() {
		t.Fatal("empty marker became a reported source")
	}
	for _, marker := range []UnavailableSource{NativeShellcheckUnavailable("reported.sh", false), NativeShellcheckUnavailable("reported.sh", true), PluginShellcheckUnavailable("reported.sh")} {
		if !marker.Present() || marker.Document() != "" || marker.Address() != "reported.sh" {
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
	if NativeShellcheckUnavailable("reported.sh", false).Reason() != "not-captured" || NativeShellcheckUnavailable("reported.sh", true).Reason() != "read-failed" {
		t.Fatal("native reported-source reasons changed")
	}
}
