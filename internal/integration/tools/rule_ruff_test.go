package tools_test

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"actionlint.kjanat.dev/internal/application"
	repoconfig "actionlint.kjanat.dev/internal/config"
	"actionlint.kjanat.dev/internal/processconfig"
	"actionlint.kjanat.dev/internal/testutil"
	"actionlint.kjanat.dev/internal/tools/adapters"
	"actionlint.kjanat.dev/internal/workspace"
)

func TestRuffPythonScripts(t *testing.T) {
	command := testutil.RequireRuff(t)
	for _, tc := range []struct {
		name, defaults, job, shell, script, config, runner string
		count                                              int
	}{
		{name: "undefined", shell: "python", script: "print(missing)", count: 1},
		{name: "syntax", shell: "python", script: "if:", count: 2},
		{name: "empty selection", shell: "python", script: "print(missing)", config: "tools: {ruff: {select: []}}"},
		{name: "custom python", shell: "python -u {0}", script: "print(missing)", count: 1},
		{name: "quoted Python command", shell: `'"python" {0}'`, script: "print(missing)", count: 1},
		{name: "quoted Windows Python path", shell: `'"C:\Program Files\Python\python.exe" {0}'`, script: "print(missing)", count: 1},
		{name: "quoted version target", shell: `'"/opt/Python Tools/python3.9" {0}'`, script: "match 1:\n    case 1: pass", count: 1},
		{name: "quoted configured target", shell: `'"/opt/Python Tools/python3.9" {0}'`, config: "tools: {ruff: {target-version: py310}}", script: "match 1:\n    case 1: pass"},
		{name: "Windows py launcher", runner: "windows-latest", shell: "py {0}", script: "print(missing)", count: 1},
		{name: "Windows py launcher flags", runner: "windows-latest", shell: "py -3 -u {0}", script: "print(missing)", count: 1},
		{name: "Windows py executable", runner: "windows-latest", shell: "py.exe {0}", script: "print(missing)", count: 1},
		{name: "Windows py executable path", runner: "windows-latest", shell: `C:\Windows\py.exe {0}`, script: "print(missing)", count: 1},
		{name: "Windows py workflow default", runner: "windows-latest", defaults: "defaults: {run: {shell: 'py {0}'}}\n", script: "print(missing)", count: 1},
		{name: "Windows py job default", runner: "windows-latest", job: "    defaults: {run: {shell: 'py {0}'}}\n", script: "print(missing)", count: 1},
		{name: "Windows py39 match syntax", runner: "windows-latest", shell: "py -3.9 {0}", script: "match 1:\n    case 1: pass", count: 1},
		{name: "Windows py310 match syntax", runner: "windows-latest", shell: "py -3.10 -u {0}", script: "match 1:\n    case 1: pass"},
		{name: "Windows py tagged match syntax", runner: "windows-latest", shell: `C:\Windows\PY.EXE -V:3.9-64 {0}`, script: "match 1:\n    case 1: pass", count: 1},
		{name: "Windows py PythonCore match syntax", runner: "windows-latest", shell: "py -V:PythonCore/3.9 {0}", script: "match 1:\n    case 1: pass", count: 1},
		{name: "Windows py configured newer target", runner: "windows-latest", shell: "py -3.9 {0}", config: "tools: {ruff: {target-version: py310}}", script: "match 1:\n    case 1: pass"},
		{name: "Windows py configured older target", runner: "windows-latest", shell: "py -3.10 {0}", config: "tools: {ruff: {target-version: py39}}", script: "match 1:\n    case 1: pass", count: 1},
		{name: "Windows py unspecified minor target", runner: "windows-latest", shell: "py -3 {0}", script: "match 1:\n    case 1: pass"},
		{name: "adapter py39 match syntax", shell: "actions-shells py -3.9 {0}", script: "match 1:\n    case 1: pass", count: 1},
		{name: "adapter py310 match syntax", shell: "actions-shell py -3.10 -u {0}", script: "match 1:\n    case 1: pass"},
		{name: "adapter py configured target", shell: "actions-shells py -3.9 {0}", config: "tools: {ruff: {target-version: py310}}", script: "match 1:\n    case 1: pass"},
		{name: "versioned python", shell: "python3.12 {0}", script: "print(missing)", count: 1},
		{name: "versioned python path", shell: "/usr/bin/python3.13 -u {0}", script: "print(missing)", count: 1},
		{name: "actions shell python", shell: "actions-shell python {0}", script: "print(missing)", count: 1},
		{name: "actions shells py alias", shell: "actions-shells py -u {0}", script: "print(missing)", count: 1},
		{name: "actions shell cmd", shell: "actions-shell.cmd python {0}", script: "print(missing)", count: 1},
		{name: "actions shell quoted arguments", shell: `actions-shell 'py' -W 'ignore::DeprecationWarning' '{0}'`, script: "print(missing)", count: 1},
		{name: "actions shell workflow default", defaults: "defaults: {run: {shell: 'actions-shell python {0}'}}\n", script: "print(missing)", count: 1},
		{name: "actions shell rust", shell: "actions-shell rust {0}", script: "println!(\"hello\");"},
		{name: "actions shell node", shell: "actions-shells node {0}", script: "console.log('hello');"},
		{name: "unknown Python wrapper", shell: "other-wrapper python {0}", script: "print(missing)"},
		{name: "workflow default", defaults: "defaults: {run: {shell: python}}\n", script: "print(missing)", count: 1},
		{name: "job default", job: "    defaults: {run: {shell: python}}\n", script: "print(missing)", count: 1},
		{name: "step overrides job", job: "    defaults: {run: {shell: python}}\n", shell: "bash", script: "echo missing"},
		{name: "job overrides workflow", defaults: "defaults: {run: {shell: python}}\n", job: "    defaults: {run: {shell: bash}}\n", script: "echo missing"},
		{name: "unknown shell", defaults: "defaults: {run: {shell: python}}\n", shell: "${{ 'bash' }}", script: "echo missing"},
		{name: "no shell", script: "echo missing"},
		{name: "noqa ignored", shell: "python", script: "print(missing) # noqa: F821", count: 1},
		{name: "template", shell: "python", script: "print(${{ github.run_number }})"},
		{name: "numeric template fragment", shell: "python", script: "value = ${{ github.run_number }}.0"},
		{name: "numeric suffix fragment", shell: "python", script: "value = 3.${{ github.run_number }}"},
		{name: "identifier template fragment", shell: "python", script: "item_${{ github.run_number }} = 1"},
		{name: "multiline identifier fragment", shell: "python", script: "item_${{\n github.run_number\n}} = 1"},
		{name: "template punctuation", shell: "python", script: "value=[${{ github.run_number }}+1, 2]\nprint(missing)", count: 1},
		{name: "multiline template punctuation", shell: "python", script: "value={${{\n github.run_number\n}}:2}\nprint(missing)", count: 1},
		{name: "quoted token fragments", shell: "python", script: "value='item_${{ github.run_number }}.0'\nprint(missing)", count: 1},
		{name: "comment token fragments", shell: "python", script: "# item_${{ github.run_number }}.0\nprint(missing)", count: 1},
		{name: "quoted template", shell: "python", script: "print('${{ github.sha }}')"},
		{name: "quoted multiline template", shell: "python", script: "print('${{\n github.sha\n}}')\nprint(missing)", count: 1},
		{name: "quoted empty template line", shell: "python", script: "print('${{\n\n github.sha\n}}')"},
		{name: "double quoted empty template line", shell: "python", script: "print(\"${{\n\n github.sha\n}}\")"},
		{name: "double quoted multiline template", shell: "python", script: "print(\"${{\n github.sha\n}}\")"},
		{name: "raw multiline template", shell: "python", script: "print(r'${{\n github.sha\n}}')"},
		{name: "comment multiline template", shell: "python", script: "# ${{\n github.sha\n}}\nprint(missing)", count: 1},
		{name: "comment empty template line", shell: "python", script: "# ${{\n\n github.sha\n}} trailing comment\nprint(missing)", count: 1},
		{name: "unquoted empty template line", shell: "python", script: "print(${{\n\n github.run_number\n}})\nprint(missing)", count: 1},
		{name: "triple quoted empty template line", shell: "python", script: "print('''${{\n\n github.sha\n}}''')\nprint(missing)", count: 1},
		{name: "quoted hash before template", shell: "python", script: "print('# ${{\n github.sha\n}}')\nprint(missing)", count: 1},
		{name: "explicit ignore", shell: "python", script: "print(missing)", config: "tools: {ruff: {ignore: [F821]}}"},
		{name: "disabled tool", shell: "python", script: "print(missing)", config: "tools: {ruff: {enabled: false}}"},
		{name: "disabled rule", shell: "python", script: "print(missing)", config: "lint: {rules: {external: {ruff: off}}}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runner := tc.runner
			if runner == "" {
				runner = "ubuntu-latest"
			}
			src := "on: push\n" + tc.defaults + `jobs:
  test:
    runs-on: ` + runner + "\n" + tc.job + `    steps:
      - run: |
          ` + strings.ReplaceAll(tc.script, "\n", "\n          ") + "\n"
			if tc.shell != "" {
				src += "        shell: " + tc.shell + "\n"
			}
			config, err := repoconfig.Parse([]byte(tc.config))
			if err != nil {
				t.Fatal(err)
			}
			result, err := application.Analyze(t.Context(), application.Options{WorkingDir: t.TempDir(), Tools: (adapters.Options{Ruff: command}).Registrations()}, []workspace.Source{{Path: "ci.yml", Content: []byte(src), Config: config}})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Diagnostics) != tc.count {
				t.Fatalf("got %+v, want %d findings", result.Diagnostics, tc.count)
			}
			for _, d := range result.Diagnostics {
				if d.Rule != "ruff" {
					t.Fatalf("unexpected diagnostic: %+v", d)
				}
			}
		})
	}
}

func TestRuffSourceMappingAndIsolation(t *testing.T) {
	command := testutil.RequireRuff(t)
	root := t.TempDir()
	testutil.WriteFixture(t, root, "ruff.toml", "[lint]\nignore = ['F821']\n")
	for _, script := range []string{"|\n          print('é', missing)", "print('é', missing)", `"print('é', missing)"`, "'print(''é'', missing)'"} {
		src := `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - shell: python
        run: ` + script + "\n"
		result, err := application.Analyze(t.Context(), application.Options{WorkingDir: root, Tools: (adapters.Options{Ruff: command}).Registrations()}, []workspace.Source{{Path: "ci.yml", Content: []byte(src)}})
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Diagnostics) != 1 {
			t.Fatalf("got %+v", result.Diagnostics)
		}
		finding := result.Diagnostics[0]
		lines := strings.Split(src, "\n")
		wantColumn := len([]rune(strings.Split(lines[finding.Start.Line-1], "missing")[0])) + 1
		if finding.Start.Column != wantColumn || finding.Code != "F821" {
			t.Fatalf("wrong source mapping: %+v want column %d", finding, wantColumn)
		}
	}
}

func TestRuffCancellation(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	options := &processconfig.Options{Executable: &exe, Environment: []string{"ACTIONLINT_TEST_RUFF=1", "ACTIONLINT_TEST_RUFF_WAIT=1"}}
	src := `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - shell: python
        run: print('ok')
`
	start := time.Now()
	_, err = application.Analyze(ctx, application.Options{WorkingDir: t.TempDir(), Tools: (adapters.Options{RuffOptions: options}).Registrations()}, []workspace.Source{{Path: "ci.yml", Content: []byte(src)}})
	if err == nil || time.Since(start) > 3*time.Second {
		t.Fatalf("cancellation did not stop Ruff promptly: %v", err)
	}
}

func TestRuffMissingExecutable(t *testing.T) {
	for _, tc := range []struct {
		name      string
		optional  bool
		config    string
		wantError bool
	}{
		{"explicit", false, "", true},
		{"automatic discovery", true, "", false},
		{"disabled tool", false, "tools: {ruff: false}", false},
		{"disabled rule", false, "lint: {rules: {external: {ruff: off}}}", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config, err := repoconfig.Parse([]byte(tc.config))
			if err != nil {
				t.Fatal(err)
			}
			missing := filepath.Join(t.TempDir(), "missing-ruff")
			_, err = application.Analyze(t.Context(), application.Options{WorkingDir: t.TempDir(),

				Tools: (adapters.Options{RuffOptions: &processconfig.Options{Executable: &missing, Optional: tc.optional}}).Registrations()}, []workspace.Source{{Path: "ci.yml", Config: config, Content: []byte(`on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: print(missing)
        shell: python
`)}},
			)
			if (err != nil) != tc.wantError {
				t.Fatalf("error = %v, wantError %v", err, tc.wantError)
			}
		})
	}
}

func TestRuffSuppressionAndOverride(t *testing.T) {
	command := testutil.RequireRuff(t)
	src := `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - shell: python
        run: print(missing) # actionlint:ignore ruff -- intentional undefined-name fixture
`
	result, err := application.Analyze(t.Context(), application.Options{WorkingDir: t.TempDir(), Tools: (adapters.Options{Ruff: command}).Registrations()}, []workspace.Source{{Path: "ci.yml", Content: []byte(src)}})
	if err != nil || len(result.Diagnostics) != 0 {
		t.Fatalf("%+v %v", result, err)
	}
	src = strings.Split(src, " # actionlint:")[0] + "\n"
	cfg, err := repoconfig.Parse([]byte("overrides: [{includes: [ci.yml], lint: {rules: {external: {ruff: warn}}}}]"))
	if err != nil {
		t.Fatal(err)
	}
	result, err = application.Analyze(t.Context(), application.Options{WorkingDir: t.TempDir(), Tools: (adapters.Options{Ruff: command}).Registrations()}, []workspace.Source{{Path: "ci.yml", Content: []byte(src), Config: cfg}})
	if err != nil || len(result.Diagnostics) != 1 || result.Diagnostics[0].Severity != "warning" {
		t.Fatalf("%+v %v", result, err)
	}
}

func TestRuffInterpolatedTokenSkipsOnlyItsScript(t *testing.T) {
	for _, script := range []string{"value = ${{ github.run_number }}.0", "import ${{ 'json' }}", "from json import ${{ 'loads' }}", `print(f"{lhs ${{ '==' }} rhs}")`, `print(f"{item_${{ 'suffix' }}}")`} {
		t.Run(script, func(t *testing.T) {
			source := `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - shell: python
        run: value = ${{ github.run_number }}.0
      - shell: python
        run: print(missing)
`
			source = strings.Replace(source, "value = ${{ github.run_number }}.0", script, 1)
			result, err := application.Analyze(t.Context(), application.Options{WorkingDir: t.TempDir(),
				Tools: (adapters.Options{Ruff: testutil.RequireRuff(t)}).Registrations()}, []workspace.Source{{Path: "ci.yml", Content: []byte(source)}},
			)
			if err != nil {
				t.Fatal(err)
			}
			interpolationLine := 0
			if strings.Contains(script, "${{ '==' }}") {
				interpolationLine = 7
			}
			ruff := ruffFindingsWithInterpolation(t, result.Diagnostics, interpolationLine)
			if len(ruff) != 1 || ruff[0].Code != "F821" || ruff[0].Start.Line != 9 {
				t.Fatalf("unexpected findings: %+v", result.Diagnostics)
			}
		})
	}
}

func TestRuffQuotedEmptyLineSkipsOnlyItsScript(t *testing.T) {
	for _, quote := range []string{"'", "\""} {
		for _, ending := range []string{"\n", "\r\n"} {
			source := `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - shell: python
        run: |
          print(` + quote + "${{\n\n            github.sha\n          }}" + quote + `)
      - shell: python3.12 {0}
        run: print(missing)
`
			source = strings.ReplaceAll(source, "\n", ending)
			result, err := application.Analyze(t.Context(), application.Options{WorkingDir: t.TempDir(),
				Tools: (adapters.Options{Ruff: testutil.RequireRuff(t)}).Registrations()}, []workspace.Source{{Path: "ci.yml", Content: []byte(source)}},
			)
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != "ruff" || result.Diagnostics[0].Code != "F821" || result.Diagnostics[0].Start.Line != 13 {
				t.Fatalf("unexpected findings: %+v", result.Diagnostics)
			}
		}
	}
}

func TestRuffProcessFailures(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, output string
		code         int
		failed       bool
	}{
		{"clean", "[]", 0, false},
		{"findings exit", "[]", 1, false},
		{"invocation failure despite JSON", "[]", 2, true},
		{"malformed JSON", "oops", 0, true},
		{"null JSON", "null", 0, true},
		{"invalid diagnostic", `[{"code":"F821"}]`, 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			options := &processconfig.Options{Executable: &exe, Environment: []string{"ACTIONLINT_TEST_RUFF=1", "ACTIONLINT_TEST_RUFF_OUTPUT=" + tc.output, "ACTIONLINT_TEST_RUFF_EXIT=" + strconv.Itoa(tc.code)}}
			src := `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - shell: python
        run: print(missing)
`
			_, err := application.Analyze(t.Context(), application.Options{WorkingDir: t.TempDir(), Tools: (adapters.Options{RuffOptions: options}).Registrations()}, []workspace.Source{{Path: "ci.yml", Content: []byte(src)}})
			if (err != nil) != tc.failed {
				t.Fatalf("error=%v, want failed=%v", err, tc.failed)
			}
		})
	}
}

func TestRuffConfiguredFlagsAndEnvironment(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("RUFF_OUTPUT_FILE", filepath.Join(t.TempDir(), "must-not-write.json"))
	for _, legacy := range []bool{false, true} {
		t.Run(strconv.FormatBool(legacy), func(t *testing.T) {
			options := &processconfig.Options{
				Executable:  &executable,
				Arguments:   []string{"--preview", "--exclude", "vendor/**"},
				Environment: []string{"ACTIONLINT_TEST_RUFF=1", "ACTIONLINT_TEST_RUFF_FLAGS=1", "ACTIONLINT_TEST_RUFF_OUTPUT=[]", "RUFF_OUTPUT_FILE=explicit-output.json"},
			}
			command := ""
			if legacy {
				command = strconv.Quote(executable) + " --preview --exclude vendor/**"
				options.Executable, options.Arguments = nil, nil
			}
			source := `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - shell: python
        run: print('ok')
`
			_, err := application.Analyze(t.Context(), application.Options{WorkingDir: t.TempDir(), Tools: (adapters.Options{Ruff: command, RuffOptions: options}).Registrations()}, []workspace.Source{{Path: "ci.yml", Content: []byte(source)}})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRuffPreviewAndAmbientOutputFile(t *testing.T) {
	command := testutil.RequireRuff(t)
	root := t.TempDir()
	output := testutil.WriteFixture(t, root, "output.json", "keep this content")
	t.Setenv("RUFF_OUTPUT_FILE", output)
	source := `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - shell: python
        run: 'if:'
`
	result, err := application.Analyze(t.Context(), application.Options{WorkingDir: root, Tools: (adapters.Options{RuffOptions: &processconfig.Options{Executable: &command, Arguments: []string{"--preview"}}}).Registrations()}, []workspace.Source{{Path: "ci.yml", Content: []byte(source)}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Diagnostics) == 0 {
		t.Fatal("missing syntax diagnostics")
	}
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Rule != "ruff" || diagnostic.Code != "invalid-syntax" {
			t.Fatalf("invalid preview diagnostic: %+v", diagnostic)
		}
	}
	if content, err := os.ReadFile(output); err != nil || string(content) != "keep this content" {
		t.Fatalf("ambient output file modified: %q, %v", content, err)
	}
}

func TestRuffInlineConfigCannotChangeSourceType(t *testing.T) {
	command := testutil.RequireRuff(t)
	source := `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - shell: python
        run: print(missing)
`
	for _, language := range []string{"ipynb", "pyi"} {
		options := &processconfig.Options{Executable: &command, Arguments: []string{"--config", "extension = { py = " + strconv.Quote(language) + " }"}}
		result, err := application.Analyze(t.Context(), application.Options{Tools: (adapters.Options{RuffOptions: options}).Registrations()}, []workspace.Source{{Path: "ci.yml", Content: []byte(source)}})
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != "F821" || result.Diagnostics[0].Start.Line != 7 || result.Diagnostics[0].Start.Column != 20 {
			t.Fatalf("inline config changed Python source type or positions: %+v", result.Diagnostics)
		}
	}
}

func TestRuffComposite(t *testing.T) {
	command := testutil.RequireRuff(t)
	root := t.TempDir()
	testutil.WriteFixture(t, root, ".github/workflows/ci.yml", testutil.GoodWorkflow)
	testutil.WriteFixture(t, root, "python-action/action.yml", `name: Python
description: test
runs:
  using: composite
  steps:
    - shell: python
      run: print(missing)
`)
	src := `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: $/python-action
`
	result, err := application.Analyze(t.Context(), application.Options{WorkingDir: root, Tools: (adapters.Options{Ruff: command}).Registrations()}, []workspace.Source{{Path: filepath.Join(root, ".github/workflows/ci.yml"), Content: []byte(src), Project: workspace.Identity(root)}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Diagnostics) != 1 || !strings.HasSuffix(filepath.ToSlash(result.Diagnostics[0].Path), "python-action/action.yml") {
		t.Fatalf("got %+v", result.Diagnostics)
	}
}
