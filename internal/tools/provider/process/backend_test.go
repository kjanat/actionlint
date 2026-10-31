package process

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/internal/analyzerhost"
	"actionlint.kjanat.dev/v2/internal/processconfig"
	"actionlint.kjanat.dev/v2/internal/tools/provider"
	"actionlint.kjanat.dev/v2/internal/tools/provider/bundled"
	"actionlint.kjanat.dev/v2/rule"
)

// TestProcessHelper is the tool process of these tests: the test binary run
// with ACTIONLINT_PROCESS_HELPER naming its behavior.
func TestProcessHelper(t *testing.T) {
	mode := os.Getenv("ACTIONLINT_PROCESS_HELPER")
	if mode == "" {
		return
	}
	stdin, err := io.ReadAll(os.Stdin)
	if err != nil {
		os.Exit(2)
	}
	args := os.Args[slices.Index(os.Args, "--")+1:]
	cwd, err := os.Getwd()
	if err != nil {
		os.Exit(2)
	}
	switch mode {
	case "echo":
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"args": args, "stdin": string(stdin), "env": os.Environ(), "cwd": cwd})
	case "envelope":
		var request analyzer.ScriptRequest
		if err := json.Unmarshal(stdin, &request); err != nil {
			os.Exit(2)
		}
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"ok": true, "errors": []string{"partial analysis"}, "diagnostics": []analyzer.ScriptDiagnostic{{
			Filename: request.Filename, Code: "EX1", Message: "envelope " + string(request.Config),
			Location: analyzer.ScriptPosition{Row: 1, Column: 1}, EndLocation: analyzer.ScriptPosition{Row: 1, Column: 8},
		}}})
	case "sarif":
		_, _ = fmt.Fprintf(os.Stdout, `{"version":"2.1.0","runs":[{"tool":{"driver":{"rules":[{"id":"S1","helpUri":"https://example.com/S1"}]}},"results":[{"ruleId":"S1","level":"error","message":{"text":"sarif finding"},"locations":[{"physicalLocation":{"artifactLocation":{"uri":%q},"region":{"startLine":1,"startColumn":3,"endColumn":5}}}]}]}]}`, filepath.Join(cwd, args[0]))
	default:
		os.Exit(2)
	}
	os.Exit(1)
}

func helperCommand(t *testing.T, mode string, env ...string) *analyzerhost.Command {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	pool := analyzerhost.NewProcessPool(t.Context(), 2)
	command, err := pool.ConfiguredCommandRunner("", &processconfig.Options{
		Executable:        &executable,
		LauncherArguments: []string{"-test.run=^TestProcessHelper$"},
		Environment:       append([]string{"ACTIONLINT_PROCESS_HELPER=" + mode}, env...),
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	return command
}

func helperManifest(spec provider.ProcessSpec) provider.Manifest {
	if spec.ExitCodes == nil {
		spec.ExitCodes = []int{0, 1}
	}
	if spec.Config.Render == "" {
		spec.Config.Render = provider.RenderNone
	}
	return provider.Manifest{
		SchemaVersion: provider.SchemaVersion, ID: "example.com/helper", Version: "1", Role: provider.RoleScript,
		Rule: rule.Descriptor{Name: "helper"}, Scope: provider.ScopeSource, Transport: provider.TransportProcess,
		Process: &spec,
	}
}

// echoOutput transforms the echo helper's report into one diagnostic whose
// message is the report and the exit status.
var echoOutput = provider.Output{Format: provider.FormatJSON, Transform: &provider.Transform{
	Language: provider.LanguageJQ,
	Inline:   `[{filename: $vars.filename, code: "ECHO", message: ({report: ., exit_code: $vars.exit_code} | tojson), location: {row: 1, column: 1}}]`,
}}

type echoReport struct {
	Report struct {
		Args  []string `json:"args"`
		Stdin string   `json:"stdin"`
		Env   []string `json:"env"`
		CWD   string   `json:"cwd"`
	} `json:"report"`
	ExitCode string `json:"exit_code"`
}

func runEcho(t *testing.T, backend *Backend, request analyzer.ScriptRequest, vars map[string]string) echoReport {
	t.Helper()
	result, err := backend.Run(t.Context(), request, vars)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("diagnostics: %+v", result.Diagnostics)
	}
	var report echoReport
	if err := json.Unmarshal([]byte(result.Diagnostics[0].Message), &report); err != nil {
		t.Fatal(err)
	}
	return report
}

func TestBackendInvocation(t *testing.T) {
	t.Setenv("ACTIONLINT_HOST_ONLY", "leaked")
	t.Setenv("ACTIONLINT_PASSED", "passed")
	t.Setenv("ACTIONLINT_REDIRECT_AMBIENT", "ambient")
	// Configured environment cannot restore a variable the manifest unsets.
	command := helperCommand(t, "echo", "ACTIONLINT_COMMAND=command", "ACTIONLINT_REDIRECT=command")
	command.Args = []string{"--user", "value"}
	manifest := helperManifest(provider.ProcessSpec{
		Entry: "helper", Stdin: provider.StdinSource,
		Args:           []string{"--", "{user-args}", "{config:toml-pairs:--set}", "--file", "{var:filename}", "{flag:dialect:--shell}", "{list:exclude:,}"},
		Env:            map[string]string{"ACTIONLINT_MANIFEST": "manifest"},
		EnvPassthrough: []string{"ACTIONLINT_PASSED", "ACTIONLINT_REDIRECT_AMBIENT"},
		EnvUnset:       []string{"ACTIONLINT_REDIRECT", "ACTIONLINT_REDIRECT_AMBIENT"},
		Config:         provider.ConfigRendering{Render: provider.RenderTOMLPairs},
		Output:         echoOutput,
	})
	backend, err := NewBackend(manifest, command, Options{})
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	report := runEcho(t, backend, analyzer.ScriptRequest{Source: "echo $x\n", Filename: "actionlint.sh", Config: json.RawMessage(`{"key": ["--fix"]}`), Directory: directory}, map[string]string{"dialect": "bash", "exclude": "SC1\nSC2"})
	want := []string{"--user", "value", "--set", `"key" = ["--fix"]`, "--file", "actionlint.sh", "--shell", "bash", "SC1,SC2"}
	if !slices.Equal(report.Report.Args, want) {
		t.Errorf("args %q, want %q", report.Report.Args, want)
	}
	if report.Report.Stdin != "echo $x\n" || report.ExitCode != "1" {
		t.Errorf("stdin %q exit %q", report.Report.Stdin, report.ExitCode)
	}
	if !sameDirectory(t, report.Report.CWD, directory) {
		t.Errorf("cwd %q, want %q", report.Report.CWD, directory)
	}
	env := strings.Join(report.Report.Env, "\n")
	for _, entry := range []string{"ACTIONLINT_MANIFEST=manifest", "ACTIONLINT_PASSED=passed", "ACTIONLINT_COMMAND=command"} {
		if !slices.ContainsFunc(report.Report.Env, func(e string) bool { return strings.EqualFold(e, entry) }) {
			t.Errorf("child environment lacks %s:\n%s", entry, env)
		}
	}
	if strings.Contains(env, "ACTIONLINT_HOST_ONLY") {
		t.Errorf("host environment leaked into the child:\n%s", env)
	}
	if strings.Contains(env, "ACTIONLINT_REDIRECT") {
		t.Errorf("unset variable reached the child:\n%s", env)
	}
}

func sameDirectory(t *testing.T, a, b string) bool {
	t.Helper()
	left, err := os.Stat(a)
	if err != nil {
		return false
	}
	right, err := os.Stat(b)
	return err == nil && os.SameFile(left, right)
}

func TestBackendDecoders(t *testing.T) {
	for _, tc := range []struct {
		mode   string
		spec   provider.ProcessSpec
		config json.RawMessage
		want   analyzer.ScriptDiagnostic
		err    string
	}{
		{
			mode: "envelope",
			spec: provider.ProcessSpec{Stdin: provider.StdinRequest, Args: []string{"--"}, Config: provider.ConfigRendering{Render: provider.RenderJSONStdin}, Output: provider.Output{Format: provider.FormatEnvelope}},
			// The request carries the configuration the transform rendered.
			config: json.RawMessage(`{"a": 1}`),
			want:   analyzer.ScriptDiagnostic{Filename: "snippet.py", Code: "EX1", Message: `envelope {"a":1}`, Location: position(1, 1), EndLocation: position(1, 8)},
			err:    "partial analysis",
		},
		{
			mode: "sarif",
			spec: provider.ProcessSpec{Stdin: provider.StdinSource, Args: []string{"--", "{var:filename}"}, Output: provider.Output{Format: provider.FormatSARIF}},
			want: analyzer.ScriptDiagnostic{Filename: "snippet.py", Code: "S1", Message: "sarif finding", Severity: "error", URL: "https://example.com/S1", Location: position(1, 3), EndLocation: position(1, 5)},
		},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			backend, err := NewBackend(helperManifest(tc.spec), helperCommand(t, tc.mode), Options{})
			if err != nil {
				t.Fatal(err)
			}
			diagnostics, err := backend.Check(t.Context(), analyzer.ScriptRequest{Source: "print(x)\n", Filename: "snippet.py", Config: tc.config, Directory: t.TempDir()})
			if tc.err == "" && err != nil || tc.err != "" && (err == nil || !strings.Contains(err.Error(), tc.err)) {
				t.Fatalf("err=%v, want %q", err, tc.err)
			}
			if len(diagnostics) != 1 || diagnostics[0].Code != tc.want.Code || diagnostics[0].Message != tc.want.Message || diagnostics[0].Location != tc.want.Location || diagnostics[0].EndLocation != tc.want.EndLocation || diagnostics[0].Filename != tc.want.Filename {
				t.Fatalf("diagnostics=%+v, want %+v", diagnostics, tc.want)
			}
		})
	}
}

func TestBackendRejectsUnlistedExitStatus(t *testing.T) {
	manifest := helperManifest(provider.ProcessSpec{Stdin: provider.StdinSource, Args: []string{"--"}, ExitCodes: []int{0}, Output: echoOutput})
	backend, err := NewBackend(manifest, helperCommand(t, "echo"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backend.Check(t.Context(), analyzer.ScriptRequest{Filename: "a.sh"}); err == nil || !strings.Contains(err.Error(), "status 1") {
		t.Fatalf("accepted an unlisted status: %v", err)
	}
}

// embedded runs manifests against a recording in-process runner.
type embedded struct {
	mu      sync.Mutex
	calls   [][]string
	version string
	output  string
}

func (e *embedded) command(t *testing.T, name string, args ...string) *analyzerhost.Command {
	t.Helper()
	command := analyzerhost.NewProcessPool(t.Context(), 2).EmbeddedCommand(name, func(argv []string, _ string) ([]byte, error) {
		e.mu.Lock()
		defer e.mu.Unlock()
		e.calls = append(e.calls, slices.Clone(argv))
		if slices.Contains(argv, "--version") {
			return []byte(e.version), nil
		}
		return []byte(e.output), nil
	})
	command.Args = args
	return command
}

func (e *embedded) checks() [][]string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return slices.DeleteFunc(slices.Clone(e.calls), func(argv []string) bool { return slices.Contains(argv, "--version") })
}

func ruffBackend(t *testing.T, runner *embedded, options Options, args ...string) *Backend {
	t.Helper()
	options.Programs = bundled.Program
	backend, err := NewBackend(bundledManifest(t, "ruff"), runner.command(t, "ruff", args...), options)
	if err != nil {
		t.Fatal(err)
	}
	return backend
}

func ruffOutput(t *testing.T, directory, filename string) string {
	t.Helper()
	encoded, err := json.Marshal([]map[string]any{{
		"filename": filepath.Join(directory, filename), "code": "F821", "message": "Undefined name `x`",
		"location": map[string]int{"row": 2, "column": 7}, "end_location": map[string]int{"row": 2, "column": 8},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

// Configuration values never become options: each rendered setting is one
// --config argument, and the rule selection one --select=VALUE or
// --ignore=VALUE argument. Ported from the Ruff adapter's native options
// tests.
func TestRuffConfigCannotBecomeCommandOptions(t *testing.T) {
	for _, config := range []string{`{"target-version": "--fix"}`, `{"lint": {"select": ["--fix"]}}`, `{"lint": {"ignore": ["--fix"]}}`} {
		directory := t.TempDir()
		runner := &embedded{version: "ruff 0.17.0\n", output: "[]"}
		backend := ruffBackend(t, runner, Options{Required: true})
		if _, err := backend.Check(t.Context(), analyzer.ScriptRequest{Source: "x", Filename: "actionlint.py", Config: json.RawMessage(config), Directory: directory}); err != nil {
			t.Fatal(err)
		}
		checks := runner.checks()
		if len(checks) != 1 || slices.Contains(checks[0], "--fix") || !slices.Contains(checks[0], "--no-fix") || !slices.ContainsFunc(checks[0], func(arg string) bool { return arg == "--config" || strings.HasSuffix(arg, "=--fix") }) {
			t.Fatalf("%s changed command options: %q", config, checks)
		}
	}
}

func TestRuffConfigRejectsSideEffectsBeforeExecution(t *testing.T) {
	for _, key := range bundledManifest(t, "ruff").Process.Config.Forbidden {
		runner := &embedded{version: "ruff 0.17.0\n", output: "[]"}
		backend := ruffBackend(t, runner, Options{Required: true})
		config, err := json.Marshal(map[string]any{key: nil})
		if err != nil {
			t.Fatal(err)
		}
		_, err = backend.Check(t.Context(), analyzer.ScriptRequest{Filename: "actionlint.py", Config: config, Directory: t.TempDir()})
		if err == nil || !strings.Contains(err.Error(), key) || len(runner.checks()) != 0 {
			t.Fatalf("%s: err=%v checks=%q", key, err, runner.checks())
		}
	}
}

func TestInvalidLauncherFailsBeforeExecution(t *testing.T) {
	for _, launcher := range []struct {
		exe  string
		args []string
	}{
		{"uvx", []string{"--from", "ruff"}},
		{"python", []string{"-c", "import ruff"}},
		{"python3.12", []string{"-m", "other"}},
		{"py.exe", []string{"ruff.py"}},
	} {
		runner := &embedded{version: "ruff 0.17.0\n", output: "[]"}
		backend, err := NewBackend(bundledManifest(t, "ruff"), runner.command(t, launcher.exe, launcher.args...), Options{Programs: bundled.Program})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := backend.Check(t.Context(), analyzer.ScriptRequest{Source: "print(1)", Filename: "actionlint.py", Directory: t.TempDir()}); err == nil || len(runner.calls) != 0 {
			t.Fatalf("%s %q: err=%v calls=%q", launcher.exe, launcher.args, err, runner.calls)
		}
	}
}

func TestRuffLauncherPrecedesManifestArguments(t *testing.T) {
	directory := t.TempDir()
	runner := &embedded{version: "ruff 0.17.0\n", output: ruffOutput(t, directory, "actionlint.py")}
	backend, err := NewBackend(bundledManifest(t, "ruff"), runner.command(t, "python3", "-I", "-m", "ruff", "--preview"), Options{Programs: bundled.Program})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backend.Check(t.Context(), analyzer.ScriptRequest{Source: "\nprint(x)\n", Filename: "actionlint.py", Directory: directory}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(runner.calls[0], []string{"-I", "-m", "ruff", "--version"}) {
		t.Fatalf("version probe argv %q", runner.calls[0])
	}
	check := runner.checks()[0]
	if !slices.Equal(check[:5], []string{"-I", "-m", "ruff", "check", "--preview"}) {
		t.Fatalf("check argv %q", check)
	}
}

func TestRuffOutputIdentityAndEffects(t *testing.T) {
	directory := t.TempDir()
	runner := &embedded{version: "ruff 0.17.0\n", output: ruffOutput(t, directory, "actionlint.py")}
	backend := ruffBackend(t, runner, Options{Required: true})
	diagnostics, err := backend.Check(t.Context(), analyzer.ScriptRequest{Source: "a = 1\r\nprint(x)\n", Filename: "actionlint.py", Directory: directory})
	if err != nil {
		t.Fatal(err)
	}
	// Ruff counts CRLF as one break; the contract keeps the CR on row 1.
	if len(diagnostics) != 1 || diagnostics[0].Filename != "actionlint.py" || diagnostics[0].TemplateEffect != analyzer.TemplateEffectOpaqueName || diagnostics[0].Location != position(2, 7) {
		t.Fatalf("diagnostics=%+v", diagnostics)
	}
	runner.output = ruffOutput(t, t.TempDir(), "actionlint.py")
	if _, err := backend.Check(t.Context(), analyzer.ScriptRequest{Source: "print(x)\n", Filename: "actionlint.py", Directory: directory}); err == nil || !strings.Contains(err.Error(), "unexpected file") {
		t.Fatalf("accepted a finding in another file: %v", err)
	}
}

func TestVersionGate(t *testing.T) {
	for _, tc := range []struct {
		version   string
		required  bool
		available bool
		templates bool
		failure   bool
	}{
		{"ruff 0.17.0\n", false, true, true, false},
		{"ruff 0.17.0\n", true, true, true, false},
		{"ruff 0.17.1\n", false, true, false, false},
		{"ruff 0.17.1\n", true, true, false, true},
		{"ruff 0.16.0\n", false, false, false, false},
		{"ruff 0.16.0\n", true, false, false, true},
	} {
		t.Run(fmt.Sprintf("%s required=%t", strings.TrimSpace(tc.version), tc.required), func(t *testing.T) {
			var warnings []error
			runner := &embedded{version: tc.version, output: "[]"}
			backend := ruffBackend(t, runner, Options{Required: tc.required, Warn: func(err error) { warnings = append(warnings, err) }})
			templates, templateErr := backend.ValidateTemplates(t.Context())
			_, checkErr := backend.Check(t.Context(), analyzer.ScriptRequest{Filename: "actionlint.py", Directory: t.TempDir()})
			_, _ = backend.ValidateTemplates(t.Context())
			if templates != tc.templates || tc.failure != (templateErr != nil) {
				t.Errorf("templates=%t err=%v", templates, templateErr)
			}
			if tc.available != (len(runner.checks()) == 1) || tc.required && !tc.available && checkErr == nil || !tc.required && checkErr != nil {
				t.Errorf("checks=%q err=%v", runner.checks(), checkErr)
			}
			if wantWarnings := !tc.required && !tc.templates; wantWarnings != (len(warnings) == 1) {
				t.Errorf("warnings=%v", warnings)
			}
			if probes := len(runner.calls) - len(runner.checks()); probes != 1 {
				t.Errorf("version probed %d times", probes)
			}
		})
	}
}

func TestBackendRequiresDeclaredReportedSources(t *testing.T) {
	runner := &embedded{output: `{"comments":[{"file":"lib.sh","line":1,"endLine":1,"column":1,"endColumn":2,"level":"info","code":2086,"message":"m"}]}`}
	manifest := bundledManifest(t, "shellcheck")
	manifest.Capabilities.ReportedSources = false
	backend, err := NewBackend(manifest, runner.command(t, "shellcheck"), Options{Programs: bundled.Program})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backend.Check(t.Context(), analyzer.ScriptRequest{Filename: "actionlint.sh", Directory: t.TempDir()}); err == nil || !strings.Contains(err.Error(), "reported sources") {
		t.Fatalf("undeclared reported source accepted: %v", err)
	}
}

func TestShellCheckUserShellDropsDialect(t *testing.T) {
	runner := &embedded{output: `{"comments":[]}`}
	backend, err := NewBackend(bundledManifest(t, "shellcheck"), runner.command(t, "shellcheck", "--shell=sh"), Options{Programs: bundled.Program})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backend.Run(t.Context(), analyzer.ScriptRequest{Filename: "actionlint.sh", Directory: t.TempDir()}, map[string]string{"dialect": "bash", "exclude": "SC1091"}); err != nil {
		t.Fatal(err)
	}
	want := []string{"--shell=sh", "--norc", "-f", "json1", "-e", "SC1091", "-"}
	if checks := runner.checks(); len(checks) != 1 || !slices.Equal(checks[0], want) {
		t.Fatalf("argv %q, want %q", checks, want)
	}
}

// The bundled manifests run the real tools when they are installed.
func TestBundledToolsEndToEnd(t *testing.T) {
	for _, tc := range []struct {
		tool, filename, source, config string
		vars                           map[string]string
		codes                          []string
	}{
		{"ruff", "actionlint.py", "import os\nprint(undefined)\n", `{"lint": {"select": ["F"]}}`, nil, []string{"F401", "F821"}},
		{"shellcheck", "actionlint.sh", "echo $foo\n", "", map[string]string{"dialect": "bash", "exclude": "SC2154"}, []string{"SC2086"}},
	} {
		t.Run(tc.tool, func(t *testing.T) {
			if _, err := exec.LookPath(tc.tool); err != nil {
				t.Skipf("%s is not installed", tc.tool)
			}
			command, err := analyzerhost.NewProcessPool(t.Context(), 2).NewCommandRunner(tc.tool, false)
			if err != nil {
				t.Fatal(err)
			}
			backend, err := NewBackend(bundledManifest(t, tc.tool), command, Options{Programs: bundled.Program, Required: true})
			if err != nil {
				t.Fatal(err)
			}
			request := analyzer.ScriptRequest{Source: tc.source, Filename: tc.filename, Directory: t.TempDir()}
			if tc.config != "" {
				request.Config = json.RawMessage(tc.config)
			}
			result, err := backend.Run(t.Context(), request, tc.vars)
			if err != nil {
				t.Fatal(err)
			}
			var codes []string
			for _, d := range result.Diagnostics {
				if d.Filename != tc.filename {
					t.Errorf("diagnostic in %q", d.Filename)
				}
				codes = append(codes, d.Code)
			}
			if !slices.Equal(codes, tc.codes) {
				t.Fatalf("codes %q, want %q: %+v", codes, tc.codes, result.Diagnostics)
			}
		})
	}
}
