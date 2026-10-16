package corewasm_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"actionlint.kjanat.dev/analyzer"
	"actionlint.kjanat.dev/internal/corewasm"
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

func TestFailuresDiscardTheInstance(t *testing.T) {
	engine := loadEngine(t, enginePath("echo"), corewasm.Options{Timeout: 300 * time.Millisecond, MemoryPages: 64})
	if engine.Limits().String() != "memory 4 MiB, time 300ms" {
		t.Fatalf("limits=%s", engine.Limits())
	}
	for source, want := range map[string]string{
		"trap":  "engine actionlint-engine-echo failed (memory 4 MiB, time 300ms)",
		"grow":  "engine actionlint-engine-echo failed (memory 4 MiB, time 300ms)",
		"hang":  "engine actionlint-engine-echo exceeded its time limit (memory 4 MiB, time 300ms)",
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
		"missing-export": "module does not export check",
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
