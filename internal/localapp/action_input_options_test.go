package localapp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/processconfig"
	"actionlint.kjanat.dev/v2/internal/tools"
	"actionlint.kjanat.dev/v2/internal/tools/ruff"
	"actionlint.kjanat.dev/v2/internal/tools/shellcheck"
	"actionlint.kjanat.dev/v2/rule"
)

type inputOptionsBackend struct {
	mu        sync.Mutex
	name      string
	requests  []analyzer.ScriptRequest
	validated map[string]int
}

func (b *inputOptionsBackend) ValidateConfig(_ context.Context, options json.RawMessage) error {
	var settings map[string]json.RawMessage
	if err := json.Unmarshal(options, &settings); err != nil {
		return err
	}
	key := "target-version"
	if b.name == "shellcheck" {
		key = "shell"
	}
	if len(settings[key]) == 0 {
		return fmt.Errorf("missing adapter option %s", key)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.validated[string(options)]++
	return nil
}

func (b *inputOptionsBackend) Check(_ context.Context, request analyzer.ScriptRequest) ([]analyzer.ScriptDiagnostic, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.requests = append(b.requests, request)
	if !strings.HasPrefix(request.Filename, "actionlint-input/") {
		return nil, nil
	}
	marker := "root_input"
	if strings.Contains(request.Source, "nested_input") {
		marker = "nested_input"
	}
	before, _, _ := strings.Cut(request.Source, marker)
	row := strings.Count(before, "\n") + 1
	return []analyzer.ScriptDiagnostic{{Filename: request.Filename, Code: "INPUT", Message: "input", Location: analyzer.ScriptPosition{Row: row, Column: 1}, EndLocation: analyzer.ScriptPosition{Row: row, Column: len(marker) + 1}}}, nil
}

func TestBuiltinInputRoutesPreserveEffectiveAdapterOptions(t *testing.T) {
	for _, tc := range []struct {
		name, shell, base, nested string
		registration              tools.Registration
	}{
		{"ruff", "python", "target-version: py312\n      select: [F]\n      config: {line-length: 99}", "target-version: py313\n          select: [E4]\n          config: {line-length: 120}", (ruff.Options{Command: "ruff", Process: &processconfig.Options{Optional: true}}).Registration()},
		{"shellcheck", "bash", "config: {shell: dash, disable: [SC2086], enable: [check-unassigned-uppercase]}", "config: {shell: bash, disable: [SC2046], enable: [check-unassigned-uppercase]}", (shellcheck.Options{Command: "shellcheck", Process: &processconfig.Options{Optional: true}}).Registration()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			files := map[string]string{
				".git/HEAD":               "ref: refs/heads/master\n",
				".github/actionlint.yaml": "tools:\n    " + tc.name + ":\n      " + tc.base + "\noverrides:\n  - includes: [nested/**]\n    tools:\n        " + tc.name + ":\n          " + tc.nested + "\n",
				"workflow.yml":            "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: example/execute@v1\n        with:\n          code: root_input\n      - run: root_run\n        shell: " + tc.shell + "\n      - uses: ./nested\n",
				"nested/action.yml":       "name: nested\ndescription: fixture\nruns:\n  using: composite\n  steps:\n    - uses: example/execute@v1\n      with:\n        code: nested_input\n    - run: nested_run\n      shell: " + tc.shell + "\n",
			}
			for name, content := range files {
				path := filepath.Join(root, filepath.FromSlash(name))
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			engine := &inputOptionsBackend{name: tc.name, validated: map[string]int{}}
			plugins := loadedPlugins{engines: []scriptEngineRegistration{{backend: engine, descriptor: analyzer.ScriptEngineDescriptor{
				Engine: tc.name, Rule: rule.Descriptor{Name: tc.name}, Shells: []string{tc.shell}, ScriptInputs: []analyzer.ScriptInput{{Action: "example/execute", Input: "code", Language: tc.shell}},
			}}}}
			options, err := plugins.configure(application.Options{WorkingDir: root, Tools: []tools.Registration{tc.registration}})
			if err != nil {
				t.Fatal(err)
			}
			result, err := application.Check(t.Context(), options, application.Input{Kind: application.Files, Paths: []string{filepath.Join(root, "workflow.yml")}})
			if err != nil {
				t.Fatal(err)
			}
			if len(engine.requests) != 4 {
				t.Fatalf("requests=%+v", engine.requests)
			}
			checked := map[string]int{}
			var inputOptions, nativeOptions [2]string
			for _, request := range engine.requests {
				checked[string(request.Config)]++
				index := 0
				if strings.Contains(request.Source, "nested_") {
					index = 1
				}
				if strings.HasPrefix(request.Filename, "actionlint-input/") {
					inputOptions[index] = string(request.Config)
					if tc.name == "shellcheck" {
						code := "SC2086"
						if index == 1 {
							code = "SC2046"
						}
						if !strings.Contains(request.Source, "disable="+code) || !strings.Contains(request.Source, "enable=check-unassigned-uppercase") {
							t.Fatalf("inline directives lost: %+v", request)
						}
					}
				} else {
					nativeOptions[index] = string(request.Config)
				}
			}
			if inputOptions != nativeOptions || inputOptions[0] == inputOptions[1] || !reflect.DeepEqual(checked, engine.validated) {
				t.Fatalf("input=%v native=%v checked=%v validated=%v", inputOptions, nativeOptions, checked, engine.validated)
			}
			want := map[string]int{"workflow.yml": 17, "nested/action.yml": 15}
			for _, finding := range result.Diagnostics {
				if finding.Code != "INPUT" {
					continue
				}
				path := finding.Path
				if filepath.IsAbs(path) {
					var err error
					path, err = filepath.Rel(root, path)
					if err != nil {
						t.Fatal(err)
					}
				}
				path = filepath.ToSlash(path)
				column, ok := want[path]
				if !ok || finding.Start.Line != 8 || finding.Start.Column != column || finding.End.Line != 8 {
					t.Fatalf("generated directive changed source coordinates: %+v", finding)
				}
				delete(want, path)
			}
			if len(want) != 0 {
				t.Fatalf("missing mapped input findings: %v", want)
			}
		})
	}
}
