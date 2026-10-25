package main

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"mvdan.cc/gofumpt/format"
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
		{"managed-plugin", "."},
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
		data, err = orderHostTypes(data)
		if err != nil {
			return err
		}
		data, err = format.Source(data, format.Options{})
		if err != nil {
			return fmt.Errorf("format binding %s: %w", relative, err)
		}
		if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
			return err
		}
		return os.WriteFile(output, data, 0o644)
	})
}

// Wacogo emits local type definitions in discovery order, which can put a
// record before its nested variant. Go declarations require dependency order.
func orderHostTypes(data []byte) ([]byte, error) {
	lines := strings.Split(string(data), "\n")
	declaration := regexp.MustCompile(`^\s*(typ\w+_) := b_\.AddType\(`)
	reference := regexp.MustCompile(`\btyp\w+_\b`)
	var indices []int
	pending := map[string]string{}
	for i, line := range lines {
		if match := declaration.FindStringSubmatch(line); match != nil {
			indices = append(indices, i)
			pending[match[1]] = line
		}
	}
	original := append([]string(nil), lines...)
	for _, index := range indices {
		chosen := ""
		for _, i := range indices {
			match := declaration.FindStringSubmatch(original[i])
			if match == nil {
				continue
			}
			name := match[1]
			line, exists := pending[name]
			if !exists {
				continue
			}
			ready := true
			for _, ref := range reference.FindAllString(line, -1) {
				if ref != name {
					if _, exists := pending[ref]; exists {
						ready = false
					}
				}
			}
			if ready {
				chosen = name
				break
			}
		}
		if chosen == "" {
			return nil, fmt.Errorf("cyclic generated host type definitions")
		}
		lines[index] = pending[chosen]
		lines[index+1] = "\t_ = " + chosen
		delete(pending, chosen)
	}
	return []byte(strings.Join(lines, "\n")), nil
}
