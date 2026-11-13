package lsp

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
	"actionlint.kjanat.dev/v2/internal/checking/scripts/registry"
	inputmodel "actionlint.kjanat.dev/v2/internal/input"
	config "actionlint.kjanat.dev/v2/internal/input/configuration"
	pluginexec "actionlint.kjanat.dev/v2/internal/plugins/execution"
	plugininstall "actionlint.kjanat.dev/v2/internal/plugins/installation"
)

type countedPluginBackend struct{ calls atomic.Int32 }

func (b *countedPluginBackend) Check(context.Context, checkinputs.ScriptRequest) ([]checkinputs.ScriptDiagnostic, error) {
	b.calls.Add(1)
	return nil, nil
}

func TestManagedDocumentPluginAndStandaloneSelection(t *testing.T) {
	t.Parallel()
	const path = "../plugins/execution/component/testdata/plugins/no-sudo.wasm"
	const workflow = "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: sudo echo test\npermissions: {}\n"
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
			t.Parallel()
			selected := plugininstall.Sources{Paths: []string{path}}
			if tc.managed {
				data, err := os.ReadFile("../plugins/execution/component/testdata/plugins/managed-no-sudo.wasm")
				if err != nil {
					t.Fatal(err)
				}
				renamed := filepath.Join(t.TempDir(), "unrelated-fork.wasm")
				if err := os.WriteFile(renamed, data, 0o600); err != nil {
					t.Fatal(err)
				}
				selected.Paths = []string{renamed}
			}
			overlay, err := checking.ConfigurationRules().ParseOverlay("config", []byte(tc.config))
			if err != nil {
				t.Fatal(err)
			}
			options := checking.Options{WorkingDir: t.TempDir(), ConfigOverlays: []config.Overlay{overlay}, Tools: registry.Registrations(registry.Selections{"shellcheck": {Command: tc.command}})}
			result, err := checking.CheckWithPlugins(t.Context(), options, inputmodel.Request{Kind: inputmodel.Stdin, Name: "ci.yml", Reader: strings.NewReader(workflow)}, selected)
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
			rules, err := checking.RulesWithPlugins(t.Context(), selected)
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
	t.Parallel()
	for _, engine := range []string{"ruff", "shellcheck"} {
		for _, enabled := range []bool{false, true} {
			t.Run(engine+"/"+map[bool]string{false: "disabled", true: "enabled"}[enabled], func(t *testing.T) {
				t.Parallel()
				backend := &countedPluginBackend{}
				shell := "bash"
				if engine == "ruff" {
					shell = "python"
				}
				plugins := &pluginexec.Loaded{ScriptEngines: []pluginexec.ScriptEngineRegistration{{Backend: backend, Descriptor: checkinputs.ScriptEngineDescriptor{
					Engine: engine, Rule: rule.Descriptor{Name: engine},
					Shells:       []string{shell},
					ScriptInputs: []checkinputs.ScriptInput{{Action: "example/execute", Input: "code", Language: "javascript"}},
				}}}}
				selections := registry.Selections{}
				if enabled {
					selections = registry.Selections{engine: {Command: engine}}
				}
				options, err := checking.ConfigurePlugins(plugins, t.Context(), checking.Options{WorkingDir: t.TempDir(), Tools: registry.Registrations(selections)})
				if err != nil {
					t.Fatal(err)
				}
				needed, err := checking.RequiredTools(t.Context(), options, nil)
				if err != nil || needed[engine] {
					t.Fatalf("plugin replacement requested native tool: %v %v", needed, err)
				}
				workflow := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo\n        shell: " + shell + "\n      - uses: example/execute@v1\n        with:\n          code: input_value\n" + "\npermissions: {}\n"
				if _, err := checking.Check(t.Context(), options, inputmodel.Request{Kind: inputmodel.Stdin, Reader: strings.NewReader(workflow)}); err != nil {
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
	t.Setenv("PATH", t.TempDir())
	plugin, err := filepath.Abs("../plugins/execution/component/testdata/plugins/managed-no-sudo.wasm")
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
				t.Parallel()
				root := t.TempDir()
				for name, content := range map[string]string{
					".git/HEAD":               "ref: refs/heads/master\n",
					".github/actionlint.yaml": "overrides:\n  - includes: [nested/**]\n    tools:\n      shellcheck:\n        backend:\n          wasm: {path: nested.wasm}\n",
					"workflow.yml":            "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: sudo echo default\n      - uses: ./nested\npermissions: {}\n",
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
				engine := &selectedInputBackend{descriptor: checkinputs.ScriptEngineDescriptor{
					Engine: "shellcheck", Rule: rule.Descriptor{Name: "shellcheck"},
					ScriptInputs: []checkinputs.ScriptInput{{Action: "example/execute", Input: "code", Language: "bash"}},
				}}
				if runs {
					engine.descriptor.Shells = []string{"bash"}
				}
				options := checking.Options{WorkingDir: root, ConfigFile: filepath.Join(root, ".github/actionlint.yaml"), Tools: registry.Registrations(registry.Selections{"shellcheck": {Command: "shellcheck"}}), LoadScriptEngine: func(context.Context, string) (checkinputs.ScriptBackend, error) {
					return engine, nil
				}}
				sources := plugininstall.Sources{Paths: []string{plugin}}
				input := inputmodel.Request{Kind: inputmodel.Files, Paths: []string{filepath.Join(root, "workflow.yml")}}
				var result *checking.Result
				var err error
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
					result, err = checking.Check(t.Context(), session.options, input)
				} else {
					result, err = checking.CheckWithPlugins(t.Context(), options, input, sources)
				}
				if err != nil {
					t.Fatal(err)
				}
				if len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != "shellcheck" || filepath.Base(result.Diagnostics[0].Path) != "workflow.yml" {
					t.Fatalf("managed default analyzer changed: %+v", result.Diagnostics)
				}
				var actual []string
				for _, request := range engine.requests {
					source := strings.TrimSpace(request.Source)
					body := source[strings.LastIndex(source, "\n")+1:]
					if !strings.Contains(source, "export CI GITHUB_ACTION GITHUB_ACTIONS GITHUB_ACTION_PATH ") {
						t.Fatalf("composite script lacks runtime environment declarations: %q", source)
					}
					if body == "echo nested_run" && !strings.HasPrefix(source, "# shellcheck external-sources=false\nset -e -o pipefail; export ") {
						t.Fatalf("composite run lacks interpreter setup: %q", source)
					}
					actual = append(actual, body)
				}
				slices.Sort(actual)
				want := []string{"echo nested_input"}
				if runs {
					want = append(want, "echo nested_run")
					slices.Sort(want)
				}
				if !slices.Equal(actual, want) {
					t.Fatalf("configured composite routes = %q, want %q", actual, want)
				}
			})
		}
	}
}
