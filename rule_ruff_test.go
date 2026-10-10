package actionlint

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"actionlint.kjanat.dev/internal/ruff"
)

func ruffForTest(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("ruff")
	if err != nil {
		t.Skip("Ruff is not installed")
	}
	return path
}

func TestRuffPythonScripts(t *testing.T) {
	command := ruffForTest(t)
	for _, tc := range []struct {
		name, defaults, job, shell, script, config, runner string
		count                                              int
	}{
		{name: "undefined", shell: "python", script: "print(missing)", count: 1},
		{name: "syntax", shell: "python", script: "if:", count: 2},
		{name: "empty selection", shell: "python", script: "print(missing)", config: "tools: {ruff: {select: []}}"},
		{name: "custom python", shell: "python -u {0}", script: "print(missing)", count: 1},
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
			src := "on: push\n" + tc.defaults + "jobs:\n  test:\n    runs-on: " + runner + "\n" + tc.job + "    steps:\n      - run: |\n          " + strings.ReplaceAll(tc.script, "\n", "\n          ") + "\n"
			if tc.shell != "" {
				src += "        shell: " + tc.shell + "\n"
			}
			config, err := ParseConfig([]byte(tc.config))
			if err != nil {
				t.Fatal(err)
			}
			result, err := Analyze(t.Context(), AnalysisRequest{Ruff: command, WorkingDir: t.TempDir(), Sources: []SourceUnit{{Path: "ci.yml", Content: []byte(src), Config: config}}})
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
	command := ruffForTest(t)
	root := t.TempDir()
	writeShellcheckFixture(t, root, "ruff.toml", "[lint]\nignore = ['F821']\n")
	for _, script := range []string{"|\n          print('é', missing)", "print('é', missing)", `"print('é', missing)"`, "'print(''é'', missing)'"} {
		src := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - shell: python\n        run: " + script + "\n"
		result, err := Analyze(t.Context(), AnalysisRequest{Ruff: command, WorkingDir: root, Sources: []SourceUnit{{Path: "ci.yml", Content: []byte(src)}}})
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Diagnostics) != 1 {
			t.Fatalf("got %+v", result.Diagnostics)
		}
		finding := result.files[0].errors[0]
		lines := strings.Split(src, "\n")
		wantColumn := len([]rune(strings.Split(lines[finding.Line-1], "missing")[0])) + 1
		// Quoted/folded scalar mappings fall back to the run key position.
		if strings.HasPrefix(script, "\"") || strings.HasPrefix(script, "'") {
			wantColumn = 9
		}
		if finding.Column != wantColumn || finding.code != "F821" {
			t.Fatalf("wrong source mapping: %+v want column %d", finding, wantColumn)
		}
	}
}

// Dispatch before Go's test flag parsing so this executable accepts Ruff argv.
func TestMain(m *testing.M) {
	if os.Getenv("ACTIONLINT_TEST_RUFF") == "1" {
		runRuffHelperProcess()
		return
	}
	os.Exit(m.Run())
}

// The portable fake process exercises failures even when Ruff is not installed.
func runRuffHelperProcess() {
	if prefix := os.Getenv("ACTIONLINT_TEST_RUFF_PREFIX"); prefix != "" {
		var expected []string
		if err := json.Unmarshal([]byte(prefix), &expected); err != nil || len(os.Args) <= len(expected) || !slices.Equal(os.Args[1:len(expected)+1], expected) {
			os.Exit(3)
		}
		file, err := os.OpenFile(os.Getenv("ACTIONLINT_TEST_RUFF_CALLS"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			os.Exit(3)
		}
		if _, err := fmt.Fprintln(file, strings.Join(os.Args[1:], " ")); err != nil {
			os.Exit(3)
		}
		if err := file.Close(); err != nil {
			os.Exit(3)
		}
		os.Args = append(os.Args[:1], os.Args[len(expected)+1:]...)
		if len(os.Args) == 2 && os.Args[1] == "--version" {
			fmt.Println("ruff 0.17.0")
			os.Exit(0)
		}
	}
	_, _ = io.Copy(io.Discard, os.Stdin)
	if os.Getenv("ACTIONLINT_TEST_RUFF_WAIT") == "1" {
		time.Sleep(10 * time.Second)
	}
	if len(os.Args) < 2 || os.Args[1] != "check" || !slices.Contains(os.Args, "--isolated") || !slices.Contains(os.Args, "--no-fix") {
		os.Exit(3)
	}
	if _, present := os.LookupEnv("RUFF_OUTPUT_FILE"); present {
		os.Exit(3)
	}
	if os.Getenv("ACTIONLINT_TEST_RUFF_FLAGS") == "1" && !slices.Equal(os.Args[2:5], []string{"--preview", "--exclude", "vendor/**"}) {
		os.Exit(3)
	}
	fmt.Print(os.Getenv("ACTIONLINT_TEST_RUFF_OUTPUT"))
	code, _ := strconv.Atoi(os.Getenv("ACTIONLINT_TEST_RUFF_EXIT"))
	os.Exit(code)
}

func TestRuffCancellation(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	options := &ExternalCommandOptions{Executable: &exe, Environment: []string{"ACTIONLINT_TEST_RUFF=1", "ACTIONLINT_TEST_RUFF_WAIT=1"}}
	src := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - shell: python\n        run: print('ok')\n"
	start := time.Now()
	_, err = Analyze(ctx, AnalysisRequest{RuffOptions: options, WorkingDir: t.TempDir(), Sources: []SourceUnit{{Path: "ci.yml", Content: []byte(src)}}})
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
			config, err := ParseConfig([]byte(tc.config))
			if err != nil {
				t.Fatal(err)
			}
			missing := filepath.Join(t.TempDir(), "missing-ruff")
			_, err = Analyze(t.Context(), AnalysisRequest{
				RuffOptions: &ExternalCommandOptions{Executable: &missing, Optional: tc.optional},
				WorkingDir:  t.TempDir(),
				Sources:     []SourceUnit{{Path: "ci.yml", Config: config, Content: []byte("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: print(missing)\n        shell: python\n")}},
			})
			if (err != nil) != tc.wantError {
				t.Fatalf("error = %v, wantError %v", err, tc.wantError)
			}
		})
	}
}

func TestRuffSuppressionAndOverride(t *testing.T) {
	command := ruffForTest(t)
	src := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - shell: python\n        run: print(missing) # actionlint:ignore ruff -- intentional undefined-name fixture\n"
	result, err := Analyze(t.Context(), AnalysisRequest{Ruff: command, WorkingDir: t.TempDir(), Sources: []SourceUnit{{Path: "ci.yml", Content: []byte(src)}}})
	if err != nil || len(result.Diagnostics) != 0 {
		t.Fatalf("%+v %v", result, err)
	}
	src = strings.Split(src, " # actionlint:")[0] + "\n"
	cfg, err := ParseConfig([]byte("overrides: [{includes: [ci.yml], lint: {rules: {external: {ruff: warn}}}}]"))
	if err != nil {
		t.Fatal(err)
	}
	result, err = Analyze(t.Context(), AnalysisRequest{Ruff: command, WorkingDir: t.TempDir(), Sources: []SourceUnit{{Path: "ci.yml", Content: []byte(src), Config: cfg}}})
	if err != nil || len(result.Diagnostics) != 1 || result.Diagnostics[0].Severity != "warning" {
		t.Fatalf("%+v %v", result, err)
	}
}

func TestRuffInterpolatedTokenSkipsOnlyItsScript(t *testing.T) {
	for _, script := range []string{"value = ${{ github.run_number }}.0", "import ${{ 'json' }}", "from json import ${{ 'loads' }}", `print(f"{lhs ${{ '==' }} rhs}")`, `print(f"{item_${{ 'suffix' }}}")`} {
		t.Run(script, func(t *testing.T) {
			source := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - shell: python\n        run: value = ${{ github.run_number }}.0\n      - shell: python\n        run: print(missing)\n"
			source = strings.Replace(source, "value = ${{ github.run_number }}.0", script, 1)
			result, err := Analyze(t.Context(), AnalysisRequest{
				Ruff: ruffForTest(t), WorkingDir: t.TempDir(),
				Sources: []SourceUnit{{Path: "ci.yml", Content: []byte(source)}},
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != "ruff" || result.Diagnostics[0].Code != "F821" || result.Diagnostics[0].Start.Line != 9 {
				t.Fatalf("unexpected findings: %+v", result.Diagnostics)
			}
		})
	}
}

func TestRuffQuotedEmptyLineSkipsOnlyItsScript(t *testing.T) {
	for _, quote := range []string{"'", "\""} {
		for _, ending := range []string{"\n", "\r\n"} {
			source := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - shell: python\n        run: |\n          print(" + quote + "${{\n\n            github.sha\n          }}" + quote + ")\n      - shell: python3.12 {0}\n        run: print(missing)\n"
			source = strings.ReplaceAll(source, "\n", ending)
			result, err := Analyze(t.Context(), AnalysisRequest{
				Ruff: ruffForTest(t), WorkingDir: t.TempDir(),
				Sources: []SourceUnit{{Path: "ci.yml", Content: []byte(source)}},
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != "ruff" || result.Diagnostics[0].Code != "F821" || result.Diagnostics[0].Start.Line != 13 {
				t.Fatalf("unexpected findings: %+v", result.Diagnostics)
			}
		}
	}
}

func TestSanitizePythonScript(t *testing.T) {
	for _, source := range []string{"print(${{ github.sha }})", "print('${{ '}}' }}')", "print(${{\n github.sha\n}})", "print(${{ 'é' }}, missing)"} {
		got, valid, err := ruff.Sanitize(source, ruffExpressionEnd)
		if err != nil {
			t.Fatal(err)
		}
		if !valid || strings.Contains(got, "${{") || len([]rune(got)) != len([]rune(source)) {
			t.Fatalf("%q -> %q, %v", source, got, valid)
		}
		for i, r := range []rune(source) {
			if (r == '\n' || r == '\r') && []rune(got)[i] != r {
				t.Fatal("line break moved")
			}
		}
	}
	if _, valid, _ := ruff.Sanitize("print(${{ missing)", ruffExpressionEnd); valid {
		t.Fatal("accepted malformed template")
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
			options := &ExternalCommandOptions{Executable: &exe, Environment: []string{"ACTIONLINT_TEST_RUFF=1", "ACTIONLINT_TEST_RUFF_OUTPUT=" + tc.output, "ACTIONLINT_TEST_RUFF_EXIT=" + strconv.Itoa(tc.code)}}
			src := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - shell: python\n        run: print(missing)\n"
			_, err := Analyze(t.Context(), AnalysisRequest{RuffOptions: options, WorkingDir: t.TempDir(), Sources: []SourceUnit{{Path: "ci.yml", Content: []byte(src)}}})
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
			options := &ExternalCommandOptions{
				Executable:  &executable,
				Arguments:   []string{"--preview", "--exclude", "vendor/**"},
				Environment: []string{"ACTIONLINT_TEST_RUFF=1", "ACTIONLINT_TEST_RUFF_FLAGS=1", "ACTIONLINT_TEST_RUFF_OUTPUT=[]", "RUFF_OUTPUT_FILE=explicit-output.json"},
			}
			command := ""
			if legacy {
				command = strconv.Quote(executable) + " --preview --exclude vendor/**"
				options.Executable, options.Arguments = nil, nil
			}
			source := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - shell: python\n        run: print('ok')\n"
			_, err := Analyze(t.Context(), AnalysisRequest{Ruff: command, RuffOptions: options, WorkingDir: t.TempDir(), Sources: []SourceUnit{{Path: "ci.yml", Content: []byte(source)}}})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRuffPreviewAndAmbientOutputFile(t *testing.T) {
	command := ruffForTest(t)
	root := t.TempDir()
	output := writeShellcheckFixture(t, root, "output.json", "keep this content")
	t.Setenv("RUFF_OUTPUT_FILE", output)
	source := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - shell: python\n        run: 'if:'\n"
	result, err := Analyze(t.Context(), AnalysisRequest{
		RuffOptions: &ExternalCommandOptions{Executable: &command, Arguments: []string{"--preview"}},
		WorkingDir:  root, Sources: []SourceUnit{{Path: "ci.yml", Content: []byte(source)}},
	})
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
	command := ruffForTest(t)
	source := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - shell: python\n        run: print(missing)\n"
	for _, language := range []string{"ipynb", "pyi"} {
		options := &ExternalCommandOptions{Executable: &command, Arguments: []string{"--config", "extension = { py = " + strconv.Quote(language) + " }"}}
		result, err := Analyze(t.Context(), AnalysisRequest{RuffOptions: options, Sources: []SourceUnit{{Path: "ci.yml", Content: []byte(source)}}})
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != "F821" || result.Diagnostics[0].Start.Line != 7 || result.Diagnostics[0].Start.Column != 20 {
			t.Fatalf("inline config changed Python source type or positions: %+v", result.Diagnostics)
		}
	}
}

func TestRuffComposite(t *testing.T) {
	command := ruffForTest(t)
	root := t.TempDir()
	writeShellcheckFixture(t, root, ".github/workflows/ci.yml", commandGoodWorkflow)
	writeShellcheckFixture(t, root, "python-action/action.yml", "name: Python\ndescription: test\nruns:\n  using: composite\n  steps:\n    - shell: python\n      run: print(missing)\n")
	src := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: $/python-action\n"
	result, err := Analyze(t.Context(), AnalysisRequest{Ruff: command, WorkingDir: root, Sources: []SourceUnit{{Path: filepath.Join(root, ".github/workflows/ci.yml"), Content: []byte(src), Project: &Project{root: root}}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Diagnostics) != 1 || !strings.HasSuffix(filepath.ToSlash(result.files[0].errors[0].Filepath), "python-action/action.yml") {
		t.Fatalf("got %+v", result.Diagnostics)
	}
}
