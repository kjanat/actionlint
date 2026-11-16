package checking_test

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

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/input/checkrequest"
	"actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginexecution"

	"actionlint.kjanat.dev/v2/internal/model/checkinputs"
	"actionlint.kjanat.dev/v2/internal/plugins/artifacts"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginacquisition"
)

func TestComponentEngineMapsCarriageReturnEscape(t *testing.T) {
	t.Parallel()
	module, err := filepath.Abs("../../../../plugins/execution/component/testdata/plugins/script-engine.wasm")
	if err != nil {
		t.Fatal(err)
	}
	input := "on: push\njobs:\n  check:\n    runs-on: ubuntu-latest\n    steps:\n      - run: \"print(0)\\rmissing\"\n        shell: python\npermissions: {}\n"
	result, err := checking.CheckWithPluginPaths(t.Context(), checking.Options{}, checkrequest.Request{Kind: checkrequest.Stdin, Reader: strings.NewReader(input)}, []string{module}, pluginacquisition.Settings{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Start != (source.Position{Line: 6, Column: 25}) ||
		result.Diagnostics[0].End != (source.Position{Line: 6, Column: 32}) {
		t.Fatalf("escaped CR lost token coordinates: %+v", result.Diagnostics)
	}
}

func TestComponentEnginePluginRegistersAdvertisedShells(t *testing.T) {
	t.Parallel()
	module, err := filepath.Abs("../../../../plugins/execution/component/testdata/plugins/script-engine.wasm")
	if err != nil {
		t.Fatal(err)
	}
	input := "on: push\njobs:\n  check:\n    runs-on: ubuntu-latest\n    steps:\n      - run: print(missing)\n        shell: python\n      - run: echo missing\n        shell: bash\npermissions: {}\n"
	result, err := checking.CheckWithPluginPaths(t.Context(), checking.Options{}, checkrequest.Request{Kind: checkrequest.Stdin, Reader: strings.NewReader(input)}, []string{module}, pluginacquisition.Settings{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != "echo" || result.Diagnostics[0].Start.Line != 6 || result.Diagnostics[0].Start.Column != 20 {
		t.Fatalf("component engine: %+v", result.Diagnostics)
	}
	descriptors, err := checking.RulesWithPluginPaths(t.Context(), []string{module}, pluginacquisition.Settings{})
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
	_, err = checking.CheckWithPluginPaths(t.Context(), checking.Options{}, checkrequest.Request{Kind: checkrequest.Stdin, Reader: strings.NewReader(input)}, []string{module, "../../../../plugins/execution/module/testdata/engines/echo.wasm"}, pluginacquisition.Settings{})
	if err == nil || !strings.Contains(err.Error(), "multiple plugins provide script engine") {
		t.Fatalf("duplicate core/component engine accepted: %v", err)
	}
	loaded, err := pluginexecution.ScriptEngineLoader(pluginacquisition.Settings{}, checking.BuiltinRuleNames(), io.Discard, false)(t.Context(), module)
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
	findings, err := loaded.Check(t.Context(), checkinputs.ScriptRequest{Source: "missing", Filename: "script.py"})
	if err != nil || len(findings) != 1 || findings[0].Code != "ECHO001" {
		t.Fatalf("configured component backend: %#v, %v", findings, err)
	}
	if _, err := pluginexecution.ScriptEngineLoader(pluginacquisition.Settings{}, checking.BuiltinRuleNames(), io.Discard, false)(t.Context(), "../../../../plugins/execution/component/testdata/plugins/no-sudo.wasm"); err == nil {
		t.Fatal("analyzer-only component accepted as configured script engine")
	}
}

func TestCoreEnginePluginRegistersAdvertisedShells(t *testing.T) {
	t.Parallel()
	acquisition := pluginacquisition.Settings{UserCacheDir: t.TempDir()}
	module, err := filepath.Abs("../../../../plugins/execution/module/testdata/engines/echo.wasm")
	if err != nil {
		t.Fatal(err)
	}
	input := "on: push\njobs:\n  check:\n    runs-on: ubuntu-latest\n    steps:\n      - run: print(missing)\n        shell: python\n      - run: echo missing\n        shell: bash\npermissions: {}\n"
	result, err := checking.CheckWithPluginPaths(t.Context(), checking.Options{}, checkrequest.Request{Kind: checkrequest.Stdin, Reader: strings.NewReader(input)}, []string{module}, acquisition)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != "echo" || result.Diagnostics[0].Start.Line != 6 || result.Diagnostics[0].Start.Column != 20 {
		t.Fatalf("metadata-selected engine: %+v", result.Diagnostics)
	}
	descriptors, err := checking.RulesWithPluginPaths(t.Context(), []string{module}, acquisition)
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
	remote, err := checking.CheckWithPluginPaths(t.Context(), checking.Options{}, checkrequest.Request{Kind: checkrequest.Stdin, Reader: strings.NewReader(input)}, []string{uri}, acquisition)
	if err != nil {
		t.Fatal(err)
	}
	if len(remote.Diagnostics) != 1 || remote.Diagnostics[0].Rule != "echo" || remote.Diagnostics[0].Start != result.Diagnostics[0].Start {
		t.Fatalf("remote engine: %+v", remote.Diagnostics)
	}
	_, err = checking.CheckWithPluginPaths(t.Context(), checking.Options{}, checkrequest.Request{Kind: checkrequest.Stdin, Reader: strings.NewReader(input)}, []string{module, module}, acquisition)
	if err == nil {
		t.Fatal("accepted duplicate engine")
	}
}

func TestModuleKindSelection(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		path string
		core bool
	}{{"../../../../plugins/execution/module/testdata/engines/echo.wasm", true}, {"../../../../plugins/execution/component/testdata/plugins/no-sudo.wasm", false}} {
		artifact, err := artifacts.Resolve(t.Context(), tc.path, artifacts.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if core := artifact.Info.Kind == artifacts.Core; core != tc.core {
			t.Fatalf("%s: core=%v", tc.path, core)
		}
	}
	path := filepath.Join(t.TempDir(), "bad.wasm")
	if err := os.WriteFile(path, []byte("not wasm"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := artifacts.Resolve(context.Background(), path, artifacts.Options{}); err == nil {
		t.Fatal("accepted invalid header")
	}
}
