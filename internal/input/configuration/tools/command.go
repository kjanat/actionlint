// Package tools owns external-tool invocation settings.
package tools

import (
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"actionlint.kjanat.dev/v2/internal/input/configuration/binding"
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

// backendDocument is the wire form of a Backend. A transport the backend does
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

// DecodeConfig reads exactly one of a process or a wasm mapping and checks
// it as NewCommand or NewModule does.
func (b *Backend) DecodeConfig(value any) error {
	var document backendDocument
	if err := binding.Decode(value, &document); err != nil {
		return err
	}
	var next Backend
	if document.Process != nil {
		command, err := NewCommand(document.Process.Executable, document.Process.Args, document.Process.Env)
		if err != nil {
			return binding.At("process", err)
		}
		next.transport = command
	}
	if document.Wasm != nil {
		module, err := NewModule(document.Wasm.Path)
		if err != nil {
			return binding.At("wasm", err)
		}
		next.transport = module
	}
	if (document.Process == nil) == (document.Wasm == nil) {
		return errors.New("backend requires exactly one of a process mapping or a wasm mapping")
	}
	*b = next
	return nil
}

// EncodeConfig writes the selected transport in the form DecodeConfig reads.
// The zero Backend is written as null.
func (b *Backend) EncodeConfig() (any, error) {
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
func NormalizePaths(value any, directory string) {
	object, ok := value.(map[string]any)
	if !ok {
		return
	}
	tools, _ := object["tools"].(map[string]any)
	for _, value := range tools {
		entry, _ := value.(map[string]any)
		backend, _ := entry["backend"].(map[string]any)
		process, _ := backend["process"].(map[string]any)
		if executable, ok := process["executable"].(string); ok && strings.ContainsAny(executable, `/\`) && !filepath.IsAbs(executable) {
			process["executable"] = filepath.Join(directory, executable)
		}
		wasm, _ := backend["wasm"].(map[string]any)
		if module, ok := wasm["path"].(string); ok && module != "" && !strings.Contains(module, "://") && !filepath.IsAbs(module) {
			wasm["path"] = filepath.Join(directory, module)
		}
	}
	overrides, _ := object["overrides"].([]any)
	for _, override := range overrides {
		NormalizePaths(override, directory)
	}
}
