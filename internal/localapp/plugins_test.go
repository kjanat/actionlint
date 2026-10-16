package localapp

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/internal/application"
	"actionlint.kjanat.dev/internal/pluginartifact"
)

func TestCoreEnginePluginUsesHostRuffAdapter(t *testing.T) {
	module, err := filepath.Abs("../corewasm/testdata/engines/echo.wasm")
	if err != nil {
		t.Fatal(err)
	}
	// The fixture advertises a non-Ruff engine, so it cannot silently take over a
	// different tool's configuration and source mapping.
	_, err = Check(t.Context(), application.Options{}, application.Input{Kind: application.Stdin, Reader: bytes.NewBufferString("on: push\njobs: {}\n")}, []string{module})
	if err == nil {
		t.Fatal("accepted unknown engine provider")
	}
}
func TestModuleKindSelection(t *testing.T) {
	for _, tc := range []struct {
		path string
		core bool
	}{{"../corewasm/testdata/engines/echo.wasm", true}, {"../component/testdata/plugins/no-sudo.wasm", false}} {
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
