// Package tools defines process and Wasm backend settings for external tools.
package tools

import (
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"actionlint.kjanat.dev/v2/internal/input/configuration/configdecode"
)

//go:generate go run ../../../development/generators/owned-copies -root ../../../.. -package ./internal/input/configuration/tools -type BackendSelection -output internal/input/configuration/tools/owned_backend.gen.go

// BackendSelection behaves as an omitted backend when it is the zero value.
type BackendSelection struct {
	transport interface{ backendTransport() } `clone:"shared"`
}

func ProcessBackend(command Command) *BackendSelection {
	return &BackendSelection{transport: command}
}

func WasmBackend(module Module) *BackendSelection {
	return &BackendSelection{transport: module}
}

func (b *BackendSelection) Process() (Command, bool) {
	if b == nil {
		return Command{}, false
	}
	command, ok := b.transport.(Command)
	return command, ok
}

func (b *BackendSelection) Wasm() (Module, bool) {
	if b == nil {
		return Module{}, false
	}
	module, ok := b.transport.(Module)
	return module, ok
}

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

func (b *BackendSelection) DecodeConfig(value any) error {
	var document backendDocument
	if err := configdecode.Decode(value, &document); err != nil {
		return err
	}
	var next BackendSelection
	if document.Process != nil {
		command, err := NewCommand(document.Process.Executable, document.Process.Args, document.Process.Env)
		if err != nil {
			return configdecode.At("process", err)
		}
		next.transport = command
	}
	if document.Wasm != nil {
		module, err := NewModule(document.Wasm.Path)
		if err != nil {
			return configdecode.At("wasm", err)
		}
		next.transport = module
	}
	if (document.Process == nil) == (document.Wasm == nil) {
		return errors.New("backend requires exactly one of a process mapping or a wasm mapping")
	}
	*b = next
	return nil
}

// EncodeConfig writes the zero BackendSelection as null.
func (b *BackendSelection) EncodeConfig() (any, error) {
	if command, ok := b.Process(); ok {
		return backendDocument{Process: &commandDocument{Executable: command.Executable(), Args: command.Args(), Env: command.Env()}}, nil
	}
	if module, ok := b.Wasm(); ok {
		return backendDocument{Wasm: &moduleDocument{Path: module.Path()}}, nil
	}
	return nil, nil
}

type Module struct {
	path string
}

func (Module) backendTransport() {}

// NewModule accepts a raw or gzip-compressed module path, file URI, HTTP(S) URL or
// oci://<host>/<repository>@sha256:<hex> reference, with an optional #sha256=<hex> fragment.
func NewModule(path string) (Module, error) {
	if path == "" || strings.ContainsRune(path, 0) {
		return Module{}, errors.New("wasm backend requires a module path")
	}
	return Module{path: path}, nil
}

// Path is relative to the declaring config file, or to the analysis directory for inline configs.
func (m Module) Path() string { return m.path }

type Command struct {
	executable string
	args       []string
	env        map[string]string
}

func (Command) backendTransport() {}

// NewCommand keeps the tool's default for an empty executable and prepends args to actionlint's analysis arguments.
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

// Executable returns "" for the tool's default.
func (c Command) Executable() string { return c.executable }

func (c Command) Args() []string { return slices.Clone(c.args) }

func (c Command) Env() map[string]string { return maps.Clone(c.env) }

// NormalizePaths must run before independently declared configs are merged.
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

func (b *BackendSelection) CopyConfig() (any, error) { return copyOwnedBackendSelection(*b), nil }
