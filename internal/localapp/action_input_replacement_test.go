package localapp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"actionlint.kjanat.dev/analyzer"
	"actionlint.kjanat.dev/internal/application"
	"actionlint.kjanat.dev/internal/tools"
	"actionlint.kjanat.dev/internal/tools/ruff"
	"actionlint.kjanat.dev/internal/tools/shellcheck"
	"actionlint.kjanat.dev/rule"
)

type inputReplacementBackend struct {
	mu       sync.Mutex
	requests []analyzer.ScriptRequest
}

func TestBuiltinInputReplacementHonorsCompositeDisable(t *testing.T) {
	for _, tc := range []struct {
		name         string
		registration tools.Registration
	}{
		{"ruff", (ruff.Options{}).Registration()},
		{"shellcheck", (shellcheck.Options{}).Registration()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			files := map[string]string{
				".git/HEAD":               "ref: refs/heads/master\n",
				".github/actionlint.yaml": "overrides:\n  - includes: [nested/**]\n    tools:\n      " + tc.name + ":\n        enabled: false\n  - includes: [other/**]\n    tools:\n      " + tc.name + ":\n        enabled: true\n",
				"workflow.yml":            "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: example/execute@v1\n        with:\n          code: enabled_input\n      - uses: ./nested\n",
				"nested/action.yml":       "name: nested\ndescription: fixture\nruns:\n  using: composite\n  steps:\n    - uses: example/execute@v1\n      with:\n        code: disabled_input\n",
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
			engine := &inputReplacementBackend{}
			plugins := loadedPlugins{engines: []scriptEngineRegistration{{backend: engine, descriptor: analyzer.ScriptEngineDescriptor{
				Engine: tc.name, Rule: rule.Descriptor{Name: tc.name}, ScriptInputs: []analyzer.ScriptInput{{Action: "example/execute", Input: "code", Language: "javascript"}},
			}}}}
			options, err := plugins.configure(application.Options{WorkingDir: root, Tools: []tools.Registration{tc.registration}})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := application.Check(t.Context(), options, application.Input{Kind: application.Files, Paths: []string{filepath.Join(root, "workflow.yml")}}); err != nil {
				t.Fatal(err)
			}
			engine.mu.Lock()
			defer engine.mu.Unlock()
			if len(engine.requests) != 1 || engine.requests[0].Source != "enabled_input" {
				t.Fatalf("disabled composite reached replacement backend: %+v", engine.requests)
			}
		})
	}
}

func (b *inputReplacementBackend) Check(_ context.Context, request analyzer.ScriptRequest) ([]analyzer.ScriptDiagnostic, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.requests = append(b.requests, request)
	return nil, nil
}

func TestBuiltinReplacementPreservesActionInputRouting(t *testing.T) {
	for _, tc := range []struct {
		name, shell, source, filename string
		registration                  tools.Registration
	}{
		{"ruff", "python", "print('${{ github.repository }}')", "actionlint.py", (ruff.Options{}).Registration()},
		{"shellcheck", "bash", "echo '${{ github.repository }}'", "actionlint.sh", (shellcheck.Options{}).Registration()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			backend := &inputReplacementBackend{}
			site := analyzer.ScriptInput{Action: "example/execute", Input: "code", Language: "javascript"}
			plugins := loadedPlugins{engines: []scriptEngineRegistration{{backend: backend, descriptor: analyzer.ScriptEngineDescriptor{
				Engine: tc.name, Rule: rule.Descriptor{Name: tc.name}, Shells: []string{tc.shell}, ScriptInputs: []analyzer.ScriptInput{site},
			}}}}
			options, err := plugins.configure(application.Options{WorkingDir: t.TempDir(), Tools: []tools.Registration{tc.registration}})
			if err != nil {
				t.Fatal(err)
			}
			if len(options.Tools) != 1 {
				t.Fatalf("replacement duplicated the tool registration: %+v", options.Tools)
			}
			workflow := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: example/execute@v1\n        with:\n          code: input_value\n      - run: " + tc.source + "\n        shell: " + tc.shell + "\n      - uses: example/other@v1\n        with:\n          code: unrelated_input\n      - run: unrelated_run\n        shell: custom {0}\n"
			_, err = application.Check(t.Context(), options, application.Input{Kind: application.Stdin, Reader: strings.NewReader(workflow)})
			if err != nil {
				t.Fatal(err)
			}
			backend.mu.Lock()
			defer backend.mu.Unlock()
			if len(backend.requests) != 2 {
				t.Fatalf("want one native run and one advertised input request, got %+v", backend.requests)
			}
			seen := map[string]bool{}
			for _, request := range backend.requests {
				if seen[request.Filename] || strings.Contains(request.Source, "${{") {
					t.Fatalf("duplicate route or unmasked expression: %+v", request)
				}
				seen[request.Filename] = true
				switch request.Filename {
				case tc.filename:
					if !strings.Contains(request.Source, "_") || len(request.Config) == 0 {
						t.Fatalf("native preprocessing/configuration lost: %+v", request)
					}
				case "actionlint-script":
					if request.Source != "input_value" {
						t.Fatalf("input was treated as a shell script: %+v", request)
					}
				default:
					t.Fatalf("unexpected route: %+v", request)
				}
			}
		})
	}
}
