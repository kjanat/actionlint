package checking_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/checking/scriptanalysis/registry"
	"actionlint.kjanat.dev/v2/internal/input/checkrequest"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
	"actionlint.kjanat.dev/v2/internal/integrations/hostprocess"
	"actionlint.kjanat.dev/v2/internal/model/scriptengine"
)

type scriptBackend func(context.Context, scriptengine.Request) ([]scriptengine.Diagnostic, error)

func (f scriptBackend) Check(ctx context.Context, request scriptengine.Request) ([]scriptengine.Diagnostic, error) {
	return f(ctx, request)
}

func (scriptBackend) TemplateContract() string { return scriptengine.TemplateEffectsContract }

func TestScriptBackendsWithoutCommands(t *testing.T) {
	t.Parallel()
	failure := errors.New("backend partially completed")
	var mu sync.Mutex
	seen := map[string]scriptengine.Request{}
	backend := scriptBackend(func(_ context.Context, request scriptengine.Request) ([]scriptengine.Diagnostic, error) {
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
		return []scriptengine.Diagnostic{{Filename: request.Filename, Code: code, Message: "test finding", Severity: "warning", Location: sourcetext.Position{Line: row, Column: 1}, EndLocation: sourcetext.Position{Line: row, Column: 2}, TemplateEffect: scriptengine.TemplateEffectNone}}, failure
	})
	cfg, err := checking.ConfigurationRules().ParseConfig([]byte("tools:\n  ruff:\n    config:\n      target-version: py312\n      lint: {extend-select: [B]}\n"))
	if err != nil {
		t.Fatal(err)
	}
	result, err := checking.Analyze(t.Context(),

		checking.Options{Tools: registry.Registrations(registry.ToolSelections{"ruff": {Backend: backend}, "shellcheck": {Backend: backend}})}, []workspace.InputFile{{Path: "ci.yml", Config: cfg, Content: []byte(`on: push
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
	)
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
	t.Parallel()
	backend := scriptBackend(func(context.Context, scriptengine.Request) ([]scriptengine.Diagnostic, error) {
		t.Fatal("ambiguous selection ran")
		return nil, nil
	})
	for _, options := range []checking.Options{
		{Tools: registry.Registrations(registry.ToolSelections{"ruff": {Backend: backend, Process: &hostprocess.CommandOptions{Executable: new("ruff")}}})},
		{Tools: registry.Registrations(registry.ToolSelections{"shellcheck": {Backend: backend, Process: &hostprocess.CommandOptions{Executable: new("shellcheck")}}})},
	} {
		if _, err := checking.Analyze(t.Context(), options, nil); err == nil {
			t.Fatal("accepted ambiguous tool")
		}
		if _, err := checking.Check(t.Context(), options, checkrequest.Request{Kind: checkrequest.Files, Paths: nil}); err == nil {
			t.Fatal("accepted ambiguous file tool")
		}
	}
}

func TestUnknownBundledProviderIsRejected(t *testing.T) {
	t.Parallel()
	if !slices.Equal(registry.IDs(), []string{"ruff", "shellcheck"}) {
		t.Fatalf("bundled tools: %v", registry.IDs())
	}
	defer func() {
		failure := recover()
		err, ok := failure.(error)
		if !ok || !strings.Contains(err.Error(), "no bundled tool provider is named missing") {
			t.Fatalf("unknown tool rejection: %v", failure)
		}
	}()
	registry.Registrations(registry.ToolSelections{"missing": {}})
	t.Fatal("unknown bundled provider accepted")
}
