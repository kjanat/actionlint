package localapp

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/tools/adapters"
	"actionlint.kjanat.dev/v2/rule"
)

type countedPluginBackend struct{ calls atomic.Int32 }

func (b *countedPluginBackend) Check(context.Context, analyzer.ScriptRequest) ([]analyzer.ScriptDiagnostic, error) {
	b.calls.Add(1)
	return nil, nil
}

func TestManagedDocumentPluginAndStandaloneSelection(t *testing.T) {
	const path = "../plugins/component/testdata/plugins/no-sudo.wasm"
	const workflow = "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: sudo echo test\n"
	for _, tc := range []struct {
		name, command, config, rule string
		managed                     bool
	}{
		{"managed", "shellcheck", "{}", "shellcheck", true},
		{"managed disabled", "", "{}", "", true},
		{"managed legacy suppression", "shellcheck", "lint: {rules: {external: {shellcheck: off}}}", "", true},
		{"standalone unaffected", "", "{}", "no-sudo", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			selected := PluginSources{Paths: []string{path}}
			if tc.managed {
				data, err := os.ReadFile("../plugins/component/testdata/plugins/managed-no-sudo.wasm")
				if err != nil {
					t.Fatal(err)
				}
				renamed := filepath.Join(t.TempDir(), "unrelated-fork.wasm")
				if err := os.WriteFile(renamed, data, 0o600); err != nil {
					t.Fatal(err)
				}
				selected.Paths = []string{renamed}
			}
			overlay, err := config.ParseOverlay("config", []byte(tc.config))
			if err != nil {
				t.Fatal(err)
			}
			options := application.Options{WorkingDir: t.TempDir(), ConfigOverlays: []config.Overlay{overlay}, Tools: adapters.Options{ShellCheck: tc.command}.Registrations()}
			result, err := CheckSources(t.Context(), options, application.Input{Kind: application.Stdin, Name: "ci.yml", Reader: strings.NewReader(workflow)}, selected)
			if err != nil {
				t.Fatal(err)
			}
			if tc.rule == "" {
				if len(result.Diagnostics) != 0 {
					t.Fatalf("disabled managed analyzer ran: %+v", result.Diagnostics)
				}
			} else if len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != tc.rule {
				t.Fatalf("wrong role/duplicate execution: %+v", result.Diagnostics)
			}
			session, err := newEditorSources(t.Context(), options, selected)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := session.close(context.WithoutCancel(t.Context())); err != nil {
					t.Error(err)
				}
			}()
			if tc.managed && len(session.options.Analyzers) != 0 {
				t.Fatal("editor kept standalone managed analyzer")
			}
			rules, err := RulesSources(t.Context(), selected)
			if err != nil {
				t.Fatal(err)
			}
			standalone := false
			for _, descriptor := range rules {
				standalone = standalone || descriptor.Name == "no-sudo"
			}
			if standalone == tc.managed {
				t.Fatal("rule listing disagrees with execution role")
			}
		})
	}
}

func TestPluginReplacementPreservesToolDisablement(t *testing.T) {
	for _, engine := range []string{"ruff", "shellcheck"} {
		for _, enabled := range []bool{false, true} {
			t.Run(engine+"/"+map[bool]string{false: "disabled", true: "enabled"}[enabled], func(t *testing.T) {
				backend := &countedPluginBackend{}
				shell := "bash"
				if engine == "ruff" {
					shell = "python"
				}
				plugins := &loadedPlugins{engines: []scriptEngineRegistration{{backend: backend, descriptor: analyzer.ScriptEngineDescriptor{
					Engine: engine, Rule: rule.Descriptor{Name: engine},
					Shells:       []string{shell},
					ScriptInputs: []analyzer.ScriptInput{{Action: "example/execute", Input: "code", Language: "javascript"}},
				}}}}
				var adaptersOptions adapters.Options
				if engine == "ruff" {
					if enabled {
						adaptersOptions.Ruff = "ruff"
					}
				} else if enabled {
					adaptersOptions.ShellCheck = "shellcheck"
				}
				options, err := plugins.configure(application.Options{WorkingDir: t.TempDir(), Tools: adaptersOptions.Registrations()})
				if err != nil {
					t.Fatal(err)
				}
				needed, err := application.RequiredTools(t.Context(), options, nil)
				if err != nil || needed[engine] {
					t.Fatalf("plugin replacement requested native tool: %v %v", needed, err)
				}
				workflow := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo\n        shell: " + shell + "\n      - uses: example/execute@v1\n        with:\n          code: input_value\n"
				if _, err := application.Check(t.Context(), options, application.Input{Kind: application.Stdin, Reader: strings.NewReader(workflow)}); err != nil {
					t.Fatal(err)
				}
				want := int32(0)
				if enabled {
					want = 2
				}
				if backend.calls.Load() != want {
					t.Fatalf("backend calls = %d, want %d", backend.calls.Load(), want)
				}
			})
		}
	}
}

func TestManagedDocumentPluginRetainsConfiguredInputRoutes(t *testing.T) {
	plugin, err := filepath.Abs("../plugins/component/testdata/plugins/managed-no-sudo.wasm")
	if err != nil {
		t.Fatal(err)
	}
	for _, entrypoint := range []string{"check", "editor"} {
		for _, runs := range []bool{false, true} {
			name := "input-only"
			if runs {
				name = "runs-and-inputs"
			}
			t.Run(entrypoint+"/"+name, func(t *testing.T) {
				root := t.TempDir()
				for name, content := range map[string]string{
					".git/HEAD":               "ref: refs/heads/master\n",
					".github/actionlint.yaml": "overrides:\n  - includes: [nested/**]\n    tools:\n      shellcheck:\n        backend:\n          wasm: {path: nested.wasm}\n",
					"workflow.yml":            "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: sudo echo default\n      - uses: ./nested\n",
					"nested/action.yml":       "name: nested\ndescription: fixture\nruns:\n  using: composite\n  steps:\n    - run: echo nested_run\n      shell: bash\n    - uses: example/execute@v1\n      with:\n        code: echo nested_input\n",
				} {
					path := filepath.Join(root, filepath.FromSlash(name))
					if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				engine := &selectedInputBackend{descriptor: analyzer.ScriptEngineDescriptor{
					Engine: "shellcheck", Rule: rule.Descriptor{Name: "shellcheck"},
					ScriptInputs: []analyzer.ScriptInput{{Action: "example/execute", Input: "code", Language: "bash"}},
				}}
				if runs {
					engine.descriptor.Shells = []string{"bash"}
				}
				options := application.Options{WorkingDir: root, ConfigFile: filepath.Join(root, ".github/actionlint.yaml"), Tools: adapters.Options{ShellCheck: "shellcheck"}.Registrations(), LoadScriptEngine: func(context.Context, string) (analyzer.ScriptBackend, error) {
					return engine, nil
				}}
				sources := PluginSources{Paths: []string{plugin}}
				input := application.Input{Kind: application.Files, Paths: []string{filepath.Join(root, "workflow.yml")}}
				var result *application.Result
				if entrypoint == "editor" {
					session, openErr := newEditorSources(t.Context(), options, sources)
					if openErr != nil {
						t.Fatal(openErr)
					}
					defer func() {
						if err := session.close(context.WithoutCancel(t.Context())); err != nil {
							t.Error(err)
						}
					}()
					result, err = application.Check(t.Context(), session.options, input)
				} else {
					result, err = CheckSources(t.Context(), options, input, sources)
				}
				if err != nil {
					t.Fatal(err)
				}
				if len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != "shellcheck" || filepath.Base(result.Diagnostics[0].Path) != "workflow.yml" {
					t.Fatalf("managed default analyzer changed: %+v", result.Diagnostics)
				}
				var actual []string
				for _, request := range engine.requests {
					actual = append(actual, strings.TrimSpace(request.Source))
				}
				slices.Sort(actual)
				want := []string{"echo nested_input"}
				if runs {
					want = append(want, "# shellcheck external-sources=false\nset -e -o pipefail\necho nested_run")
					slices.Sort(want)
				}
				if !slices.Equal(actual, want) {
					t.Fatalf("configured composite routes = %q, want %q", actual, want)
				}
			})
		}
	}
}
