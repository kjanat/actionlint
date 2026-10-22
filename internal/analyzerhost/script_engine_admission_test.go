package analyzerhost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
)

type admissionEngine struct{ closed bool }

func (*admissionEngine) Check(context.Context, analyzer.ScriptRequest) ([]analyzer.ScriptDiagnostic, error) {
	return nil, nil
}

func (*admissionEngine) ValidateConfig(context.Context, json.RawMessage) error { return nil }

func (e *admissionEngine) Close(ctx context.Context) error {
	e.closed = true
	return ctx.Err()
}

func TestScriptEnginesBoundsConcurrentDistinctAdmissions(t *testing.T) {
	var loaded []*admissionEngine
	var loadedMu sync.Mutex
	engines := NewScriptEngines(func(context.Context, string) (analyzer.ScriptBackend, error) {
		backend := &admissionEngine{}
		loadedMu.Lock()
		loaded = append(loaded, backend)
		loadedMu.Unlock()
		return backend, nil
	})
	var wg sync.WaitGroup
	errorsSeen := make(chan error, 20)
	for i := range 20 {
		wg.Go(func() {
			_, err := engines.Load(t.Context(), fmt.Sprintf("engine-%d.wasm", i))
			errorsSeen <- err
		})
	}
	wg.Wait()
	close(errorsSeen)
	accepted, rejected := 0, 0
	for err := range errorsSeen {
		switch {
		case err == nil:
			accepted++
		case strings.Contains(err.Error(), "session limit of 4"):
			rejected++
		default:
			t.Fatalf("unexpected admission error: %v", err)
		}
	}
	if accepted != 4 || rejected != 16 || len(loaded) != 4 {
		t.Fatalf("accepted=%d rejected=%d loaded=%d", accepted, rejected, len(loaded))
	}
	for path, want := range engines.loaded {
		if got, err := engines.Load(t.Context(), path); err != nil || got != want {
			t.Fatalf("cached admission %q: backend=%v err=%v", path, got, err)
		}
	}
	if err := engines.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, backend := range loaded {
		if !backend.closed {
			t.Fatal("admitted engine was not closed")
		}
	}
}

func TestScriptEnginesBoundsFailedAdmissions(t *testing.T) {
	failure := errors.New("invalid module")
	loads := 0
	engines := NewScriptEngines(func(context.Context, string) (analyzer.ScriptBackend, error) {
		loads++
		return nil, failure
	})
	for i := range 20 {
		_, err := engines.Load(t.Context(), fmt.Sprintf("invalid-%d.wasm", i))
		if i < 4 && !errors.Is(err, failure) || i >= 4 && (err == nil || !strings.Contains(err.Error(), "session limit of 4")) {
			t.Fatalf("admission %d: %v", i, err)
		}
	}
	if loads != 4 || len(engines.failed) != 4 {
		t.Fatalf("loads=%d retained failures=%d", loads, len(engines.failed))
	}
	if _, err := engines.Load(t.Context(), "invalid-0.wasm"); !errors.Is(err, failure) || loads != 4 {
		t.Fatalf("cached failure=%v loads=%d", err, loads)
	}
}

func TestScriptEnginesRejectsNilSuccessfulLoad(t *testing.T) {
	loads := 0
	engines := NewScriptEngines(func(context.Context, string) (analyzer.ScriptBackend, error) {
		loads++
		return nil, nil
	})
	for range 2 {
		backend, err := engines.Load(t.Context(), "missing.wasm")
		if backend != nil || err == nil || !strings.Contains(err.Error(), "missing.wasm") || !strings.Contains(err.Error(), "nil backend") {
			t.Fatalf("backend=%v err=%v", backend, err)
		}
	}
	if loads != 1 || len(engines.loaded) != 0 || len(engines.failed) != 1 {
		t.Fatalf("loads=%d loaded=%d failed=%d", loads, len(engines.loaded), len(engines.failed))
	}
	if err := engines.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestScriptEnginesCannotReopenAfterClose(t *testing.T) {
	loads := 0
	engines := NewScriptEngines(func(context.Context, string) (analyzer.ScriptBackend, error) {
		loads++
		return &admissionEngine{}, nil
	})
	if _, err := engines.Load(t.Context(), "engine.wasm"); err != nil {
		t.Fatal(err)
	}
	if err := engines.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := engines.Load(t.Context(), "engine.wasm"); err == nil || !strings.Contains(err.Error(), "closed") || loads != 1 {
		t.Fatalf("closed session loaded engine: loads=%d err=%v", loads, err)
	}
}

func TestScriptEnginesSharesAdmissionBudgetAcrossSuccessAndFailure(t *testing.T) {
	loads := 0
	engines := NewScriptEngines(func(_ context.Context, path string) (analyzer.ScriptBackend, error) {
		loads++
		if strings.HasPrefix(path, "invalid") {
			return nil, errors.New("invalid module")
		}
		return &admissionEngine{}, nil
	})
	defer engines.Close(t.Context())
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := engines.Load(ctx, "cancelled.wasm"); !errors.Is(err, context.Canceled) || loads != 0 {
		t.Fatalf("canceled admission: loads=%d err=%v", loads, err)
	}
	for _, path := range []string{"invalid-1.wasm", "valid-1.wasm", "invalid-2.wasm", "valid-2.wasm"} {
		_, err := engines.Load(t.Context(), path)
		if strings.HasPrefix(path, "valid") && err != nil {
			t.Fatal(err)
		}
	}
	if _, err := engines.Load(t.Context(), "excess.wasm"); err == nil || !strings.Contains(err.Error(), "session limit of 4") {
		t.Fatalf("mixed admissions bypassed budget: %v", err)
	}
	if loads != 4 || len(engines.loaded) != 2 || len(engines.failed) != 2 {
		t.Fatalf("loads=%d retained engines=%d failures=%d", loads, len(engines.loaded), len(engines.failed))
	}
}

func TestScriptEnginesSymlinkAliasesCannotBypassAdmissionLimit(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "engine.wasm")
	if err := os.WriteFile(target, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	loads := 0
	engines := NewScriptEngines(func(context.Context, string) (analyzer.ScriptBackend, error) {
		loads++
		return &admissionEngine{}, nil
	})
	defer engines.Close(t.Context())
	for i := range 5 {
		alias := filepath.Join(root, fmt.Sprintf("alias-%d.wasm", i))
		if err := os.Symlink(target, alias); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		_, err := engines.Load(t.Context(), alias)
		if i < 4 && err != nil || i == 4 && (err == nil || !strings.Contains(err.Error(), "session limit of 4")) {
			t.Fatalf("alias %d: %v", i, err)
		}
	}
	if loads != 4 {
		t.Fatalf("aliases loaded %d engines", loads)
	}
}
