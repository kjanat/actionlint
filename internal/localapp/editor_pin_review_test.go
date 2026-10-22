package localapp

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/testutil"
)

func TestEditorEngineRequiresRemotePinBeforeDownload(t *testing.T) {
	testutil.IsolateUserCache(t)
	data, err := os.ReadFile("../plugins/core/testdata/engines/echo.wasm")
	if err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, _ = w.Write(data)
	}))
	defer server.Close()
	session, err := newEditorSession(t.Context(), application.Options{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := session.close(t.Context()); err != nil {
			t.Error(err)
		}
	}()
	source := server.URL + "/engine.wasm"
	for _, suffix := range []string{"", "#sha256=bad"} {
		if _, err := session.options.LoadScriptEngine(t.Context(), source+suffix); err == nil || !strings.Contains(err.Error(), "sha256") {
			t.Fatalf("unpinned source accepted: %v", err)
		}
	}
	if requests.Load() != 0 {
		t.Fatal("invalid pin reached the network")
	}
	pinned := fmt.Sprintf("%s#sha256=%x", source, sha256.Sum256(data))
	engine, err := session.options.LoadScriptEngine(t.Context(), pinned)
	if err != nil {
		t.Fatal(err)
	}
	findings, err := engine.Check(t.Context(), analyzer.ScriptRequest{Source: "print(missing)", Filename: "step.py"})
	if err != nil || len(findings) != 1 || findings[0].Code != "F821" {
		t.Fatalf("pinned engine: %+v, %v", findings, err)
	}
	if _, err := session.options.LoadScriptEngine(t.Context(), pinned); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 1 {
		t.Fatalf("pinned snapshot downloaded %d times", requests.Load())
	}
}
