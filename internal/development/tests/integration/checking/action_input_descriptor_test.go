package checking_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/checking/scriptanalysis/backends"
	"actionlint.kjanat.dev/v2/internal/checking/scriptanalysis/contract"
	"actionlint.kjanat.dev/v2/internal/checking/scriptanalysis/registry"
	"actionlint.kjanat.dev/v2/internal/input/checkrequest"
	"actionlint.kjanat.dev/v2/internal/integrations/dependencycache"
	"actionlint.kjanat.dev/v2/internal/integrations/hostprocess"
	"actionlint.kjanat.dev/v2/internal/model/checkinputs"
	"actionlint.kjanat.dev/v2/internal/model/rule"
	"actionlint.kjanat.dev/v2/internal/model/scriptengine"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginexecution"

	"actionlint.kjanat.dev/v2/internal/assembly"
)

// providerRegistration returns the registration of the bundled provider
// named name, run as selection says.
func providerRegistration(t *testing.T, name string, selection backends.ToolSelection) contract.Registration {
	t.Helper()
	for _, registration := range registry.Registrations(registry.ToolSelections{name: selection}) {
		if registration.Name == name {
			return registration
		}
	}
	t.Fatalf("no bundled provider %s", name)
	return contract.Registration{}
}

func TestBuiltinInputOnlyDescriptorRejectsNativeRuns(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, shell  string
		registration contract.Registration
	}{
		{"ruff", "python", providerRegistration(t, "ruff", backends.ToolSelection{Command: "ruff", Process: &hostprocess.CommandOptions{Optional: true}})},
		{"shellcheck", "bash", providerRegistration(t, "shellcheck", backends.ToolSelection{Command: "shellcheck", Process: &hostprocess.CommandOptions{Optional: true}})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			engine := &inputReplacementBackend{}
			plugins := pluginexecution.Loaded{ScriptEngines: []pluginexecution.ScriptEngineRegistration{{Backend: engine, Descriptor: scriptengine.Descriptor{Engine: tc.name, Rule: rule.Descriptor{Name: tc.name}, ScriptInputs: []checkinputs.ScriptInput{{Action: "example/execute", Input: "code", Language: tc.shell}}}}}}
			options, err := assembly.ConfigurePlugins(&plugins, t.Context(), checking.Options{WorkingDir: t.TempDir(), Tools: []contract.Registration{tc.registration}})
			if err != nil {
				t.Fatal(err)
			}
			workflow := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: unsupported_run\n        shell: " + tc.shell + "\n      - uses: example/execute@v1\n        with:\n          code: supported_input\n"
			_, err = checking.Check(t.Context(), options, checkrequest.Request{Kind: checkrequest.Stdin, Reader: strings.NewReader(workflow)})
			if err != nil {
				t.Fatal(err)
			}
			if len(engine.requests) != 1 || !strings.HasSuffix(engine.requests[0].Source, "\nsupported_input\n") && engine.requests[0].Source != "supported_input" {
				t.Fatalf("input-only guest received unsupported run: %+v", engine.requests)
			}
			if tc.name == "shellcheck" && !strings.HasPrefix(engine.requests[0].Source, "export CI GITHUB_ACTION ") {
				t.Fatalf("shell input lacks runtime environment declarations: %q", engine.requests[0].Source)
			}
		})
	}
}

func TestConfiguredComponentCollectsInputSelectors(t *testing.T) {
	t.Parallel()
	module, err := os.ReadFile("../../../../plugins/pluginexecution/component/testdata/plugins/ruff-action-input.wasm")
	if err != nil {
		t.Fatal(err)
	}
	wasm := filepath.Join(t.TempDir(), "ruff.wasm")
	if err := os.WriteFile(wasm, module, 0o600); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	files := map[string]string{
		".git/HEAD":               "ref: refs/heads/master\n",
		".github/actionlint.yaml": fmt.Sprintf("tools:\n  ruff:\n    backend:\n      wasm:\n        path: %q\noverrides:\n  - includes: [disabled/**]\n    tools:\n      ruff:\n        enabled: false\n", filepath.ToSlash(wasm)),
		"workflow.yml":            "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: example/execute@v1\n        with:\n          code: missing();\n      - run: missing();\n        shell: python\n      - uses: ./nested\n      - uses: ./disabled\n",
		"nested/action.yml":       "name: nested\ndescription: fixture\nruns:\n  using: composite\n  steps:\n    - uses: example/execute@v1\n      with:\n        code: missing();\n",
		"disabled/action.yml":     "name: disabled\ndescription: fixture\nruns:\n  using: composite\n  steps:\n    - uses: example/execute@v1\n      with:\n        code: missing();\n",
	}
	for path, source := range files {
		path = filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	registration := providerRegistration(t, "ruff", backends.ToolSelection{Command: "ruff", Process: &hostprocess.CommandOptions{Optional: true}})
	result, err := assembly.CheckWithPluginPaths(t.Context(), checking.Options{WorkingDir: root, Tools: []contract.Registration{registration}}, checkrequest.Request{Kind: checkrequest.Files, Paths: []string{filepath.Join(root, "workflow.yml")}}, nil, dependencycache.Acquisition{})
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, d := range result.Diagnostics {
		if d.Code == "ECHO001" {
			count++
			if strings.Contains(d.Path, "disabled") {
				t.Fatalf("disabled composite routed: %+v", d)
			}
		}
	}
	if count != 2 {
		t.Fatalf("configured component input findings=%d diagnostics=%+v", count, result.Diagnostics)
	}
}

func TestConfiguredComponentRejectsForeignEngineIdentity(t *testing.T) {
	t.Parallel()
	module, err := filepath.Abs("../../../../plugins/pluginexecution/component/testdata/plugins/action-input.wasm")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"ruff", "shellcheck"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			files := map[string]string{
				".git/HEAD":               "ref: refs/heads/master\n",
				".github/actionlint.yaml": fmt.Sprintf("tools:\n  %s:\n    backend:\n      wasm:\n        path: %q\noverrides:\n  - includes: [disabled/**]\n    tools:\n      %s:\n        enabled: false\n", name, filepath.ToSlash(module), name),
				"workflow.yml":            "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: example/execute@v1\n        with:\n          code: missing();\n      - run: missing();\n        shell: python\n      - uses: ./nested\n      - uses: ./disabled\n",
				"nested/action.yml":       "name: nested\ndescription: fixture\nruns:\n  using: composite\n  steps:\n    - uses: example/execute@v1\n      with:\n        code: missing();\n",
				"disabled/action.yml":     "name: disabled\ndescription: fixture\nruns:\n  using: composite\n  steps:\n    - uses: example/execute@v1\n      with:\n        code: missing();\n",
			}
			for path, source := range files {
				path = filepath.Join(root, filepath.FromSlash(path))
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			registration := providerRegistration(t, "ruff", backends.ToolSelection{Command: "ruff", Process: &hostprocess.CommandOptions{Optional: true}})
			if name == "shellcheck" {
				registration = providerRegistration(t, "shellcheck", backends.ToolSelection{Command: "shellcheck", Process: &hostprocess.CommandOptions{Optional: true}})
			}
			_, err := assembly.CheckWithPluginPaths(t.Context(), checking.Options{WorkingDir: root, Tools: []contract.Registration{registration}}, checkrequest.Request{Kind: checkrequest.Files, Paths: []string{filepath.Join(root, "workflow.yml")}}, nil, dependencycache.Acquisition{})
			if err == nil || !strings.Contains(err.Error(), `does not match tool "`+name+`"`) {
				t.Fatalf("foreign engine accepted for %s: %v", name, err)
			}
		})
	}
}

func TestDisabledInputSelectorsDoNotConflict(t *testing.T) {
	t.Parallel()
	for _, enabled := range []bool{false, true} {
		t.Run(strconv.FormatBool(enabled), func(t *testing.T) {
			t.Parallel()
			engine := &inputReplacementBackend{}
			registration, err := backends.RegisterEngine(scriptengine.Descriptor{Rule: rule.Descriptor{Name: "independent", Category: "correctness"}, ScriptInputs: []checkinputs.ScriptInput{{Action: "actions/github-script", Input: "script", Language: "python"}}}, engine)
			if err != nil {
				t.Fatal(err)
			}
			root := t.TempDir()
			configFile := filepath.Join(root, "config.yaml")
			setting := "off"
			if enabled {
				setting = "error"
			}
			if err := os.WriteFile(configFile, []byte("lint:\n  rules:\n    plugins:\n      independent: "+setting+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err = checking.Check(t.Context(), checking.Options{WorkingDir: root, ConfigFile: configFile, Tools: []contract.Registration{registration}}, checkrequest.Request{Kind: checkrequest.Stdin, Reader: strings.NewReader("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/github-script@v9\n        with:\n          script: print(1)\n")})
			if enabled {
				if err == nil || !strings.Contains(err.Error(), "conflicting languages") {
					t.Fatalf("active conflict accepted: %v", err)
				}
			} else if err != nil || len(engine.requests) != 0 {
				t.Fatalf("disabled catalogue still active: requests=%+v err=%v", engine.requests, err)
			}
		})
	}
}

type pythonInputBackend struct{}

func (pythonInputBackend) TemplateContract() string { return scriptengine.TemplateEffectsContract }

func (pythonInputBackend) Check(_ context.Context, request scriptengine.Request) ([]scriptengine.Diagnostic, error) {
	// Model Ruff's undefined-name ranges for the opaque identifier and real name.
	var findings []scriptengine.Diagnostic
	for _, match := range regexp.MustCompile(`_[a-z0-9_]*|missing`).FindAllStringIndex(request.Source, -1) {
		before := request.Source[:match[0]]
		row := strings.Count(before, "\n") + 1
		column := match[0] - strings.LastIndex(before, "\n")
		findings = append(findings, scriptengine.Diagnostic{Filename: request.Filename, Code: "F821", Message: "undefined name", Location: sourcetext.Position{Line: row, Column: column}, EndLocation: sourcetext.Position{Line: row, Column: column + match[1] - match[0]}, TemplateEffect: scriptengine.TemplateEffectOpaqueName})
	}
	return findings, nil
}

func TestRuffInputFiltersOnlyManufacturedNames(t *testing.T) {
	t.Parallel()
	plugins := pluginexecution.Loaded{ScriptEngines: []pluginexecution.ScriptEngineRegistration{{Backend: pythonInputBackend{}, Descriptor: scriptengine.Descriptor{Engine: "ruff", Rule: rule.Descriptor{Name: "ruff"}, TemplateContract: scriptengine.TemplateEffectsContract, ScriptInputs: []checkinputs.ScriptInput{{Action: "owner/python", Input: "source", Language: "python"}}}}}}
	options, err := assembly.ConfigurePlugins(&plugins, t.Context(), checking.Options{WorkingDir: t.TempDir(), Tools: []contract.Registration{providerRegistration(t, "ruff", backends.ToolSelection{Command: "ruff", Process: &hostprocess.CommandOptions{Optional: true}})}})
	if err != nil {
		t.Fatal(err)
	}
	workflow := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: owner/python@v1\n        with:\n          source: |\n            print(${{ github.repository }})\n            print(missing)\n"
	result, err := checking.Check(t.Context(), options, checkrequest.Request{Kind: checkrequest.Stdin, Reader: strings.NewReader(workflow)})
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, d := range result.Diagnostics {
		if d.Code == "F821" {
			count++
			if d.Start.Line != 10 || d.Start.Column != 19 {
				t.Fatalf("synthetic finding escaped or real coordinates changed: %+v", d)
			}
		}
	}
	if count != 1 {
		t.Fatalf("manufactured F821 not filtered independently: %+v", result.Diagnostics)
	}
}

type selectedInputBackend struct {
	inputReplacementBackend
	descriptor scriptengine.Descriptor
}

func (b *selectedInputBackend) ScriptDescriptor() scriptengine.Descriptor {
	return b.descriptor
}

func TestInputDiscoveryRetainsSuccessfulToolSelection(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	for _, mode := range []string{"caller-disabled", "failed-optional", "built"} {
		for _, failure := range []string{"missing", "conflicting"} {
			t.Run(mode+"/"+failure, func(t *testing.T) {
				t.Parallel()
				root := t.TempDir()
				enabled := mode != "caller-disabled"
				files := map[string]string{
					".git/HEAD":               "ref: refs/heads/master\n",
					".github/actionlint.yaml": fmt.Sprintf("tools:\n  shellcheck:\n    enabled: %t\n    backend:\n      wasm: {path: default.wasm}\noverrides:\n  - includes: [nested/**]\n    tools:\n      shellcheck:\n        enabled: true\n        backend:\n          wasm:\n            path: nested.wasm\n", enabled),
					"workflow.yml":            "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: print(1)\n        shell: python\n      - uses: ./nested\n",
					"nested/action.yml":       "name: nested\ndescription: fixture\nruns:\n  using: composite\n  steps:\n    - uses: actions/github-script@v9\n      with:\n        script: console.log(1);\n",
				}
				for name, source := range files {
					path := filepath.Join(root, filepath.FromSlash(name))
					if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				activeRuff := &inputReplacementBackend{}
				shell := providerRegistration(t, "shellcheck", backends.ToolSelection{Command: "shellcheck", Process: &hostprocess.CommandOptions{Optional: true}})
				failedBuilds := 0
				if mode == "failed-optional" {
					shell.Build = func(contract.Context) (contract.Runner, error) {
						failedBuilds++
						return nil, errors.New("optional ShellCheck unavailable")
					}
					shell.Required = func(contract.Context) bool { return false }
				}
				loads := 0
				_, err := checking.Check(t.Context(), checking.Options{WorkingDir: root, Tools: []contract.Registration{providerRegistration(t, "ruff", backends.ToolSelection{Backend: activeRuff}), shell}, LoadScriptEngine: func(_ context.Context, path string) (scriptengine.Backend, error) {
					if filepath.Base(path) == "default.wasm" {
						return &inputReplacementBackend{}, nil
					}
					loads++
					if failure == "missing" {
						return nil, errors.New("override module missing")
					}
					return &selectedInputBackend{descriptor: scriptengine.Descriptor{Engine: "shellcheck", Rule: rule.Descriptor{Name: "shellcheck"}, ScriptInputs: []checkinputs.ScriptInput{{Action: "actions/github-script", Input: "script", Language: "python"}}}}, nil
				}}, checkrequest.Request{Kind: checkrequest.Files, Paths: []string{filepath.Join(root, "workflow.yml")}})
				// The composite action's own configuration enables the tool
				// even when the calling workflow disables it (decision b5 in
				// docs/design/tool-providers-decisions.md).
				if mode == "built" || mode == "caller-disabled" {
					want := "override module missing"
					if failure == "conflicting" {
						want = "conflicting languages"
					}
					if loads != 1 || err == nil || !strings.Contains(err.Error(), want) {
						t.Fatalf("built runner lost composite override: loads=%d err=%v", loads, err)
					}
					return
				}
				// An optional tool whose build failed stays off, even for the
				// composite action its override configures.
				if err != nil || loads != 0 || len(activeRuff.requests) != 1 {
					t.Fatalf("failed optional tool revived by composite override: loads=%d ruff=%+v err=%v", loads, activeRuff.requests, err)
				}
				if failedBuilds != 1 {
					t.Fatalf("optional build not exercised once: %d", failedBuilds)
				}
			})
		}
	}
}
