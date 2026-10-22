package ruff

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
)

type testBackend func(context.Context, analyzer.ScriptRequest) ([]analyzer.ScriptDiagnostic, error)

func (f testBackend) Check(ctx context.Context, request analyzer.ScriptRequest) ([]analyzer.ScriptDiagnostic, error) {
	return f(ctx, request)
}

func TestBackendConfigIsolationAndCancellation(t *testing.T) {
	cfg := Config{Native: map[string]any{"lint": map[string]any{"select": []any{"F"}}}, Ignore: []string{"F401"}}
	options, err := engineOptions(cfg)
	if err != nil {
		t.Fatal(err)
	}
	options["lint"].(map[string]any)["select"] = []any{"ALL"}
	if _, exists := cfg.Native["lint"].(map[string]any)["ignore"]; exists {
		t.Fatal("modified shared config")
	}
	original, _ := json.Marshal(cfg.Native)
	if strings.Contains(string(original), "ALL") {
		t.Fatal("nested map shared")
	}
	ctx, cancel := context.WithCancel(t.Context())
	checker := NewBackend(ctx, testBackend(func(context.Context, analyzer.ScriptRequest) ([]analyzer.ScriptDiagnostic, error) {
		t.Fatal("cancelled backend started")
		return nil, nil
	}), nil)
	python := "python"
	if err := checker.Check("print(missing)", &python, "test", Config{}, func(Diagnostic) { t.Fatal("reported cancelled result") }); err != nil {
		t.Fatal(err)
	}
	cancel()
	if !errors.Is(checker.Wait(), context.Canceled) {
		t.Fatal("cancellation lost")
	}
}

func TestBackendRejectsForeignDiagnosticsAtomically(t *testing.T) {
	backend := testBackend(func(context.Context, analyzer.ScriptRequest) ([]analyzer.ScriptDiagnostic, error) {
		valid := Diagnostic{Filename: stdinFilename, Code: "F821", Message: "undefined", Location: Position{Row: 1, Column: 1}}
		invalid := valid
		invalid.Filename = "other.py"
		return []Diagnostic{valid, invalid}, nil
	})
	checker := NewBackend(t.Context(), backend, nil)
	python := "python"
	if err := checker.Check("x", &python, "test", Config{}, func(Diagnostic) { t.Fatal("published unchecked batch") }); err != nil {
		t.Fatal(err)
	}
	if err := checker.Wait(); err == nil {
		t.Fatal("foreign filename accepted")
	}
}
