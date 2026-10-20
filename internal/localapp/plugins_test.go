package localapp

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/internal/application"
	pluginartifact "actionlint.kjanat.dev/v2/internal/plugins/artifact"
)

func TestComponentEnginePluginRegistersAdvertisedShells(t *testing.T) {
	module, err := filepath.Abs("../plugins/component/testdata/plugins/script-engine.wasm")
	if err != nil {
		t.Fatal(err)
	}
	input := "on: push\njobs:\n  check:\n    runs-on: ubuntu-latest\n    steps:\n      - run: print(missing)\n        shell: python\n      - run: echo missing\n        shell: bash\n"
	result, err := Check(t.Context(), application.Options{}, application.Input{Kind: application.Stdin, Reader: strings.NewReader(input)}, []string{module})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != "echo" || result.Diagnostics[0].Start.Line != 6 || result.Diagnostics[0].Start.Column != 20 {
		t.Fatalf("component engine: %+v", result.Diagnostics)
	}
	descriptors, err := Rules(t.Context(), []string{module})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, descriptor := range descriptors {
		if descriptor.Name == "echo" && descriptor.Provider == "echo-component" {
			found = true
		}
	}
	if !found {
		t.Fatal("component engine missing from rule catalogue")
	}
	_, err = Check(t.Context(), application.Options{}, application.Input{Kind: application.Stdin, Reader: strings.NewReader(input)}, []string{module, "../plugins/core/testdata/engines/echo.wasm"})
	if err == nil || !strings.Contains(err.Error(), "multiple plugins provide script engine") {
		t.Fatalf("duplicate core/component engine accepted: %v", err)
	}
	loaded, err := scriptEngineLoader(io.Discard, false)(t.Context(), module)
	if err != nil {
		t.Fatal(err)
	}
	closer, ok := loaded.(interface{ Close(context.Context) error })
	if !ok {
		t.Fatal("component engine has no lifecycle")
	}
	defer func() {
		if err := closer.Close(context.Background()); err != nil {
			t.Error(err)
		}
	}()
	findings, err := loaded.Check(t.Context(), analyzer.ScriptRequest{Source: "missing", Filename: "script.py"})
	if err != nil || len(findings) != 1 || findings[0].Code != "ECHO001" {
		t.Fatalf("configured component backend: %#v, %v", findings, err)
	}
	if _, err := scriptEngineLoader(io.Discard, false)(t.Context(), "../plugins/component/testdata/plugins/no-sudo.wasm"); err == nil {
		t.Fatal("analyzer-only component accepted as configured script engine")
	}
}

func TestCoreEnginePluginRegistersAdvertisedShells(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	t.Setenv("LocalAppData", cache)
	t.Setenv("HOME", cache)
	module, err := filepath.Abs("../plugins/core/testdata/engines/echo.wasm")
	if err != nil {
		t.Fatal(err)
	}
	input := "on: push\njobs:\n  check:\n    runs-on: ubuntu-latest\n    steps:\n      - run: print(missing)\n        shell: python\n      - run: echo missing\n        shell: bash\n"
	result, err := Check(t.Context(), application.Options{}, application.Input{Kind: application.Stdin, Reader: strings.NewReader(input)}, []string{module})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != "echo" || result.Diagnostics[0].Start.Line != 6 || result.Diagnostics[0].Start.Column != 20 {
		t.Fatalf("metadata-selected engine: %+v", result.Diagnostics)
	}
	descriptors, err := Rules(t.Context(), []string{module})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, descriptor := range descriptors {
		if descriptor.Name == "echo" {
			found = true
		}
	}
	if !found {
		t.Fatal("script engine missing from catalogue")
	}
	data, err := os.ReadFile(module)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(data) }))
	defer server.Close()
	uri := fmt.Sprintf("%s/engine.wasm#sha256=%x", server.URL, sha256.Sum256(data))
	remote, err := Check(t.Context(), application.Options{}, application.Input{Kind: application.Stdin, Reader: strings.NewReader(input)}, []string{uri})
	if err != nil {
		t.Fatal(err)
	}
	if len(remote.Diagnostics) != 1 || remote.Diagnostics[0].Rule != "echo" || remote.Diagnostics[0].Start != result.Diagnostics[0].Start {
		t.Fatalf("remote engine: %+v", remote.Diagnostics)
	}
	_, err = Check(t.Context(), application.Options{}, application.Input{Kind: application.Stdin, Reader: strings.NewReader(input)}, []string{module, module})
	if err == nil {
		t.Fatal("accepted duplicate engine")
	}
}

func TestModuleKindSelection(t *testing.T) {
	for _, tc := range []struct {
		path string
		core bool
	}{{"../plugins/core/testdata/engines/echo.wasm", true}, {"../plugins/component/testdata/plugins/no-sudo.wasm", false}} {
		artifact, err := pluginartifact.Resolve(t.Context(), tc.path, pluginartifact.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if core := artifact.Info.Kind == pluginartifact.Core; core != tc.core {
			t.Fatalf("%s: core=%v", tc.path, core)
		}
	}
	path := filepath.Join(t.TempDir(), "bad.wasm")
	if err := os.WriteFile(path, []byte("not wasm"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := pluginartifact.Resolve(context.Background(), path, pluginartifact.Options{}); err == nil {
		t.Fatal("accepted invalid header")
	}
}
