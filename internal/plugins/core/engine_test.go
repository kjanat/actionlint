package core_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"actionlint.kjanat.dev/analyzer"
	corewasm "actionlint.kjanat.dev/internal/plugins/core"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/experimental"
)

func enginePath(name string) string {
	return filepath.Join("testdata", "engines", name+".wasm")
}

func loadEngine(t *testing.T, path string, options corewasm.Options) *corewasm.Engine {
	t.Helper()
	engine, err := corewasm.Load(t.Context(), path, options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := engine.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return engine
}

func TestEchoEngineChecksAndValidates(t *testing.T) {
	engine := loadEngine(t, enginePath("echo"), corewasm.Options{})
	metadata := engine.Metadata()
	if metadata.ID != "actionlint-engine-echo" || metadata.Engine.Version != "1.0.0" || metadata.Capabilities.MaxInputBytes != 8388608 {
		t.Fatalf("metadata=%+v", metadata)
	}
	if engine.Limits() != (corewasm.Limits{Timeout: corewasm.DefaultTimeout, MemoryPages: corewasm.DefaultMemoryPages}) {
		t.Fatalf("limits=%+v", engine.Limits())
	}
	diagnostics, err := engine.Check(t.Context(), analyzer.ScriptRequest{Source: "label = 'é🦀'; print(missing)\n", Filename: "workflow.step.py", Config: json.RawMessage(`{"lint":{"select":["F821"]}}`)})
	if err != nil {
		t.Fatal(err)
	}
	want := analyzer.ScriptDiagnostic{
		Filename: "workflow.step.py", Code: "F821", Name: "undefined-name", Severity: "error", Message: "Undefined name `missing`",
		Location: analyzer.ScriptPosition{Row: 1, Column: 21}, EndLocation: analyzer.ScriptPosition{Row: 1, Column: 28},
		URL: "https://docs.astral.sh/ruff/rules/undefined-name/",
	}
	if len(diagnostics) != 1 || diagnostics[0] != want {
		t.Fatalf("diagnostics=%+v", diagnostics)
	}
	clean, err := engine.Check(t.Context(), analyzer.ScriptRequest{Source: "print('ok')\n", Filename: "step.py"})
	if err != nil || len(clean) != 0 {
		t.Fatalf("clean=%+v err=%v", clean, err)
	}
	if _, err := engine.Check(t.Context(), analyzer.ScriptRequest{Source: "x\n", Filename: "step.py", Config: json.RawMessage(`{"select":["FAKE999"]}`)}); err == nil || err.Error() != "engine actionlint-engine-echo: invalid Ruff config: unknown rule selector FAKE" {
		t.Fatalf("config error=%v", err)
	}
	if err := engine.ValidateConfig(t.Context(), json.RawMessage(`{"lint":{"select":["F821"]}}`)); err != nil {
		t.Fatal(err)
	}
	if err := engine.ValidateConfig(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	if err := engine.ValidateConfig(t.Context(), json.RawMessage(`{"select":["FAKE999"]}`)); err == nil || !strings.HasSuffix(err.Error(), "unknown rule selector FAKE") {
		t.Fatalf("validate error=%v", err)
	}
	if _, err := engine.Check(t.Context(), analyzer.ScriptRequest{Source: strings.Repeat("x", 9*1024*1024), Filename: "step.py"}); err == nil || !strings.Contains(err.Error(), "exceeds the engine limit of 8388608 bytes") {
		t.Fatalf("limit error=%v", err)
	}
}

func TestGzipModuleLoads(t *testing.T) {
	raw, err := os.ReadFile(enginePath("echo"))
	if err != nil {
		t.Fatal(err)
	}
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "echo.wasm.gz")
	if err := os.WriteFile(path, compressed.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	engine := loadEngine(t, path, corewasm.Options{CacheDir: filepath.Join(t.TempDir(), "cache")})
	if engine.Metadata().ID != "actionlint-engine-echo" {
		t.Fatalf("metadata=%+v", engine.Metadata())
	}
}

func TestFailedResponsePreservesDiagnostics(t *testing.T) {
	engine := loadEngine(t, enginePath("echo"), corewasm.Options{})
	findings, err := engine.Check(t.Context(), analyzer.ScriptRequest{Source: "missing # partial", Filename: "step.py"})
	if err == nil || !strings.Contains(err.Error(), "analysis incomplete") {
		t.Fatalf("missing completion failure: %v", err)
	}
	if len(findings) != 1 || findings[0].Code != "F821" || findings[0].Location.Column != 1 || findings[0].EndLocation.Column != 8 {
		t.Fatalf("partial findings lost: %+v", findings)
	}
}

func TestFailuresDiscardTheInstance(t *testing.T) {
	engine := loadEngine(t, enginePath("echo"), corewasm.Options{MemoryPages: 64})
	if engine.Limits().String() != "memory 4 MiB, time 10s" {
		t.Fatalf("limits=%s", engine.Limits())
	}
	for source, want := range map[string]string{
		"trap":  "engine actionlint-engine-echo failed (memory 4 MiB, time 10s)",
		"grow":  "engine actionlint-engine-echo failed (memory 4 MiB, time 10s)",
		"error": "",
	} {
		_, err := engine.Check(t.Context(), analyzer.ScriptRequest{Source: source, Filename: "step.py"})
		if want == "" {
			if err != nil {
				t.Fatalf("%s: err=%v", source, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%s: err=%v", source, err)
		}
		recovered, err := engine.Check(t.Context(), analyzer.ScriptRequest{Source: "print(missing)", Filename: "step.py"})
		if err != nil || len(recovered) != 1 {
			t.Fatalf("after %s: diagnostics=%+v err=%v", source, recovered, err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := engine.Check(ctx, analyzer.ScriptRequest{Source: "hang", Filename: "step.py"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled: err=%v", err)
	}
}

func TestCancelledCallDiscardsTheInstance(t *testing.T) {
	callCtx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var instances []api.Module
	listener := experimental.FunctionListenerFunc(func(_ context.Context, instance api.Module, _ api.FunctionDefinition, _ []uint64, _ experimental.StackIterator) {
		instances = append(instances, instance)
		if len(instances) == 1 {
			// Cancel only after acquiring an instance and entering guest code.
			// Compilation and scheduling speed cannot decide this test's outcome.
			cancel()
		}
	})
	loadCtx := experimental.WithFunctionListenerFactory(t.Context(), experimental.FunctionListenerFactoryFunc(func(definition api.FunctionDefinition) experimental.FunctionListener {
		if slices.Contains(definition.ExportNames(), "check") {
			return listener
		}
		return nil
	}))
	engine, err := corewasm.Load(loadCtx, enginePath("echo"), corewasm.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := engine.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	if _, err := engine.Check(callCtx, analyzer.ScriptRequest{Source: "hang", Filename: "step.py"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled guest: %v", err)
	}
	recovered, err := engine.Check(t.Context(), analyzer.ScriptRequest{Source: "print(missing)", Filename: "step.py"})
	if err != nil || len(recovered) != 1 {
		t.Fatalf("after cancellation: diagnostics=%+v err=%v", recovered, err)
	}
	if len(instances) != 2 || instances[0] == instances[1] || !instances[0].IsClosed() {
		t.Fatalf("cancelled instance was not closed and replaced: %v", instances)
	}
}

func TestConcurrentChecks(t *testing.T) {
	engine := loadEngine(t, enginePath("echo"), corewasm.Options{})
	var wg sync.WaitGroup
	errs := make(chan error, 32)
	for range 8 {
		wg.Go(func() {
			for range 4 {
				diagnostics, err := engine.Check(t.Context(), analyzer.ScriptRequest{Source: "print(missing)", Filename: "step.py"})
				if err == nil && (len(diagnostics) != 1 || diagnostics[0].Location.Column != 7) {
					err = errors.New("unexpected diagnostics")
				}
				if err != nil {
					errs <- err
				}
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func TestLoadRefusesIncompatibleModules(t *testing.T) {
	for name, want := range map[string]string{
		"imports":        "module imports env.host; a core engine must not import",
		"missing-export": "metadata advertises no check version this host supports (version 1)",
		"signature":      "export abi_version has signature [] -> [i64]; ABI version 1 needs [] -> [i32]",
		"abi-version":    "module reports ABI version 2; this host implements 1",
		"no-check":       "metadata advertises no check version this host supports (version 1)",
	} {
		engine, err := corewasm.Load(t.Context(), filepath.Join("testdata", "wat", name+".wasm"), corewasm.Options{})
		if err == nil {
			_ = engine.Close(t.Context())
			t.Fatalf("%s loaded", name)
		}
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("%s: err=%v", name, err)
		}
	}
	if _, err := corewasm.Load(t.Context(), filepath.Join("testdata", "missing.wasm"), corewasm.Options{}); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing: err=%v", err)
	}
}

func TestClosedEngineRejectsCalls(t *testing.T) {
	engine, err := corewasm.Load(t.Context(), enginePath("echo"), corewasm.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Check(t.Context(), analyzer.ScriptRequest{Source: "x", Filename: "step.py"}); err == nil {
		t.Fatal("check succeeded after close")
	}
}

func TestInitializationHonorsEngineTimeout(t *testing.T) {
	for _, name := range []string{"hang-start", "hang-abi", "hang-metadata"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			start := time.Now()
			engine, err := corewasm.Load(ctx, filepath.Join("testdata", "wat", name+".wasm"), corewasm.Options{Timeout: 100 * time.Millisecond})
			if engine != nil {
				_ = engine.Close(context.Background())
			}
			if err == nil || ctx.Err() != nil || time.Since(start) > 2*time.Second {
				t.Fatalf("engine timeout was not enforced: elapsed=%s err=%v caller=%v", time.Since(start), err, ctx.Err())
			}
		})
	}
}

func TestCompilationDoesNotConsumeGuestTimeout(t *testing.T) {
	const timeout = 100 * time.Millisecond
	var delay sync.Once
	// Listener construction is part of compilation, before guest execution.
	// Make a cold compilation outlast the call budget without a large fixture
	// or dependence on the compiler's speed.
	ctx := experimental.WithFunctionListenerFactory(t.Context(), experimental.FunctionListenerFactoryFunc(func(api.FunctionDefinition) experimental.FunctionListener {
		delay.Do(func() { time.Sleep(2 * timeout) })
		return nil
	}))
	engine, err := corewasm.Load(ctx, enginePath("echo"), corewasm.Options{Timeout: timeout})
	if err != nil {
		t.Fatalf("compilation consumed guest initialization budget: %v", err)
	}
	t.Cleanup(func() {
		if err := engine.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	findings, err := engine.Check(t.Context(), analyzer.ScriptRequest{Source: "print(missing)", Filename: "step.py"})
	if err != nil || len(findings) != 1 || findings[0].Code != "F821" {
		t.Fatalf("compiled engine failed: findings=%+v err=%v", findings, err)
	}
}

func TestCompilationHonorsCallerCancellation(t *testing.T) {
	for _, name := range []string{"cancel", "deadline"} {
		t.Run(name, func(t *testing.T) {
			deadline := name == "deadline"
			ctx, cancel := context.WithCancel(t.Context())
			want := context.Canceled
			if deadline {
				cancel()
				ctx, cancel = context.WithTimeout(t.Context(), 20*time.Millisecond)
				want = context.DeadlineExceeded
			}
			defer cancel()
			called := false
			listener := experimental.FunctionListenerFunc(func(context.Context, api.Module, api.FunctionDefinition, []uint64, experimental.StackIterator) {
				called = true
			})
			loadCtx := experimental.WithFunctionListenerFactory(ctx, experimental.FunctionListenerFactoryFunc(func(api.FunctionDefinition) experimental.FunctionListener {
				if deadline {
					<-ctx.Done()
				} else {
					cancel()
				}
				return listener
			}))
			engine, err := corewasm.Load(loadCtx, enginePath("echo"), corewasm.Options{})
			if engine != nil {
				_ = engine.Close(context.Background())
			}
			if !errors.Is(err, want) || called {
				t.Fatalf("cancelled compilation entered guest code: err=%v guestCalled=%v", err, called)
			}
		})
	}
}

func TestCleanupFailureDiscardsInstance(t *testing.T) {
	for _, name := range []string{"trap-free-output", "trap-free-input", "hang-free-output"} {
		t.Run(name, func(t *testing.T) {
			engine := loadEngine(t, filepath.Join("testdata", "wat", name+".wasm"), corewasm.Options{Timeout: 100 * time.Millisecond})
			// Trap fixtures fail only their first deallocation. Both calls must
			// fail, proving the second call receives a fresh instance.
			for range 2 {
				_, err := engine.Check(t.Context(), analyzer.ScriptRequest{Source: "hello", Filename: "step.py"})
				if err == nil || !strings.Contains(err.Error(), "engine lifecycle") {
					t.Fatalf("cleanup failure was lost: %v", err)
				}
			}
		})
	}
}
