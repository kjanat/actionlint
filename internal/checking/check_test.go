package checking_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	"actionlint.kjanat.dev/v2/internal/checking/scripts/registry"
	inputmodel "actionlint.kjanat.dev/v2/internal/input"
)

type ownedBackend struct {
	closeErr, checkErr error
	cancel             context.CancelFunc
	closed             int
	closeContextError  error
}

func (b *ownedBackend) Check(context.Context, checkinputs.ScriptRequest) ([]checkinputs.ScriptDiagnostic, error) {
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
	t.Parallel()
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
			t.Parallel()
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
			result, err := checking.Check(ctx, checking.Options{
				WorkingDir: root, ConfigFile: config, SkipProjectConfig: true,
				LoadScriptEngine: func(context.Context, string) (checkinputs.ScriptBackend, error) { loads++; return backend, nil }, Tools: registry.Registrations(registry.Selections{"shellcheck": {Command: "shellcheck"}}),
			}, inputmodel.Request{Kind: inputmodel.Stdin, Reader: strings.NewReader("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo one\n      - run: echo two\n")})
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
	t.Parallel()
	result, err := checking.Check(t.Context(), checking.Options{WorkingDir: t.TempDir()}, inputmodel.Request{Kind: inputmodel.Files})
	if err != nil || result == nil || result.FileCount != 0 {
		t.Fatalf("result=%+v error=%v", result, err)
	}
}
