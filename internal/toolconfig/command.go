// Package toolconfig owns external-tool invocation settings.
package toolconfig

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v4"
)

// Backend selects one transport. Process is the currently available configured
// transport. Future core-WASM and component adapters have their own settings;
// executable names and argv are never the general backend contract.
type Backend struct {
	// Process configures native executable invocation.
	Process *Command `yaml:"process" jsonschema:"required"`
}

func (b *Backend) UnmarshalYAML(n *yaml.Node) error {
	type plain Backend
	var next plain
	if err := n.Load(&next, yaml.WithV3Defaults(), yaml.WithKnownFields()); err != nil {
		return err
	}
	if next.Process == nil {
		return errors.New("backend requires a process mapping")
	}
	*b = Backend(next)
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
		executable := field(field(field(field(field(node, "tools"), name), "backend"), "process"), "executable")
		if executable != nil && executable.Tag == "!!str" && strings.ContainsAny(executable.Value, `/\`) && !filepath.IsAbs(executable.Value) {
			executable.Value = filepath.Join(directory, executable.Value)
		}
	}
	if overrides := field(node, "overrides"); overrides != nil {
		for _, child := range overrides.Content {
			NormalizePaths(child, directory)
		}
	}
}
