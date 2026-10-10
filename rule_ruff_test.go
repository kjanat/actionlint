package actionlint

import (
	"context"
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
		name, defaults, job, shell, script, config string
		count                                      int
	}{
		{name: "undefined", shell: "python", script: "print(missing)", count: 1},
		{name: "syntax", shell: "python", script: "if:", count: 2},
		{name: "empty selection", shell: "python", script: "print(missing)", config: "tools: {ruff: {select: []}}"},
		{name: "custom python", shell: "python -u {0}", script: "print(missing)", count: 1},
		{name: "workflow default", defaults: "defaults: {run: {shell: python}}\n", script: "print(missing)", count: 1},
		{name: "job default", job: "    defaults: {run: {shell: python}}\n", script: "print(missing)", count: 1},
		{name: "step overrides job", job: "    defaults: {run: {shell: python}}\n", shell: "bash", script: "echo missing"},
		{name: "job overrides workflow", defaults: "defaults: {run: {shell: python}}\n", job: "    defaults: {run: {shell: bash}}\n", script: "echo missing"},
		{name: "unknown shell", defaults: "defaults: {run: {shell: python}}\n", shell: "${{ 'bash' }}", script: "echo missing"},
		{name: "no shell", script: "echo missing"},
		{name: "noqa ignored", shell: "python", script: "print(missing) # noqa: F821", count: 1},
		{name: "template", shell: "python", script: "print(${{ github.run_number }})"},
		{name: "quoted template", shell: "python", script: "print('${{ github.sha }}')"},
		{name: "explicit ignore", shell: "python", script: "print(missing)", config: "tools: {ruff: {ignore: [F821]}}"},
		{name: "disabled tool", shell: "python", script: "print(missing)", config: "tools: {ruff: {enabled: false}}"},
		{name: "disabled rule", shell: "python", script: "print(missing)", config: "lint: {rules: {external: {ruff: off}}}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := "on: push\n" + tc.defaults + "jobs:\n  test:\n    runs-on: ubuntu-latest\n" + tc.job + "    steps:\n      - run: |\n          " + tc.script + "\n"
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
		// Quoted/folded scalar mappings are not yet available to script analyzers;
		// retain the run key rather than manufacture a character position.
		if strings.HasPrefix(script, "\"") || strings.HasPrefix(script, "'") {
			wantColumn = 9
		}
		if finding.Column != wantColumn || finding.code != "F821" {
			t.Fatalf("wrong source mapping: %+v want column %d", finding, wantColumn)
		}
	}
}

// The portable fake process exercises failures even when Ruff is not installed.
func TestRuffHelperProcess(t *testing.T) {
	if os.Getenv("ACTIONLINT_TEST_RUFF") != "1" {
		return
	}
	_, _ = io.Copy(io.Discard, os.Stdin)
	if os.Getenv("ACTIONLINT_TEST_RUFF_WAIT") == "1" {
		time.Sleep(10 * time.Second)
	}
	if !slices.Contains(os.Args, "--isolated") || !slices.Contains(os.Args, "--no-fix") {
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
	options := &ExternalCommandOptions{Executable: &exe, Arguments: []string{"-test.run=^TestRuffHelperProcess$"}, Environment: []string{"ACTIONLINT_TEST_RUFF=1", "ACTIONLINT_TEST_RUFF_WAIT=1"}}
	src := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - shell: python\n        run: print('ok')\n"
	start := time.Now()
	_, err = Analyze(ctx, AnalysisRequest{RuffOptions: options, WorkingDir: t.TempDir(), Sources: []SourceUnit{{Path: "ci.yml", Content: []byte(src)}}})
	if err == nil || time.Since(start) > 3*time.Second {
		t.Fatalf("cancellation did not stop Ruff promptly: %v", err)
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

func TestSanitizePythonScript(t *testing.T) {
	for _, source := range []string{"print(${{ github.sha }})", "print('${{ '}}' }}')", "print(${{\n github.sha\n}})", "print(${{ 'é' }}, missing)"} {
		got, valid := ruff.Sanitize(source, ruffExpressionEnd)
		if !valid || strings.Contains(got, "${{") || len([]rune(got)) != len([]rune(source)) {
			t.Fatalf("%q -> %q, %v", source, got, valid)
		}
		for i, r := range []rune(source) {
			if (r == '\n' || r == '\r') && []rune(got)[i] != r {
				t.Fatal("line break moved")
			}
		}
	}
	if _, valid := ruff.Sanitize("print(${{ missing)", ruffExpressionEnd); valid {
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
			options := &ExternalCommandOptions{Executable: &exe, Arguments: []string{"-test.run=^TestRuffHelperProcess$"}, Environment: []string{"ACTIONLINT_TEST_RUFF=1", "ACTIONLINT_TEST_RUFF_OUTPUT=" + tc.output, "ACTIONLINT_TEST_RUFF_EXIT=" + strconv.Itoa(tc.code)}}
			src := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - shell: python\n        run: print(missing)\n"
			_, err := Analyze(t.Context(), AnalysisRequest{RuffOptions: options, WorkingDir: t.TempDir(), Sources: []SourceUnit{{Path: "ci.yml", Content: []byte(src)}}})
			if (err != nil) != tc.failed {
				t.Fatalf("error=%v, want failed=%v", err, tc.failed)
			}
		})
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
	if len(result.Diagnostics) != 1 || !strings.HasSuffix(result.files[0].errors[0].Filepath, "python-action/action.yml") {
		t.Fatalf("got %+v", result.Diagnostics)
	}
}
