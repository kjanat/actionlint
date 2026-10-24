package main

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Wacogo v0.1.0 has no filename option. Stage its output so only .gen.go files
// reach the bindings tree, including after subsequent regeneration.
func generateBindings() error {
	temporary, err := os.MkdirTemp("", "actionlint-wit-bindings-")
	if err != nil {
		return err
	}
	defer func() {
		if err := os.RemoveAll(temporary); err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
	}()
	for _, world := range []struct{ name, source string }{
		{"annotated-plugin", "."},
		// Preserve the host interface's canonical analyzer.wit source header.
		{"plugin", "analyzer.wit"},
	} {
		cmd := exec.Command("go", "-C", "../../crates/actionlint-extension-api/wit", "tool", "wacogo-witgen", "generate",
			"-w", "kjanat:actionlint/"+world.name, "-o", temporary,
			"-p", "actionlint.kjanat.dev/v2/internal/plugins/component/bindings", world.source)
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			return err
		}
	}
	return filepath.WalkDir(temporary, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return fmt.Errorf("unexpected WIT binding output: %s", path)
		}
		relative, err := filepath.Rel(temporary, path)
		if err != nil {
			return err
		}
		output := filepath.Join("../../internal/plugins/component/bindings", strings.TrimSuffix(relative, ".go")+".gen.go")
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
			return err
		}
		return os.WriteFile(output, data, 0o644)
	})
}
