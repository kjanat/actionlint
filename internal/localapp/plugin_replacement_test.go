package localapp

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"actionlint.kjanat.dev/analyzer"
	"actionlint.kjanat.dev/internal/application"
	"actionlint.kjanat.dev/internal/config"
	"actionlint.kjanat.dev/internal/tools/adapters"
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
				selected.managed = map[string]string{path: "shellcheck"}
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
				t.Fatal("editor retained standalone managed analyzer")
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
				plugins := &loadedPlugins{engines: []scriptEngineRegistration{{backend: backend, descriptor: analyzer.ScriptEngineDescriptor{Engine: engine}}}}
				var adaptersOptions adapters.Options
				shell := "bash"
				if engine == "ruff" {
					shell = "python"
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
				workflow := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo\n        shell: " + shell + "\n"
				if _, err := application.Check(t.Context(), options, application.Input{Kind: application.Stdin, Reader: strings.NewReader(workflow)}); err != nil {
					t.Fatal(err)
				}
				want := int32(0)
				if enabled {
					want = 1
				}
				if backend.calls.Load() != want {
					t.Fatalf("backend calls = %d, want %d", backend.calls.Load(), want)
				}
			})
		}
	}
}
