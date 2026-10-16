// Package toolconfig owns external-tool invocation settings.
package toolconfig

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v4"
)

// Backend selects one transport: a native process or a core WebAssembly engine.
// Executable names and argv are never the general backend contract.
type Backend struct {
	// Process configures native executable invocation.
	Process *Command `yaml:"process,omitempty"`
	// Wasm selects a core WebAssembly engine that follows the actionlint-plugin-ruff JSON ABI.
	Wasm *Module `yaml:"wasm,omitempty"`
}

func (b *Backend) UnmarshalYAML(n *yaml.Node) error {
	type plain Backend
	var next plain
	if err := n.Load(&next, yaml.WithV3Defaults(), yaml.WithKnownFields()); err != nil {
		return err
	}
	if (next.Process == nil) == (next.Wasm == nil) {
		return errors.New("backend requires exactly one of a process mapping or a wasm mapping")
	}
	*b = Backend(next)
	return nil
}

// Module locates a core WebAssembly engine.
type Module struct {
	// Path is the raw or gzip-compressed module file. Relative paths resolve
	// from the declaring config file; inline configs use the analysis directory.
	Path string `yaml:"path" jsonschema:"required"`
}

func (m *Module) UnmarshalYAML(n *yaml.Node) error {
	type plain Module
	var next plain
	if err := n.Load(&next, yaml.WithV3Defaults(), yaml.WithKnownFields()); err != nil {
		return err
	}
	if next.Path == "" || strings.ContainsRune(next.Path, 0) {
		return errors.New("wasm backend requires a module path")
	}
	*m = Module(next)
	return nil
}

// Command is an argv invocation, never shell source.
type Command struct {
	// Executable is a command on PATH or a literal path. Relative paths resolve
	// from the declaring config file; inline configs use the analysis directory.
	Executable string `yaml:"executable,omitempty"`
	// Args are literal arguments prepended to actionlint's analysis arguments.
	Args []string `yaml:"args,omitempty" jsonschema:"nullable"`
	// Env overrides named variables in the child process only.
	Env map[string]string `yaml:"env,omitempty" jsonschema:"nullable"`
}

func (c *Command) UnmarshalYAML(n *yaml.Node) error {
	type plain Command
	var next plain
	if err := n.Load(&next, yaml.WithV3Defaults(), yaml.WithKnownFields()); err != nil {
		return err
	}
	for _, value := range append([]string{next.Executable}, next.Args...) {
		if strings.ContainsRune(value, 0) {
			return errors.New("tool executable and arguments cannot contain NUL")
		}
	}
	for key, value := range next.Env {
		if key == "" || strings.ContainsAny(key, "=\x00") || strings.ContainsRune(value, 0) {
			return fmt.Errorf("invalid tool environment entry %q", key)
		}
	}
	*c = Command(next)
	return nil
}

// NormalizePaths binds configured executable paths to their declaring file,
// including overrides, before independently declared configs are merged.
func NormalizePaths(node *yaml.Node, directory string) {
	if node == nil {
		return
	}
	if node.Kind == yaml.DocumentNode {
		for _, child := range node.Content {
			NormalizePaths(child, directory)
		}
		return
	}
	field := func(n *yaml.Node, name string) *yaml.Node {
		if n == nil || n.Kind != yaml.MappingNode {
			return nil
		}
		for i := 0; i+1 < len(n.Content); i += 2 {
			if n.Content[i].Value == name {
				return n.Content[i+1]
			}
		}
		return nil
	}
	for _, name := range []string{"ruff", "shellcheck"} {
		backend := field(field(field(node, "tools"), name), "backend")
		executable := field(field(backend, "process"), "executable")
		if executable != nil && executable.Tag == "!!str" && strings.ContainsAny(executable.Value, `/\`) && !filepath.IsAbs(executable.Value) {
			executable.Value = filepath.Join(directory, executable.Value)
		}
		module := field(field(backend, "wasm"), "path")
		if module != nil && module.Tag == "!!str" && module.Value != "" && !filepath.IsAbs(module.Value) {
			module.Value = filepath.Join(directory, module.Value)
		}
	}
	if overrides := field(node, "overrides"); overrides != nil {
		for _, child := range overrides.Content {
			NormalizePaths(child, directory)
		}
	}
}
