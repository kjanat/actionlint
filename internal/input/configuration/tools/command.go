// Package tools owns external-tool invocation settings.
package tools

import (
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"github.com/kjanat/go-yaml/v4"
)

// Backend selects one transport: a native process or a core WebAssembly engine.
// Executable names and argv are never the general backend contract.
//
// ProcessBackend, WasmBackend and decoding select exactly one transport. The
// zero Backend selects neither and behaves as an omitted backend.
type Backend struct {
	// transport is a Command or a Module.
	transport interface{ backendTransport() }
}

// ProcessBackend selects native invocation of command.
func ProcessBackend(command Command) *Backend {
	return &Backend{transport: command}
}

// WasmBackend selects the core WebAssembly engine at module.
func WasmBackend(module Module) *Backend {
	return &Backend{transport: module}
}

// Process returns the native invocation when the backend selects one.
func (b *Backend) Process() (Command, bool) {
	if b == nil {
		return Command{}, false
	}
	command, ok := b.transport.(Command)
	return command, ok
}

// Wasm returns the core WebAssembly engine when the backend selects one.
func (b *Backend) Wasm() (Module, bool) {
	if b == nil {
		return Module{}, false
	}
	module, ok := b.transport.(Module)
	return module, ok
}

// backendDocument is the YAML form of a Backend. A transport the backend does
// not select is omitted or null.
type backendDocument struct {
	Process *commandDocument `yaml:"process,omitempty"`
	Wasm    *moduleDocument  `yaml:"wasm,omitempty"`
}

type commandDocument struct {
	Executable string            `yaml:"executable,omitempty"`
	Args       []string          `yaml:"args,omitempty"`
	Env        map[string]string `yaml:"env,omitempty"`
}

type moduleDocument struct {
	Path string `yaml:"path"`
}

// UnmarshalYAML reads exactly one of a process or a wasm mapping and checks
// it as NewCommand or NewModule does.
func (b *Backend) UnmarshalYAML(n *yaml.Node) error {
	var document backendDocument
	if err := n.Load(&document, yaml.WithV3Defaults(), yaml.WithKnownFields()); err != nil {
		return err
	}
	var next Backend
	if document.Process != nil {
		command, err := NewCommand(document.Process.Executable, document.Process.Args, document.Process.Env)
		if err != nil {
			return err
		}
		next.transport = command
	}
	if document.Wasm != nil {
		module, err := NewModule(document.Wasm.Path)
		if err != nil {
			return err
		}
		next.transport = module
	}
	if (document.Process == nil) == (document.Wasm == nil) {
		return errors.New("backend requires exactly one of a process mapping or a wasm mapping")
	}
	*b = next
	return nil
}

// MarshalYAML writes the selected transport in the form UnmarshalYAML reads.
// The zero Backend is written as null.
func (b *Backend) MarshalYAML() (any, error) {
	if command, ok := b.Process(); ok {
		return backendDocument{Process: &commandDocument{Executable: command.Executable(), Args: command.Args(), Env: command.Env()}}, nil
	}
	if module, ok := b.Wasm(); ok {
		return backendDocument{Wasm: &moduleDocument{Path: module.Path()}}, nil
	}
	return nil, nil
}

// Module locates a core WebAssembly engine.
type Module struct {
	path string
}

func (Module) backendTransport() {}

// NewModule locates a core WebAssembly engine at path: a raw or
// gzip-compressed module path, file URI, HTTP(S) URL, or
// oci://<host>/<repository>@sha256:<hex> reference to an image with a
// WebAssembly layer. URI fragments may pin downloaded bytes with
// #sha256=<hex>. path must be nonempty and cannot contain NUL.
func NewModule(path string) (Module, error) {
	if path == "" || strings.ContainsRune(path, 0) {
		return Module{}, errors.New("wasm backend requires a module path")
	}
	return Module{path: path}, nil
}

// Path returns the module location. Relative paths in a config file resolve
// from that file; inline configs use the analysis directory.
func (m Module) Path() string { return m.path }

// Command is an argv invocation, never shell source.
type Command struct {
	executable string
	args       []string
	env        map[string]string
}

func (Command) backendTransport() {}

// NewCommand returns an invocation of executable, a command on PATH or a
// literal path; an empty executable keeps the tool's default. args are
// literal arguments prepended to actionlint's analysis arguments, and env
// overrides named variables in the child process only. NewCommand keeps
// copies of args and env. Values cannot contain NUL, and environment names
// cannot be empty or contain "=".
func NewCommand(executable string, args []string, env map[string]string) (Command, error) {
	if strings.ContainsRune(executable, 0) || slices.ContainsFunc(args, func(arg string) bool { return strings.ContainsRune(arg, 0) }) {
		return Command{}, errors.New("tool executable and arguments cannot contain NUL")
	}
	for _, key := range slices.Sorted(maps.Keys(env)) {
		if key == "" || strings.ContainsAny(key, "=\x00") || strings.ContainsRune(env[key], 0) {
			return Command{}, fmt.Errorf("invalid tool environment entry %q", key)
		}
	}
	return Command{executable: executable, args: slices.Clone(args), env: maps.Clone(env)}, nil
}

// Executable returns the command on PATH or literal path, or "" for the
// tool's default. Relative paths in a config file resolve from that file;
// inline configs use the analysis directory.
func (c Command) Executable() string { return c.executable }

// Args returns a copy of the literal arguments prepended to actionlint's
// analysis arguments.
func (c Command) Args() []string { return slices.Clone(c.args) }

// Env returns a copy of the variables overridden in the child process.
func (c Command) Env() map[string]string { return maps.Clone(c.env) }

// NormalizePaths binds every tool's configured executable and module paths to
// their declaring file, including overrides, before independently declared
// configs are merged.
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
	var entries []*yaml.Node
	if tools := field(node, "tools"); tools != nil && tools.Kind == yaml.MappingNode {
		for i := 1; i < len(tools.Content); i += 2 {
			entries = append(entries, tools.Content[i])
		}
	}
	for _, entry := range entries {
		backend := field(entry, "backend")
		executable := field(field(backend, "process"), "executable")
		if executable != nil && executable.Tag == "!!str" && strings.ContainsAny(executable.Value, `/\`) && !filepath.IsAbs(executable.Value) {
			executable.Value = filepath.Join(directory, executable.Value)
		}
		module := field(field(backend, "wasm"), "path")
		if module != nil && module.Tag == "!!str" && module.Value != "" && !strings.Contains(module.Value, "://") && !filepath.IsAbs(module.Value) {
			module.Value = filepath.Join(directory, module.Value)
		}
	}
	if overrides := field(node, "overrides"); overrides != nil {
		for _, child := range overrides.Content {
			NormalizePaths(child, directory)
		}
	}
}
