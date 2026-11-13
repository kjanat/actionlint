package lsp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"actionlint.kjanat.dev/v2/internal/checking"
	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
	"actionlint.kjanat.dev/v2/internal/checking/scripts/registry"
	inputmodel "actionlint.kjanat.dev/v2/internal/input"
	plugininstall "actionlint.kjanat.dev/v2/internal/plugins/installation"
)

type editorTestBackend struct {
	checks     atomic.Int32
	closes     atomic.Int32
	validates  atomic.Int32
	lastSource string
}

func (b *editorTestBackend) Check(_ context.Context, request checkinputs.ScriptRequest) ([]checkinputs.ScriptDiagnostic, error) {
	b.lastSource = request.Source
	b.checks.Add(1)
	return nil, nil
}

func (b *editorTestBackend) ValidateConfig(context.Context, json.RawMessage) error {
	b.validates.Add(1)
	return nil
}

func (b *editorTestBackend) Close(context.Context) error {
	b.closes.Add(1)
	return nil
}

func TestLanguageServerRetainsConfiguredEngineUntilShutdown(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for path, content := range map[string]string{
		".git/HEAD":               "ref: refs/heads/main\n",
		".github/actionlint.yaml": "tools:\n  ruff:\n    backend:\n      wasm: {path: engines/ruff.wasm}\n",
	} {
		full := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	backend := &editorTestBackend{}
	var loads atomic.Int32
	options := checking.Options{
		WorkingDir: root, Tools: registry.Registrations(registry.Selections{"ruff": {Command: "ruff"}}),
		LoadScriptEngine: func(context.Context, string) (checkinputs.ScriptBackend, error) {
			loads.Add(1)
			return backend, nil
		},
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	in, client := io.Pipe()
	receive, out := io.Pipe()
	defer client.Close()
	defer receive.Close()
	stopIO := context.AfterFunc(ctx, func() {
		_ = client.CloseWithError(ctx.Err())
		_ = receive.CloseWithError(ctx.Err())
	})
	defer stopIO()
	done := make(chan error, 1)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		done <- LanguageServer(ctx, in, out, options, plugininstall.Sources{})
		_ = out.Close()
	}()
	t.Cleanup(func() {
		cancel()
		_ = client.Close()
		_ = receive.Close()
		select {
		case <-finished:
		case <-time.After(time.Second):
			t.Error("language server did not finish after closing its streams")
		}
	})
	reader := bufio.NewReader(receive)
	send := func(body string) {
		t.Helper()
		if _, err := fmt.Fprintf(client, "Content-Length: %d\r\n\r\n%s", len(body), body); err != nil {
			t.Fatal(err)
		}
	}
	read := func() {
		t.Helper()
		var size int
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fmt.Sscanf(line, "Content-Length: %d", &size); err != nil {
			t.Fatal(err)
		}
		if _, err := reader.ReadString('\n'); err != nil {
			t.Fatal(err)
		}
		body := make([]byte, size)
		if _, err := io.ReadFull(reader, body); err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(body, []byte("window/logMessage")) {
			t.Fatalf("analysis failed: %s", body)
		}
	}
	send(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	read()
	uri, err := json.Marshal(FileURI(filepath.Join(root, ".github", "workflows", "ci.yml")))
	if err != nil {
		t.Fatal(err)
	}
	workflow := func(script string) string {
		return "on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: " + script + "\n        shell: python\n" + "\npermissions: {}\n"
	}
	first, err := json.Marshal(workflow("print(first)"))
	if err != nil {
		t.Fatal(err)
	}
	send(fmt.Sprintf(`{"jsonrpc":"2.0","method":"textDocument/didOpen","params":{"textDocument":{"uri":%s,"version":1,"text":%s}}}`, uri, first))
	read()
	second, err := json.Marshal(workflow("print(second)"))
	if err != nil {
		t.Fatal(err)
	}
	send(fmt.Sprintf(`{"jsonrpc":"2.0","method":"textDocument/didChange","params":{"textDocument":{"uri":%s,"version":2},"contentChanges":[{"text":%s}]}}`, uri, second))
	read()
	if loads.Load() != 1 || backend.checks.Load() != 2 || backend.closes.Load() != 0 || backend.validates.Load() < 2 {
		t.Fatalf("loads=%d checks=%d closes=%d validates=%d", loads.Load(), backend.checks.Load(), backend.closes.Load(), backend.validates.Load())
	}
	send(`{"jsonrpc":"2.0","id":2,"method":"shutdown"}`)
	read()
	send(`{"jsonrpc":"2.0","method":"exit"}`)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if backend.closes.Load() != 1 || backend.lastSource != "print(second)" {
		t.Fatalf("closes=%d last source=%q", backend.closes.Load(), backend.lastSource)
	}
}

func TestEditorSessionRetainsExplicitPluginSnapshot(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		path, rule, shell, script string
	}{
		{"../plugins/execution/module/testdata/engines/echo.wasm", "echo", "python", "print(missing)"},
		{"../plugins/execution/component/testdata/plugins/no-sudo.wasm", "no-sudo", "bash", "sudo first"},
	} {
		t.Run(test.rule, func(t *testing.T) {
			t.Parallel()
			testEditorPluginSnapshot(t, test.path, test.rule, test.shell, test.script)
		})
	}
}

func testEditorPluginSnapshot(t *testing.T, fixture, rule, shell, firstScript string) {
	t.Helper()
	data, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "echo.wasm")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	session, err := newEditorSession(t.Context(), checking.Options{WorkingDir: t.TempDir()}, []string{path}, plugininstall.Acquisition{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := session.close(t.Context()); err != nil {
			t.Error(err)
		}
	}()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	for _, script := range []string{firstScript, firstScript + " # changed"} {
		document := filepath.Join(session.options.WorkingDir, "ci.yml")
		result, err := analyzeDocuments(t.Context(), session.options, map[string]Document{
			document: {Text: "on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: " + script + "\n        shell: " + shell + "\n" + "\npermissions: {}\n"},
		})
		if err != nil || len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != rule {
			t.Fatalf("plugin snapshot: %+v, %v", result.Diagnostics, err)
		}
	}
}

func TestEditorConfiguredLocalEngineTracksArtifactContent(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../plugins/execution/module/testdata/engines/echo.wasm")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "echo.wasm")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	session, err := newEditorSession(t.Context(), checking.Options{}, nil, plugininstall.Acquisition{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := session.close(t.Context()); err != nil {
			t.Error(err)
		}
	}()
	for range 2 {
		if _, err := session.options.LoadScriptEngine(t.Context(), path); err != nil {
			t.Fatal(err)
		}
	}
	if len(session.engines.loaded) != 1 {
		t.Fatalf("unchanged artifact loaded %d times", len(session.engines.loaded))
	}
	// Add a valid unnamed custom section without changing the engine behavior.
	data = append(data, 0, 2, 0, 0)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := session.options.LoadScriptEngine(t.Context(), path); err != nil {
		t.Fatal(err)
	}
	if len(session.engines.loaded) != 2 {
		t.Fatal("changed artifact reused its previous compiled engine")
	}
}

func TestEditorEngineCancelledLoadCanRetry(t *testing.T) {
	t.Parallel()
	backend := &editorTestBackend{}
	loads := 0
	session, err := newEditorSession(t.Context(), checking.Options{
		LoadScriptEngine: func(context.Context, string) (checkinputs.ScriptBackend, error) {
			loads++
			if loads == 1 {
				return nil, context.Canceled
			}
			return backend, nil
		},
	}, nil, plugininstall.Acquisition{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := session.close(t.Context()); err != nil {
			t.Error(err)
		}
	}()
	if _, err := session.options.LoadScriptEngine(t.Context(), "engine"); !errors.Is(err, context.Canceled) {
		t.Fatalf("first load: %v", err)
	}
	if _, err := session.options.LoadScriptEngine(t.Context(), "engine"); err != nil {
		t.Fatal(err)
	}
	if loads != 2 {
		t.Fatalf("loads=%d", loads)
	}
	if _, err := session.options.LoadScriptEngine(t.Context(), "other"); err != nil {
		t.Fatal(err)
	}
	if loads != 3 {
		t.Fatal("new configured identity did not load")
	}
}

func TestEditorEngineAdmissionHonorsCancellation(t *testing.T) {
	t.Parallel()
	started, release := make(chan struct{}), make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	session, err := newEditorSession(t.Context(), checking.Options{
		LoadScriptEngine: func(context.Context, string) (checkinputs.ScriptBackend, error) {
			close(started)
			<-release
			return &editorTestBackend{}, nil
		},
	}, nil, plugininstall.Acquisition{})
	if err != nil {
		t.Fatal(err)
	}
	first := make(chan error, 1)
	go func() {
		_, err := session.options.LoadScriptEngine(t.Context(), "first")
		first <- err
	}()
	<-started
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	second := make(chan error, 1)
	go func() {
		_, err := session.options.LoadScriptEngine(ctx, "second")
		second <- err
	}()
	select {
	case err := <-second:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("queued load: %v", err)
		}
	case <-time.After(time.Second):
		t.Error("cancelled load waited for another engine to finish loading")
	}
	unblock()
	if err := <-first; err != nil {
		t.Error(err)
	}
	if err := session.close(t.Context()); err != nil {
		t.Error(err)
	}
}

type describedOnlyEditorBackend struct{ checkinputs.ScriptBackend }

func (describedOnlyEditorBackend) ScriptDescriptor() checkinputs.ScriptEngineDescriptor {
	return editorInputDescriptor()
}

type describedValidatingEditorBackend struct{ editorTestBackend }

func (*describedValidatingEditorBackend) ScriptDescriptor() checkinputs.ScriptEngineDescriptor {
	return editorInputDescriptor()
}

func editorInputDescriptor() checkinputs.ScriptEngineDescriptor {
	return checkinputs.ScriptEngineDescriptor{Engine: "ruff", Rule: rule.Descriptor{Name: "ruff"}, ScriptInputs: []checkinputs.ScriptInput{{Action: "owner/python", Input: "source", Language: "python"}}}
}

func TestBorrowEditorEnginePreservesOptionalCapabilities(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name                  string
		backend               checkinputs.ScriptBackend
		described, validating bool
	}{
		{"plain", pythonInputBackend{}, false, false},
		{"validating", &editorTestBackend{}, false, true},
		{"described", describedOnlyEditorBackend{ScriptBackend: pythonInputBackend{}}, true, false},
		{"both", &describedValidatingEditorBackend{}, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			borrowed := borrowEditorEngine(tc.backend)
			_, described := borrowed.(checkinputs.DescribedScriptBackend)
			_, validating := borrowed.(checkinputs.ScriptConfigValidator)
			_, closing := borrowed.(interface{ Close(context.Context) error })
			if described != tc.described || validating != tc.validating || closing {
				t.Fatalf("borrowed capabilities changed: described=%t validating=%t close=%t", described, validating, closing)
			}
		})
	}
}

func TestEditorSessionRetainsConfiguredInputSelectors(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for name, source := range map[string]string{".git/HEAD": "ref: refs/heads/master\n", ".github/actionlint.yaml": "tools:\n  ruff:\n    backend:\n      wasm: {path: engines/ruff.wasm}\n"} {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	engine := &describedValidatingEditorBackend{}
	var loads atomic.Int32
	session, err := newEditorSession(t.Context(), checking.Options{WorkingDir: root, ConfigFile: filepath.Join(root, ".github/actionlint.yaml"), Tools: registry.Registrations(registry.Selections{"ruff": {Command: "ruff"}}), LoadScriptEngine: func(context.Context, string) (checkinputs.ScriptBackend, error) { loads.Add(1); return engine, nil }}, nil, plugininstall.Acquisition{})
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"first", "second"} {
		workflow := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: owner/python@v1\n        with:\n          source: print('" + value + "')\n" + "\npermissions: {}\n"
		_, err := checking.Check(t.Context(), session.options, inputmodel.Request{Kind: inputmodel.Stdin, Name: filepath.Join(root, ".github/workflows/ci.yml"), Reader: bytes.NewBufferString(workflow)})
		if err != nil {
			t.Fatal(err)
		}
	}
	if loads.Load() != 1 || engine.checks.Load() != 2 || engine.closes.Load() != 0 || engine.validates.Load() != 2 {
		t.Errorf("borrowed input capabilities/lifecycle lost: loads=%d checks=%d closes=%d validates=%d", loads.Load(), engine.checks.Load(), engine.closes.Load(), engine.validates.Load())
	}
	if err := session.close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if engine.closes.Load() != 1 || engine.lastSource != "print('second')" {
		t.Fatalf("session did not retain then close engine once: closes=%d source=%q", engine.closes.Load(), engine.lastSource)
	}
}
