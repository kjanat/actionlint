package core_test

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"actionlint.kjanat.dev/analyzer"
	corewasm "actionlint.kjanat.dev/internal/plugins/core"
	"actionlint.kjanat.dev/internal/tools"
	backendroute "actionlint.kjanat.dev/internal/tools/backend"
	"actionlint.kjanat.dev/rule"
)

// Build small guests directly so timeout tests need no external compiler.
func negotiationFixture(t *testing.T, check, encoding, hang string, versions ...int) string {
	t.Helper()
	return negotiationFixtureWithShells(t, check, encoding, hang, nil, versions...)
}

func negotiationFixtureWithShells(t *testing.T, check, encoding, hang string, shells []string, versions ...int) string {
	t.Helper()
	u := func(n int) []byte { return binary.AppendUvarint(nil, uint64(n)) }
	name := func(s string) []byte { return append(u(len(s)), s...) }
	constant := func(op byte, n int64) []byte {
		result := []byte{op}
		for {
			b := byte(n & 127)
			n >>= 7
			if n == 0 && b&64 == 0 || n == -1 && b&64 != 0 {
				return append(result, b)
			}
			result = append(result, b|128)
		}
	}
	version := 0
	if len(versions) > 0 {
		version = versions[0]
	}
	metadata, err := json.Marshal(corewasm.Metadata{
		MetadataVersion: version,
		ABIVersion:      1, Interfaces: map[string][]corewasm.Interface{"check": {{Version: 1, Export: check}}},
		Capabilities: corewasm.Capabilities{PositionEncoding: encoding, ScriptShells: shells},
	})
	if err != nil {
		t.Fatal(err)
	}
	response := []byte(`{"ok":true,"diagnostics":[]}`)
	module := []byte("\x00asm\x01\x00\x00\x00")
	section := func(id byte, payload []byte) {
		module = append(module, id)
		module = append(module, u(len(payload))...)
		module = append(module, payload...)
	}
	section(1, []byte{6, 0x60, 0, 1, 0x7f, 0x60, 1, 0x7f, 1, 0x7f, 0x60, 1, 0x7f, 0, 0x60, 2, 0x7f, 0x7f, 1, 0x7e, 0x60, 0, 1, 0x7e, 0x60, 0, 0})
	section(3, []byte{6, 0, 1, 2, 3, 4, 5})
	section(5, []byte{1, 0, 1})
	names := []string{"abi_version", "alloc", "dealloc", check, "metadata", "memory"}
	if check == "check" {
		names = append(names, "validate_config")
	}
	exports := u(len(names))
	for i, export := range names {
		exports = append(exports, name(export)...)
		kind, index := byte(0), byte(i)
		if export == "memory" {
			kind, index = 2, 0
		}
		if export == "validate_config" {
			index = 3
		}
		exports = append(exports, kind, index)
	}
	section(7, exports)
	if hang == "start" {
		section(8, []byte{5})
	}
	bodies := [][]byte{constant(0x41, 1), constant(0x41, 4096), {}, constant(0x42, int64(2048)<<32|int64(len(response))), constant(0x42, int64(len(metadata))), {}}
	index := map[string]int{"abi_version": 0, "metadata": 4, "start": 5}
	if hang != "" {
		bodies[index[hang]] = []byte{0x03, 0x40, 0x0c, 0, 0x0b, 0x00}
	}
	code := []byte{6}
	for _, instructions := range bodies {
		body := append([]byte{0}, instructions...)
		body = append(body, 0x0b)
		code = append(code, u(len(body))...)
		code = append(code, body...)
	}
	section(10, code)
	data := []byte{2, 0, 0x41, 0, 0x0b}
	data = append(data, u(len(metadata))...)
	data = append(data, metadata...)
	data = append(data, 0)
	data = append(data, constant(0x41, 2048)...)
	data = append(data, 0x0b)
	data = append(data, u(len(response))...)
	data = append(data, response...)
	section(11, data)
	path := filepath.Join(t.TempDir(), "guest.wasm")
	if err := os.WriteFile(path, module, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLegacyShellSelectorsAreNormalized(t *testing.T) {
	engine := loadEngine(t, negotiationFixtureWithShells(t, "check", "unicode", "", []string{"Python", "python.exe"}), corewasm.Options{})
	shells := engine.Metadata().Capabilities.ScriptShells
	if len(shells) != 2 || shells[0] != "python" || shells[1] != "python" {
		t.Fatalf("legacy shell selectors were not normalized: %q", shells)
	}
}

func TestLegacyShellSelectorsMatchConfiguredRoute(t *testing.T) {
	engine := loadEngine(t, negotiationFixtureWithShells(t, "check", "unicode", "", []string{"Python", "python.exe"}), corewasm.Options{})
	described := coreDescribedBackend{ScriptBackend: engine, descriptor: analyzer.ScriptEngineDescriptor{
		Rule: rule.Descriptor{Name: "ruff"}, Shells: engine.Metadata().Capabilities.ScriptShells,
	}}
	nativeCalls := 0
	registration := backendroute.WithConfiguredEngine(tools.Registration{
		Descriptor: rule.Descriptor{Name: "ruff"},
		Build: func(tools.Context) (tools.Runner, error) {
			return coreBatchRunner(func(_ context.Context, batch tools.Batch) ([]tools.Finding, error) {
				nativeCalls++
				if len(batch.Scripts) != 2 || batch.Scripts[0].Text != "print(1)" || batch.Scripts[1].Text != "print(2)" {
					t.Fatalf("legacy core shell selectors lost supported runs: %+v", batch.Scripts)
				}
				return nil, nil
			}), nil
		},
	}, func(tools.Context) (analyzer.ScriptBackend, error) { return described, nil })
	runner, err := registration.Build(tools.Context{})
	if err != nil {
		t.Fatal(err)
	}
	python, bash := "python", "bash"
	_, err = runner.Analyze(t.Context(), tools.Batch{Scripts: []tools.Script{
		{Text: "print(1)", Shell: &python},
		{Text: "print(2)", Execution: tools.Execution{Shell: "\"C:\\Program Files\\Python\\python.exe\"\u2003{0}"}},
		{Text: "echo no", Shell: &bash},
	}})
	if err != nil || nativeCalls != 1 {
		t.Fatalf("native calls=%d err=%v", nativeCalls, err)
	}
}

type coreDescribedBackend struct {
	analyzer.ScriptBackend
	descriptor analyzer.ScriptEngineDescriptor
}

func (b coreDescribedBackend) ScriptDescriptor() analyzer.ScriptEngineDescriptor {
	return b.descriptor
}

type coreBatchRunner func(context.Context, tools.Batch) ([]tools.Finding, error)

func (r coreBatchRunner) Analyze(ctx context.Context, batch tools.Batch) ([]tools.Finding, error) {
	return r(ctx, batch)
}

func TestNegotiatesAliasedCheckWithoutConfigValidation(t *testing.T) {
	guest := loadEngine(t, negotiationFixture(t, "inspect", "unicode", ""), corewasm.Options{})
	if findings, err := guest.Check(t.Context(), analyzer.ScriptRequest{Source: "echo ok"}); err != nil || len(findings) != 0 {
		t.Fatalf("findings=%v err=%v", findings, err)
	}
	if err := guest.ValidateConfig(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
}

func TestRejectsUnsupportedPositionEncoding(t *testing.T) {
	_, err := corewasm.Load(t.Context(), negotiationFixture(t, "check", "utf16", ""), corewasm.Options{})
	if err == nil || !strings.Contains(err.Error(), "unsupported position encoding") {
		t.Fatalf("err=%v", err)
	}
}

func TestMetadataDocumentVersions(t *testing.T) {
	for _, version := range []int{-1, 0, 1, 2} {
		engine, err := corewasm.Load(t.Context(), negotiationFixture(t, "check", "unicode", "", version), corewasm.Options{})
		if version == 0 || version == 1 {
			if err != nil {
				t.Fatalf("version %d: %v", version, err)
			}
			if err := engine.Close(t.Context()); err != nil {
				t.Fatal(err)
			}
		} else if err == nil || !strings.Contains(err.Error(), "document version") {
			t.Fatalf("unsupported version %d accepted: %v", version, err)
		}
	}
}

func TestRejectsMemoryPagesAboveWasmMaximum(t *testing.T) {
	if _, err := corewasm.Load(t.Context(), "not-opened.wasm", corewasm.Options{MemoryPages: 65537}); err == nil || !strings.Contains(err.Error(), "65536 pages") {
		t.Fatalf("invalid memory limit was not rejected: %v", err)
	}
}

func TestBootstrapGuestCallsHaveDeadline(t *testing.T) {
	for _, operation := range []string{"start", "abi_version", "metadata"} {
		t.Run(operation, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()
			_, err := corewasm.Load(ctx, negotiationFixture(t, "check", "unicode", operation), corewasm.Options{Timeout: 20 * time.Millisecond})
			if err == nil || !strings.Contains(err.Error(), "exceeded its time limit") {
				t.Fatalf("err=%v", err)
			}
			if ctx.Err() != nil {
				t.Fatal("bootstrap used the caller deadline instead of the guest budget")
			}
		})
	}
}
