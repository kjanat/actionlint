package analysis_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"actionlint.kjanat.dev/v2/analysis"
	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/config"
)

type scriptBackend func(context.Context, analyzer.ScriptRequest) ([]analyzer.ScriptDiagnostic, error)

func (f scriptBackend) Check(ctx context.Context, request analyzer.ScriptRequest) ([]analyzer.ScriptDiagnostic, error) {
	return f(ctx, request)
}

func TestScriptBackendsWithoutCommands(t *testing.T) {
	failure := errors.New("backend partially completed")
	var mu sync.Mutex
	seen := map[string]analyzer.ScriptRequest{}
	backend := scriptBackend(func(_ context.Context, request analyzer.ScriptRequest) ([]analyzer.ScriptDiagnostic, error) {
		mu.Lock()
		seen[request.Filename] = request
		mu.Unlock()
		if strings.Contains(request.Source, "${{") {
			t.Errorf("unmasked expression: %s", request.Source)
		}
		code := "F821"
		if request.Filename == "actionlint.sh" {
			code = "SC2086"
		}
		// Find the real script line after any host-provided shell setup/directives.
		row := 0
		for i, line := range strings.Split(request.Source, "\n") {
			if strings.Contains(line, "missing") {
				row = i + 1
				break
			}
		}
		return []analyzer.ScriptDiagnostic{{Filename: request.Filename, Code: code, Message: "test finding", Severity: "warning", Location: analyzer.ScriptPosition{Row: row, Column: 1}, EndLocation: analyzer.ScriptPosition{Row: row, Column: 2}}}, failure
	})
	cfg, err := config.Parse([]byte("tools:\n  ruff:\n    config:\n      target-version: py312\n      lint: {extend-select: [B]}\n"))
	if err != nil {
		t.Fatal(err)
	}
	result, err := analysis.Analyze(t.Context(), analysis.Request{
		Sources: []analysis.Source{{Path: "ci.yml", Config: cfg, Content: []byte(`on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - shell: python
        run: |
          print(missing, '${{ github.actor }}')
      - shell: bash
        run: |
          echo missing "${{ github.actor }}"
`)}},
		Options: analysis.Options{Ruff: analysis.Tool{Backend: backend}, ShellCheck: analysis.Tool{Backend: backend}},
	})
	if !errors.Is(err, failure) {
		t.Fatalf("partial error lost: %v", err)
	}
	var findings int
	for _, d := range result.Diagnostics {
		if d.Rule != "ruff" && d.Rule != "shellcheck" {
			continue
		}
		findings++
		if d.Severity != "warning" {
			t.Errorf("backend severity lost: %+v", d)
		}
		if d.Start.Line != 8 && d.Start.Line != 11 {
			t.Errorf("unmapped diagnostic: %+v", d)
		}
		if d.End.Column != d.Start.Column+1 {
			t.Errorf("range lost: %+v", d)
		}
	}
	if findings != 2 || len(seen) != 2 {
		t.Fatalf("findings=%d requests=%v result=%+v", findings, seen, result)
	}
	var options map[string]any
	if err := json.Unmarshal(seen["actionlint.py"].Config, &options); err != nil {
		t.Fatal(err)
	}
	if options["target-version"] != "py312" || options["lint"] == nil {
		t.Fatal(options)
	}
}

func TestBackendSelectionIsExclusive(t *testing.T) {
	backend := scriptBackend(func(context.Context, analyzer.ScriptRequest) ([]analyzer.ScriptDiagnostic, error) {
		t.Fatal("ambiguous selection ran")
		return nil, nil
	})
	for _, options := range []analysis.Options{
		{Ruff: analysis.Tool{Backend: backend, Process: &analysis.Process{Executable: "ruff"}}},
		{ShellCheck: analysis.Tool{Backend: backend, Process: &analysis.Process{Executable: "shellcheck"}}},
	} {
		if _, err := analysis.Analyze(t.Context(), analysis.Request{Options: options}); err == nil {
			t.Fatal("accepted ambiguous tool")
		}
		if _, err := analysis.Files(t.Context(), nil, analysis.FileOptions{Options: options}); err == nil {
			t.Fatal("accepted ambiguous file tool")
		}
	}
}
