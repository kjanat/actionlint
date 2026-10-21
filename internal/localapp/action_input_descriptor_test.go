package localapp

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

	"actionlint.kjanat.dev/analyzer"
	"actionlint.kjanat.dev/internal/application"
	"actionlint.kjanat.dev/internal/processconfig"
	"actionlint.kjanat.dev/internal/tools"
	"actionlint.kjanat.dev/internal/tools/backend"
	"actionlint.kjanat.dev/internal/tools/ruff"
	"actionlint.kjanat.dev/internal/tools/shellcheck"
	"actionlint.kjanat.dev/rule"
)

func TestBuiltinInputOnlyDescriptorRejectsNativeRuns(t *testing.T) {
	for _, tc := range []struct {
		name, shell  string
		registration tools.Registration
	}{
		{"ruff", "python", (ruff.Options{Command: "ruff", Process: &processconfig.Options{Optional: true}}).Registration()},
		{"shellcheck", "bash", (shellcheck.Options{Command: "shellcheck", Process: &processconfig.Options{Optional: true}}).Registration()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			engine := &inputReplacementBackend{}
			plugins := loadedPlugins{engines: []scriptEngineRegistration{{backend: engine, descriptor: analyzer.ScriptEngineDescriptor{Engine: tc.name, Rule: rule.Descriptor{Name: tc.name}, ScriptInputs: []analyzer.ScriptInput{{Action: "example/execute", Input: "code", Language: tc.shell}}}}}}
			options, err := plugins.configure(application.Options{WorkingDir: t.TempDir(), Tools: []tools.Registration{tc.registration}})
			if err != nil {
				t.Fatal(err)
			}
			workflow := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: unsupported_run\n        shell: " + tc.shell + "\n      - uses: example/execute@v1\n        with:\n          code: supported_input\n"
			_, err = application.Check(t.Context(), options, application.Input{Kind: application.Stdin, Reader: strings.NewReader(workflow)})
			if err != nil {
				t.Fatal(err)
			}
			if len(engine.requests) != 1 || engine.requests[0].Source != "supported_input" {
				t.Fatalf("input-only guest received unsupported run: %+v", engine.requests)
			}
		})
	}
}

func TestConfiguredComponentCollectsInputSelectors(t *testing.T) {
	module, err := filepath.Abs("../plugins/component/testdata/plugins/action-input.wasm")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"ruff", "shellcheck"} {
		t.Run(name, func(t *testing.T) {
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
			registration := (ruff.Options{Command: "ruff", Process: &processconfig.Options{Optional: true}}).Registration()
			if name == "shellcheck" {
				registration = (shellcheck.Options{Command: "shellcheck", Process: &processconfig.Options{Optional: true}}).Registration()
			}
			result, err := Check(t.Context(), application.Options{WorkingDir: root, Tools: []tools.Registration{registration}}, application.Input{Kind: application.Files, Paths: []string{filepath.Join(root, "workflow.yml")}}, nil)
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
		})
	}
}

func TestDisabledInputSelectorsDoNotConflict(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(strconv.FormatBool(enabled), func(t *testing.T) {
			engine := &inputReplacementBackend{}
			registration, err := backend.RegisterEngine(analyzer.ScriptEngineDescriptor{Rule: rule.Descriptor{Name: "independent"}, ScriptInputs: []analyzer.ScriptInput{{Action: "actions/github-script", Input: "script", Language: "python"}}}, engine)
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
			_, err = application.Check(t.Context(), application.Options{WorkingDir: root, ConfigFile: configFile, Tools: []tools.Registration{registration}}, application.Input{Kind: application.Stdin, Reader: strings.NewReader("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/github-script@v9\n        with:\n          script: print(1)\n")})
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

func (pythonInputBackend) Check(_ context.Context, request analyzer.ScriptRequest) ([]analyzer.ScriptDiagnostic, error) {
	// Model Ruff's undefined-name ranges for the opaque identifier and real name.
	var findings []analyzer.ScriptDiagnostic
	for _, match := range regexp.MustCompile(`_[a-z0-9_]*|missing`).FindAllStringIndex(request.Source, -1) {
		before := request.Source[:match[0]]
		row := strings.Count(before, "\n") + 1
		column := match[0] - strings.LastIndex(before, "\n")
		findings = append(findings, analyzer.ScriptDiagnostic{Filename: request.Filename, Code: "F821", Message: "undefined name", Location: analyzer.ScriptPosition{Row: row, Column: column}, EndLocation: analyzer.ScriptPosition{Row: row, Column: column + match[1] - match[0]}})
	}
	return findings, nil
}
func TestRuffInputFiltersOnlyManufacturedNames(t *testing.T) {
	plugins := loadedPlugins{engines: []scriptEngineRegistration{{backend: pythonInputBackend{}, descriptor: analyzer.ScriptEngineDescriptor{Engine: "ruff", Rule: rule.Descriptor{Name: "ruff"}, ScriptInputs: []analyzer.ScriptInput{{Action: "owner/python", Input: "source", Language: "python"}}}}}}
	options, err := plugins.configure(application.Options{WorkingDir: t.TempDir(), Tools: []tools.Registration{(ruff.Options{Command: "ruff", Process: &processconfig.Options{Optional: true}}).Registration()}})
	if err != nil {
		t.Fatal(err)
	}
	workflow := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: owner/python@v1\n        with:\n          source: |\n            print(${{ github.repository }})\n            print(missing)\n"
	result, err := application.Check(t.Context(), options, application.Input{Kind: application.Stdin, Reader: strings.NewReader(workflow)})
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
	descriptor analyzer.ScriptEngineDescriptor
}

func (b *selectedInputBackend) ScriptDescriptor() analyzer.ScriptEngineDescriptor {
	return b.descriptor
}

func TestInputDiscoveryRetainsSuccessfulToolSelection(t *testing.T) {
	for _, mode := range []string{"caller-disabled", "failed-optional", "built"} {
		for _, failure := range []string{"missing", "conflicting"} {
			t.Run(mode+"/"+failure, func(t *testing.T) {
				root := t.TempDir()
				enabled := mode != "caller-disabled"
				files := map[string]string{
					".git/HEAD":               "ref: refs/heads/master\n",
					".github/actionlint.yaml": fmt.Sprintf("tools:\n  shellcheck:\n    enabled: %t\noverrides:\n  - includes: [nested/**]\n    tools:\n      shellcheck:\n        enabled: true\n        backend:\n          wasm:\n            path: nested.wasm\n", enabled),
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
				shell := (shellcheck.Options{Command: "shellcheck", Process: &processconfig.Options{Optional: true}}).Registration()
				failedBuilds := 0
				if mode == "failed-optional" {
					shell.Build = func(tools.Context) (tools.Runner, error) {
						failedBuilds++
						return nil, errors.New("optional ShellCheck unavailable")
					}
					shell.Required = func(tools.Context) bool { return false }
				}
				loads := 0
				_, err := application.Check(t.Context(), application.Options{WorkingDir: root, Tools: []tools.Registration{(ruff.Options{Backend: activeRuff}).Registration(), shell}, LoadScriptEngine: func(context.Context, string) (analyzer.ScriptBackend, error) {
					loads++
					if failure == "missing" {
						return nil, errors.New("override module missing")
					}
					return &selectedInputBackend{descriptor: analyzer.ScriptEngineDescriptor{Rule: rule.Descriptor{Name: "shellcheck"}, ScriptInputs: []analyzer.ScriptInput{{Action: "actions/github-script", Input: "script", Language: "python"}}}}, nil
				}}, application.Input{Kind: application.Files, Paths: []string{filepath.Join(root, "workflow.yml")}})
				if mode == "built" {
					want := "override module missing"
					if failure == "conflicting" {
						want = "conflicting languages"
					}
					if loads != 1 || err == nil || !strings.Contains(err.Error(), want) {
						t.Fatalf("built runner lost composite override: loads=%d err=%v", loads, err)
					}
					return
				}
				if err != nil || loads != 0 || len(activeRuff.requests) != 1 {
					t.Fatalf("unselected tool revived by composite override: loads=%d ruff=%+v err=%v", loads, activeRuff.requests, err)
				}
				if mode == "failed-optional" && failedBuilds != 1 {
					t.Fatalf("optional build not exercised once: %d", failedBuilds)
				}
			})
		}
	}
}
