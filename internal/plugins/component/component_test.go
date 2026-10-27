package component_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"actionlint.kjanat.dev/v2/analysis"
	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/plugins/component"
)

func pluginPath(name string) string {
	return filepath.Join("testdata", "plugins", name+".wasm")
}

func loadPlugin(t *testing.T, name string, options component.Options) *component.Plugin {
	t.Helper()
	plugin, err := component.Load(t.Context(), pluginPath(name), options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := plugin.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return plugin
}

func writeRepository(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	files[".git/HEAD"] = "ref: refs/heads/main\n"
	for path, content := range files {
		full := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func analyzeRepository(t *testing.T, root string, plugin analyzer.Analyzer) *analysis.Result {
	t.Helper()
	workflow := filepath.Join(root, ".github", "workflows", "ci.yml")
	result, err := analysis.Files(t.Context(), []string{workflow}, analysis.FileOptions{
		WorkingDir: root, Analyzers: []analyzer.Registration{{Analyzer: plugin}}, SkipProjectConfig: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

const pipeWorkflow = "on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: curl -sSf https://example.com/install.sh | sudo bash\n"

func TestRustPluginReportsPipeToShell(t *testing.T) {
	plugin := loadPlugin(t, "pipe-to-shell", component.Options{})
	descriptor := plugin.Descriptor()
	changed := plugin.Descriptor()
	if len(changed.Examples) != 1 {
		t.Fatal("missing rule examples")
	}
	changed.Examples[0].Before = "mutated"
	changed.OptionsSchema[0] = 'x'
	if plugin.Descriptor().Examples[0].Before == "mutated" || plugin.Descriptor().OptionsSchema[0] == 'x' {
		t.Fatal("descriptor aliases plugin metadata")
	}

	if len(descriptor.Examples) != 1 || descriptor.Category != "security" || descriptor.Provider != "example" || descriptor.Help == "" || len(descriptor.OptionsSchema) == 0 || descriptor.Name != "pipe-to-shell" || strings.Join(factNames(descriptor.Requires), ",") != "scripts,invocations" {
		t.Fatalf("descriptor=%+v", descriptor)
	}
	if plugin.Limits() != (component.Limits{Timeout: component.DefaultTimeout, MemoryPages: component.DefaultMemoryPages}) {
		t.Fatalf("limits=%+v", plugin.Limits())
	}
	root := writeRepository(t, map[string]string{".github/workflows/ci.yml": pipeWorkflow})
	result := analyzeRepository(t, root, plugin)
	if len(result.Diagnostics) != 1 {
		t.Fatalf("diagnostics=%+v", result.Diagnostics)
	}
	finding := result.Diagnostics[0]
	if finding.Reference == nil || finding.Reference.Node == "" || !strings.HasSuffix(finding.Reference.Address, "steps[0]") || finding.Help == "" || finding.Rule != "pipe-to-shell" || finding.Severity != "warning" || !strings.Contains(finding.Message, "into bash") {
		t.Fatalf("finding=%+v", finding)
	}
	if finding.Start != (diagnostic.Position{Line: 6, Column: 62}) || finding.End != (diagnostic.Position{Line: 6, Column: 66}) {
		t.Fatalf("range=%+v-%+v", finding.Start, finding.End)
	}
}

func TestCPluginReportsSudoInWorkflowAndCompositeAction(t *testing.T) {
	plugin := loadPlugin(t, "no-sudo", component.Options{})
	if plugin.Descriptor().Name != "no-sudo" {
		t.Fatalf("descriptor=%+v", plugin.Descriptor())
	}
	root := writeRepository(t, map[string]string{
		".github/workflows/ci.yml":         "on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: ./.github/actions/setup\n      - run: make && sudo make install\n      - run: FOO=\"hello world\" sudo make test\n      - run: |-\n          FOO=\"hello\n          world\" sudo make test\n",
		".github/actions/setup/action.yml": "name: setup\ndescription: setup\nruns:\n  using: composite\n  steps:\n    - run: sudo apt-get install -y jq\n      shell: bash\n",
	})
	result := analyzeRepository(t, root, plugin)
	if len(result.Diagnostics) != 4 {
		t.Fatalf("diagnostics=%+v", result.Diagnostics)
	}
	action, workflow, assignment, multiline := result.Diagnostics[0], result.Diagnostics[1], result.Diagnostics[2], result.Diagnostics[3]
	if !strings.HasSuffix(filepath.ToSlash(action.Path), ".github/actions/setup/action.yml") || action.Start != (diagnostic.Position{Line: 6, Column: 12}) || action.End != (diagnostic.Position{Line: 6, Column: 16}) {
		t.Fatalf("action finding=%+v", action)
	}
	if !strings.HasSuffix(filepath.ToSlash(workflow.Path), ".github/workflows/ci.yml") || workflow.Rule != "no-sudo" || workflow.Start != (diagnostic.Position{Line: 7, Column: 22}) || workflow.End != (diagnostic.Position{Line: 7, Column: 26}) {
		t.Fatalf("workflow finding=%+v", workflow)
	}
	if !strings.HasSuffix(filepath.ToSlash(assignment.Path), ".github/workflows/ci.yml") || assignment.Rule != "no-sudo" || assignment.Start != (diagnostic.Position{Line: 8, Column: 32}) {
		t.Fatalf("assignment-prefixed command finding=%+v", assignment)
	}
	if !strings.HasSuffix(filepath.ToSlash(multiline.Path), ".github/workflows/ci.yml") || multiline.Rule != "no-sudo" || multiline.Start != (diagnostic.Position{Line: 11, Column: 18}) {
		t.Fatalf("multiline assignment finding=%+v", multiline)
	}
}

func factNames(facts []analyzer.Fact) []string {
	names := make([]string, len(facts))
	for i, fact := range facts {
		names[i] = string(fact)
	}
	return names
}

func TestLoadRefusesIncompatibleComponents(t *testing.T) {
	for name, want := range map[string]string{
		"wrong-version":    "plugin exports kjanat:actionlint/analyzer@99.0.0; this host supports kjanat:actionlint/analyzer@0.2.0",
		"forbidden-import": "plugin imports wasi:sockets/tcp-create-socket@0.2.9, which this host does not grant",
		"unknown-import":   "plugin imports example:other/capability@1.0.0, which this host does not grant",
		"no-analyzer":      "plugin does not export kjanat:actionlint/analyzer@0.2.0",
	} {
		path := filepath.Join("testdata", "wat", name+".wasm")
		plugin, err := component.Load(t.Context(), path, component.Options{})
		if err == nil {
			_ = plugin.Close(t.Context())
			t.Fatalf("%s loaded", name)
		}
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("%s: err=%v", name, err)
		}
	}
	if _, err := component.Load(t.Context(), filepath.Join("testdata", "missing.wasm"), component.Options{}); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing: err=%v", err)
	}
	if _, err := component.Load(t.Context(), filepath.Join("testdata", "wat", "no-analyzer.wat"), component.Options{}); err == nil || !strings.Contains(err.Error(), "no-analyzer.wat") {
		t.Fatalf("text: err=%v", err)
	}
}

func misbehave(t *testing.T, plugin *component.Plugin, options string) ([]diagnostic.Diagnostic, error) {
	t.Helper()
	return plugin.Analyze(t.Context(), analyzer.Input{Path: "ci.yml", Options: json.RawMessage(options)})
}

func TestFailuresAreReportedAndIsolated(t *testing.T) {
	var mu sync.Mutex
	var logged []string
	plugin := loadPlugin(t, "misbehave", component.Options{Timeout: 3 * time.Second, MemoryPages: 96, Log: func(line string) {
		mu.Lock()
		defer mu.Unlock()
		logged = append(logged, line)
	}})
	if plugin.Limits().String() != "memory 6 MiB, time 3s" {
		t.Fatalf("limits=%s", plugin.Limits())
	}
	for _, options := range []string{"trap", "panic", "allocate", "exit"} {
		findings, err := misbehave(t, plugin, options)
		if err == nil || findings != nil || !strings.Contains(err.Error(), "plugin misbehave failed (memory 6 MiB, time 3s)") {
			t.Fatalf("%s: findings=%v err=%v", options, findings, err)
		}
		recovered, err := misbehave(t, plugin, "ok")
		if err != nil || len(recovered) != 1 || recovered[0].Code != "MB001" || recovered[0].Message != `options "ok"` {
			t.Fatalf("after %s: findings=%+v err=%v", options, recovered, err)
		}
	}
	if _, err := misbehave(t, plugin, "hang"); !errors.Is(err, context.DeadlineExceeded) || !strings.HasPrefix(err.Error(), "plugin misbehave exceeded its time limit (memory 6 MiB, time 3s): ") {
		t.Fatalf("hang: err=%v", err)
	}
	if _, err := misbehave(t, plugin, "error"); err == nil || !strings.HasSuffix(err.Error(), ": analysis failed on purpose") {
		t.Fatalf("error: err=%v", err)
	}
	if _, err := misbehave(t, plugin, "file"); err == nil || !strings.Contains(err.Error(), "filesystem denied") {
		t.Fatalf("file: err=%v", err)
	}
	if _, err := misbehave(t, plugin, "environment"); err == nil || !strings.HasSuffix(err.Error(), "environment: []") {
		t.Fatalf("environment: err=%v", err)
	}
	findings, err := misbehave(t, plugin, "log")
	if err != nil || len(findings) != 0 {
		t.Fatalf("log: findings=%v err=%v", findings, err)
	}
	mu.Lock()
	defer mu.Unlock()
	log := strings.Join(logged, "\n")
	for _, want := range []string{"plugin panic requested", "memory allocation of 1048576 bytes failed", "analyzing ci.yml\nstdout line\nstderr line"} {
		if !strings.Contains(log, want) {
			t.Fatalf("log lacks %q:\n%s", want, log)
		}
	}
}

func TestCancelledContextStopsAnalysis(t *testing.T) {
	plugin := loadPlugin(t, "misbehave", component.Options{})
	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	_, err := plugin.Analyze(ctx, analyzer.Input{Path: "ci.yml", Options: json.RawMessage("hang")})
	if err == nil || !errors.Is(err, context.Canceled) || time.Since(start) > 5*time.Second {
		t.Fatalf("err=%v after %s", err, time.Since(start))
	}
	cancel()
	if _, err := plugin.Analyze(ctx, analyzer.Input{Path: "ci.yml"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
}

func TestConcurrentCallsAreSerialized(t *testing.T) {
	plugin := loadPlugin(t, "misbehave", component.Options{})
	var wg sync.WaitGroup
	errs := make(chan error, 24)
	for range 8 {
		wg.Go(func() {
			for range 3 {
				findings, err := misbehave(t, plugin, `{"n":1}`)
				if err == nil && (len(findings) != 1 || findings[0].Message != `options "{\"n\":1}"`) {
					err = errors.New("unexpected findings")
				}
				if err != nil {
					errs <- err
				}
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func TestClosedPluginRejectsCalls(t *testing.T) {
	plugin, err := component.Load(t.Context(), pluginPath("misbehave"), component.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := plugin.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := misbehave(t, plugin, "ok"); err == nil {
		t.Fatal("analyze succeeded after close")
	}
}

func TestTinyGoPluginReportsInsecureDownloads(t *testing.T) {
	plugin := loadPlugin(t, "insecure-download", component.Options{})
	if plugin.Descriptor().Name != "insecure-download" {
		t.Fatalf("descriptor=%+v", plugin.Descriptor())
	}
	root := writeRepository(t, map[string]string{
		".github/workflows/ci.yml":         "on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: ./.github/actions/setup\n      - run: wget --no-check-certificate https://example.com/x.sh\n",
		".github/actions/setup/action.yml": "name: setup\ndescription: setup\nruns:\n  using: composite\n  steps:\n    - run: curl -k https://example.com | sh\n      shell: bash\n",
	})
	result := analyzeRepository(t, root, plugin)
	if len(result.Diagnostics) != 2 {
		t.Fatalf("diagnostics=%+v", result.Diagnostics)
	}
	action, workflow := result.Diagnostics[0], result.Diagnostics[1]
	if !strings.HasSuffix(filepath.ToSlash(action.Path), ".github/actions/setup/action.yml") || action.Message != "step disables TLS verification with -k for a download" || action.Start != (diagnostic.Position{Line: 6, Column: 17}) || action.End != (diagnostic.Position{Line: 6, Column: 19}) {
		t.Fatalf("action finding=%+v", action)
	}
	if workflow.Rule != "insecure-download" || workflow.Message != "step disables TLS verification with --no-check-certificate for a download" || workflow.Start != (diagnostic.Position{Line: 7, Column: 19}) || workflow.End != (diagnostic.Position{Line: 7, Column: 41}) {
		t.Fatalf("workflow finding=%+v", workflow)
	}
}

func TestComponentRejectsUndeclaredRulesAndRetainsValidFindings(t *testing.T) {
	plugin := loadPlugin(t, "misbehave", component.Options{})
	findings, err := misbehave(t, plugin, "rule-identities")
	if err == nil || !strings.Contains(err.Error(), `undeclared diagnostic rule "undeclared-check"`) || !strings.Contains(err.Error(), `undeclared diagnostic rule "shellcheck"`) {
		t.Fatalf("findings=%+v err=%v", findings, err)
	}
	if len(findings) != 3 {
		t.Fatalf("valid findings lost: %+v", findings)
	}
	for i, want := range []string{"", "misbehave", ""} {
		if findings[i].Rule != want || findings[i].Code != "MB001" {
			t.Fatalf("finding %d=%+v", i, findings[i])
		}
	}
}

func TestRustPluginDistinguishesLogicalOrFromPipes(t *testing.T) {
	plugin := loadPlugin(t, "pipe-to-shell", component.Options{})
	for _, tt := range []struct {
		script string
		want   int
	}{
		{"curl URL || bash", 0},
		{"curl URL | bash", 1},
		{"false || curl URL | bash", 1},
		{"curl URL || echo ignored | bash", 0},
		{"curl URL | bash || wget URL | sh", 2},
	} {
		t.Run(tt.script, func(t *testing.T) {
			root := writeRepository(t, map[string]string{".github/workflows/ci.yml": "on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: " + tt.script + "\n"})
			result := analyzeRepository(t, root, plugin)
			if len(result.Diagnostics) != tt.want {
				t.Fatalf("diagnostics=%+v, want %d", result.Diagnostics, tt.want)
			}
		})
	}
}

func TestOutputLimitAndPartialLines(t *testing.T) {
	for _, debug := range []bool{false, true} {
		var logged []string
		options := component.Options{}
		if debug {
			options.Log = func(line string) { logged = append(logged, line) }
		}
		plugin := loadPlugin(t, "misbehave", options)
		for _, mode := range []string{"output", "direct-log-output"} {
			if _, err := misbehave(t, plugin, mode); err == nil || !strings.Contains(err.Error(), "plugin output exceeds") {
				t.Fatalf("debug=%v mode=%s: swallowed output failure: %v", debug, mode, err)
			}
		}
		logged = nil
		for range 2 {
			if _, err := misbehave(t, plugin, "partial-log"); err != nil {
				t.Fatal(err)
			}
		}
		if debug && strings.Join(logged, "|") != "partial stdout|partial stdout" {
			t.Fatalf("partial lines: %q", logged)
		}
	}
}

func TestPartialOutcomePreservesFindings(t *testing.T) {
	plugin := loadPlugin(t, "misbehave", component.Options{})
	findings, err := misbehave(t, plugin, "partial")
	if err == nil || !strings.Contains(err.Error(), "analysis incomplete on purpose") {
		t.Fatalf("error=%v", err)
	}
	if len(findings) != 1 || findings[0].Code != "MB001" {
		t.Fatalf("partial findings=%+v", findings)
	}
	result, err := analysis.Analyze(t.Context(), analysis.Request{
		Sources: []analysis.Source{{Path: "ci.yml", Content: []byte(pipeWorkflow)}},
		Options: analysis.Options{Analyzers: []analyzer.Registration{{Analyzer: plugin, Options: json.RawMessage(`"partial"`)}}},
	})
	if err == nil || !strings.Contains(err.Error(), "analysis incomplete on purpose") {
		t.Fatalf("application error=%v", err)
	}
	if result == nil || len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != "MB001" {
		t.Fatalf("application partial result=%+v", result)
	}
}

func TestPartialOutcomeRetainsValidRulesAndAllErrors(t *testing.T) {
	plugin := loadPlugin(t, "misbehave", component.Options{})
	wantMessages := []string{`rule 0: ""`, `rule 2: "misbehave"`, `rule 4: ""`}
	checkError := func(err error) {
		t.Helper()
		if err == nil {
			t.Fatal("missing partial-analysis and rule errors")
		}
		for _, want := range []string{"analysis incomplete on purpose", `undeclared diagnostic rule "undeclared-check"`, `undeclared diagnostic rule "shellcheck"`} {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("error lost %q: %v", want, err)
			}
		}
	}
	for _, options := range []string{"rule-identities-partial", `"rule-identities-partial"`} {
		findings, err := misbehave(t, plugin, options)
		checkError(err)
		if len(findings) != len(wantMessages) {
			t.Fatalf("direct findings=%+v", findings)
		}
		for i, want := range []string{"", "misbehave", ""} {
			if findings[i].Rule != want || findings[i].Code != "MB001" || findings[i].Message != wantMessages[i] {
				t.Fatalf("direct finding %d=%+v", i, findings[i])
			}
		}
	}
	result, err := analysis.Analyze(t.Context(), analysis.Request{
		Sources: []analysis.Source{{Path: "ci.yml", Content: []byte(pipeWorkflow)}},
		Options: analysis.Options{Analyzers: []analyzer.Registration{{Analyzer: plugin, Options: json.RawMessage(`"rule-identities-partial"`)}}},
	})
	checkError(err)
	if result == nil || len(result.Diagnostics) != len(wantMessages) {
		t.Fatalf("application partial result=%+v", result)
	}
	for i, finding := range result.Diagnostics {
		if finding.Rule != "misbehave" || finding.Code != "MB001" || finding.Message != wantMessages[i] {
			t.Fatalf("application finding %d=%+v", i, finding)
		}
	}
}

func TestLoadRejectsShapeBeforeDescribe(t *testing.T) {
	for _, name := range []string{"wrong-signature", "wrong-record", "wrong-enum"} {
		t.Run(name, func(t *testing.T) {
			plugin, err := component.Load(t.Context(), filepath.Join("testdata", "contracts", name+".wasm"), component.Options{})
			if plugin != nil {
				_ = plugin.Close(t.Context())
			}
			if err == nil || !strings.Contains(err.Error(), "incompatible analyzer contract") {
				t.Fatalf("expected rejection before trapping describe: %v", err)
			}
		})
	}
}
