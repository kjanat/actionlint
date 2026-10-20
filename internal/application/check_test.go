package application_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/tools/adapters"
)

type ownedBackend struct {
	closeErr, checkErr error
	cancel             context.CancelFunc
	closed             int
	closeContextError  error
}

func (b *ownedBackend) Check(context.Context, analyzer.ScriptRequest) ([]analyzer.ScriptDiagnostic, error) {
	if b.cancel != nil {
		b.cancel()
	}
	return nil, b.checkErr
}

func (b *ownedBackend) Close(ctx context.Context) error {
	b.closed++
	b.closeContextError = ctx.Err()
	return b.closeErr
}

func TestCheckOwnsLoadedBackendLifetime(t *testing.T) {
	checkFailure := errors.New("checker failed")
	closeFailure := errors.New("cleanup failed")
	for _, tc := range []struct {
		name               string
		checkErr, closeErr error
		cancel             bool
	}{
		{name: "success"},
		{name: "analysis failure", checkErr: checkFailure},
		{name: "cleanup failure", closeErr: closeFailure},
		{name: "both failures", checkErr: checkFailure, closeErr: closeFailure},
		{name: "cancellation", checkErr: context.Canceled, cancel: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			config := filepath.Join(root, "config.yaml")
			if err := os.WriteFile(config, []byte("tools:\n  shellcheck:\n    backend:\n      wasm: {path: checker.wasm}\n"), 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			backend := &ownedBackend{checkErr: tc.checkErr, closeErr: tc.closeErr}
			if tc.cancel {
				backend.cancel = cancel
			}
			loads := 0
			result, err := application.Check(ctx, application.Options{
				WorkingDir: root, ConfigFile: config, SkipProjectConfig: true,
				LoadScriptEngine: func(context.Context, string) (analyzer.ScriptBackend, error) { loads++; return backend, nil }, Tools: adapters.Options{ShellCheck: "shellcheck"}.Registrations(),
			}, application.Input{Kind: application.Stdin, Reader: strings.NewReader("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo one\n      - run: echo two\n")})
			if loads != 1 || backend.closed != 1 || backend.closeContextError != nil {
				t.Fatalf("loads=%d closes=%d cleanup context=%v", loads, backend.closed, backend.closeContextError)
			}
			if result == nil || result.FileCount != 1 {
				t.Fatalf("partial result lost: result=%+v err=%v", result, err)
			}
			if tc.checkErr == nil && tc.closeErr == nil && err != nil {
				t.Fatal(err)
			}
			for _, want := range []error{tc.checkErr, tc.closeErr} {
				if want != nil && !errors.Is(err, want) {
					t.Errorf("error=%v, missing cause %v", err, want)
				}
			}
		})
	}
}

func TestEmptyFileSelectionDoesNotDiscoverRepository(t *testing.T) {
	result, err := application.Check(t.Context(), application.Options{WorkingDir: t.TempDir()}, application.Input{Kind: application.Files})
	if err != nil || result == nil || result.FileCount != 0 {
		t.Fatalf("result=%+v error=%v", result, err)
	}
}
