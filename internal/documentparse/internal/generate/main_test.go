package main

import (
	"bytes"
	"errors"
	"go/token"
	"go/types"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestMapKeyErrorRejectsMutableKeys(t *testing.T) {
	fixture := types.NewPackage("fixture", "fixture")
	pointerKey := types.NewPointer(types.Typ[types.Int])
	structKey := types.NewStruct([]*types.Var{
		types.NewVar(token.NoPos, fixture, "Pointer", types.NewPointer(types.Typ[types.Int])),
	}, nil)
	for name, key := range map[string]types.Type{
		"pointer":                   pointerKey,
		"struct containing pointer": structKey,
		"unsafe pointer":            types.Typ[types.UnsafePointer],
	} {
		t.Run(name, func(t *testing.T) {
			if err := mapKeyError(key); err == nil {
				t.Fatal("accepted map key containing a pointer")
			}
		})
	}
}

func TestInterfaceImplementationsRejectUnsupportedShapes(t *testing.T) {
	for _, test := range []struct {
		name       string
		typeName   string
		methodRecv bool
		wantError  bool
	}{
		{name: "unexported pointer implementation", typeName: "privateImpl", wantError: true},
		{name: "value receiver implementation", typeName: "ValueImpl", methodRecv: true, wantError: true},
		{name: "exported pointer implementation", typeName: "PointerImpl"},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := types.NewPackage("fixture", "fixture")
			scope := types.NewScope(fixture.Scope(), token.NoPos, token.NoPos, "fixture")
			namedInterface := types.NewInterfaceType([]*types.Func{
				types.NewFunc(token.NoPos, fixture, "Run", types.NewSignatureType(nil, nil, nil, nil, nil, false)),
			}, nil).Complete()
			typeName := types.NewTypeName(token.NoPos, fixture, test.typeName, nil)
			named := types.NewNamed(typeName, types.NewStruct(nil, nil), nil)
			receiver := types.Type(named)
			if !test.methodRecv {
				receiver = types.NewPointer(named)
			}
			methodType := types.NewSignatureType(
				types.NewVar(token.NoPos, fixture, "", receiver), nil, nil, nil, nil, false,
			)
			named.AddMethod(types.NewFunc(token.NoPos, fixture, "Run", methodType))
			if err := scope.Insert(typeName); err != nil {
				t.Fatal(err)
			}
			implementations, err := interfaceImplementations(namedInterface, scope)
			if (err != nil) != test.wantError {
				t.Fatalf("interfaceImplementations() error = %v, wantError %t", err, test.wantError)
			}
			if err == nil && (len(implementations) != 1 || implementations[0] != named) {
				t.Fatalf("implementations = %v, want only %s", implementations, named)
			}
		})
	}
}

func TestGeneratorRejectsUnsupportedShapesBeforeEmitting(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	generateDir := filepath.Dir(file)
	temporary := t.TempDir()
	executable := filepath.Join(temporary, "generate")
	if runtime.GOOS == "windows" {
		executable += ".exe"
	}
	build := exec.CommandContext(t.Context(), "go", "build", "-o", executable, ".")
	build.Dir = generateDir
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build generator: %v\n%s", err, output)
	}

	for _, test := range []struct {
		name, source, wantError string
	}{
		{
			name:      "pointer map key",
			source:    "package workflow\ntype Workflow struct { Values map[*int]string }\n",
			wantError: "unsupported mutable map key type",
		},
		{
			name:      "struct map key containing pointer",
			source:    "package workflow\ntype Key struct { Pointer *int }\ntype Workflow struct { Values map[Key]string }\n",
			wantError: "unsupported mutable map key type",
		},
		{
			name:      "unsafe pointer map key",
			source:    "package workflow\nimport \"unsafe\"\ntype Workflow struct { Values map[unsafe.Pointer]string }\n",
			wantError: "unsupported mutable map key type",
		},
		{
			name: "unexported interface implementation",
			source: "package workflow\ntype Exec interface { run() }\ntype privateImpl struct{}\nfunc (*privateImpl) run() {}\n" +
				"type Workflow struct { Value Exec }\n",
			wantError: "unsupported unexported implementation",
		},
		{
			name: "value receiver interface implementation",
			source: "package workflow\ntype Exec interface { run() }\ntype ValueImpl struct{}\nfunc (ValueImpl) run() {}\n" +
				"type Workflow struct { Value Exec }\n",
			wantError: "unsupported value-receiver implementation",
		},
		{
			name: "non-struct pointer interface implementation",
			source: "package workflow\ntype Exec interface { run() }\ntype StringImpl string\nfunc (*StringImpl) run() {}\n" +
				"type Workflow struct { Value Exec }\n",
			wantError: "unsupported non-struct implementation",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := filepath.Join(t.TempDir(), "fixture")
			workflow := filepath.Join(fixture, "internal", "workflow")
			if err := os.MkdirAll(workflow, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(fixture, "go.mod"), []byte("module fixture\n\ngo 1.27.0\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(workflow, "workflow.go"), []byte(test.source), 0o600); err != nil {
				t.Fatal(err)
			}
			output := filepath.Join(t.TempDir(), "frozen")
			if err := os.MkdirAll(output, 0o755); err != nil {
				t.Fatal(err)
			}
			command := exec.CommandContext(t.Context(), executable, fixture, output)
			if result, err := command.CombinedOutput(); err == nil || !bytes.Contains(result, []byte(test.wantError)) {
				t.Fatalf("generator output = %q, error = %v; want failure containing %q", result, err, test.wantError)
			}
			entries, err := os.ReadDir(output)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 0 {
				t.Fatalf("generator emitted files for unsupported shape: %v", entries)
			}
		})
	}
}

func TestGeneratedFilesAreCurrent(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	generateDir := filepath.Dir(file)
	root := filepath.Clean(filepath.Join(generateDir, "..", "..", "..", ".."))
	temporary := t.TempDir()
	target := filepath.Join(temporary, "internal", "workflow", "declaration")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(t.Context(), "go", "run", ".", root, target)
	command.Dir = generateDir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("go run generator: %v\n%s", err, output)
	}
	aliasPath := filepath.Join(temporary, "internal", "documentparse", "owned_views.gen.go")
	if _, err := os.Stat(aliasPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("generator emitted parser view aliases: %v", err)
	}
	for _, path := range []struct {
		generated string
		checkedIn string
	}{
		{filepath.Join(target, "clone.gen.go"), filepath.Join(root, "internal", "workflow", "declaration", "clone.gen.go")},
		{filepath.Join(target, "views.gen.go"), filepath.Join(root, "internal", "workflow", "declaration", "views.gen.go")},
	} {
		generated, err := os.ReadFile(path.generated)
		if err != nil {
			t.Fatal(err)
		}
		checkedIn, err := os.ReadFile(path.checkedIn)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(generated, checkedIn) {
			t.Errorf("generated output %s is stale; run go generate ./internal/documentparse", path.checkedIn)
		}
	}
}
