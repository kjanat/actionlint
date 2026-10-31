package backend

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/documentparse"
	"actionlint.kjanat.dev/v2/internal/fsread"
	"actionlint.kjanat.dev/v2/internal/source/text"
	"actionlint.kjanat.dev/v2/internal/tools"
	"actionlint.kjanat.dev/v2/internal/tools/profile"
	"actionlint.kjanat.dev/v2/internal/tools/profile/mask"
	"actionlint.kjanat.dev/v2/internal/tools/provider/process"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/internal/yaml/codec"
	"actionlint.kjanat.dev/v2/rule"
)

// scriptFixture keeps the scalar's original lines and columns by placing its
// anchor before an appended workflow, whose step runs it through an alias.
func scriptFixture(t *testing.T, input string) tools.Script {
	t.Helper()
	content := strings.Replace(input, "run:", "run: &script", 1) + "\non: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: *script\n"
	document := documentparse.Workflow("fix.yml", []byte(content))
	run, ok := document.Workflow.Jobs["test"].Steps[0].Exec.(*workflowlang.ExecRun)
	if !ok {
		t.Fatal("fixture is not a run step")
	}
	snapshot, err := newCapturedSnapshots(t, sourceFixtureBase(t)).Document("fix.yml", []byte(content), document)
	if err != nil {
		t.Fatal(err)
	}
	workflow, ok := snapshot.WorkflowView()
	if !ok {
		t.Fatal("fixture has no workflow")
	}
	job, ok := workflow.Job("test")
	if !ok {
		t.Fatal("fixture has no job")
	}
	view, ok := slices.Collect(job.Steps())[0].Run()
	if !ok {
		t.Fatal("fixture has no run")
	}
	return tools.Script{Text: run.Run.Value, Source: view.Source(), Key: run.RunPos, Value: run.Run.Pos, Path: "fix.yml", Content: []byte(content), Execution: tools.Execution{Shell: "bash"}}
}

// generated prepares a script as a shell preparation does: a kept shebang,
// then a generated directive and startup line, then the script.
func generated(script string) profile.Prepared {
	var out strings.Builder
	var lines []profile.Line
	row := 0
	write := func(line string, origin profile.LineOrigin) {
		out.WriteString(strings.TrimSuffix(line, "\n"))
		out.WriteString("\n")
		mapped := profile.Line{Origin: origin}
		if origin == profile.LineScript {
			row++
			mapped.Line = row
		}
		lines = append(lines, mapped)
	}
	body := slices.DeleteFunc(strings.SplitAfter(script, "\n"), func(line string) bool { return line == "" })
	if len(body) != 0 && strings.HasPrefix(body[0], "#!") {
		write(body[0], profile.LineScript)
		body = body[1:]
	}
	write("# tool disable=X1", profile.LineDirective)
	write("set -e", profile.LineStartup)
	for _, line := range body {
		write(line, profile.LineScript)
	}
	return profile.Prepared{Source: out.String(), LineMap: func(line int) profile.Line {
		if line < 1 || line > len(lines) {
			return profile.Line{Origin: profile.LineDirective}
		}
		return lines[line-1]
	}}
}

func applyFix(t *testing.T, input string, fix diagnostic.Fix) string {
	t.Helper()
	offset := func(pos diagnostic.Position) int {
		start := 0
		for line := 1; line < pos.Line; line++ {
			i := strings.IndexByte(input[start:], '\n')
			if i < 0 {
				t.Fatalf("edit line outside source: %+v", pos)
			}
			start += i + 1
		}
		line, _, _ := strings.Cut(input[start:], "\n")
		i, ok := text.ByteOffsetAtColumn(line, pos.Column)
		if !ok {
			t.Fatalf("edit column outside source: %+v", pos)
		}
		return start + i
	}
	result := input
	for _, edit := range slices.Backward(fix.Edits) {
		start, end := offset(edit.Start), offset(edit.End)
		result = result[:start] + edit.Replacement + result[end:]
	}
	return result
}

func replacement(row, column int, text string) analyzer.ScriptReplacement {
	return analyzer.ScriptReplacement{Row: row, EndRow: row, Column: column, EndColumn: column, Text: text}
}

func TestScriptFixLiteralMapping(t *testing.T) {
	for _, newline := range []string{"\n", "\r\n"} {
		t.Run("newline="+strings.ReplaceAll(newline, "\r", `\r`), func(t *testing.T) {
			input := strings.ReplaceAll("run: |\n  #!/bin/bash\n  echo\té 🐚 $VALUE\n  echo $OTHER\nnext: unchanged\n", "\n", newline)
			script := scriptFixture(t, input)
			fix, ok := scriptFix(script.Source, generated(script.Source.Value()), analyzer.ScriptFix{Description: "Quote variables", Replacements: []analyzer.ScriptReplacement{
				replacement(4, 10, `"`), replacement(4, 16, `"`), replacement(5, 6, `"`), replacement(5, 12, `"`),
			}})
			if !ok || len(fix.Edits) != 4 || fix.Description != "Quote variables" || fix.Applicability != "suggested" {
				t.Fatalf("mapped fix: %+v", fix)
			}
			changed := applyFix(t, input, fix)
			want := strings.ReplaceAll(strings.ReplaceAll(input, "$VALUE", `"$VALUE"`), "$OTHER", `"$OTHER"`)
			if changed != want {
				t.Fatalf("changed YAML: %q, want %q", changed, want)
			}
			var decoded struct {
				Run  string `yaml:"run"`
				Next string `yaml:"next"`
			}
			root, err := codec.Parse([]byte(changed))
			if err != nil {
				t.Fatal(err)
			}
			session, cursor, err := codec.Open(root)
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			entered, err := cursor.Enter()
			if err != nil {
				t.Fatal(err)
			}
			if err := entered.Decode(&decoded); err != nil {
				t.Fatal(err)
			}
			if decoded.Run != "#!/bin/bash\necho\té 🐚 \"$VALUE\"\necho \"$OTHER\"\n" || decoded.Next != "unchanged" {
				t.Fatalf("decoded YAML changed unexpectedly: %+v", decoded)
			}
		})
	}
}

func TestScriptFixRejectsAmbiguousChanges(t *testing.T) {
	input := "run: |\n  echo $VALUE\n  echo $OTHER\n\n"
	for _, tc := range []struct {
		name         string
		input        string
		replacements []analyzer.ScriptReplacement
	}{
		{"generated config", input, []analyzer.ScriptReplacement{replacement(1, 1, "#")}},
		{"generated startup", input, []analyzer.ScriptReplacement{replacement(2, 1, "#")}},
		{"cross line", input, []analyzer.ScriptReplacement{{Row: 3, EndRow: 4, Column: 1, EndColumn: 3, Text: "echo"}}},
		{"newline insertion", input, []analyzer.ScriptReplacement{replacement(3, 6, "\nevil: key")}},
		{"carriage return", input, []analyzer.ScriptReplacement{replacement(3, 6, "\r")}},
		{"NUL", input, []analyzer.ScriptReplacement{replacement(3, 6, "\x00")}},
		{"invalid UTF8", input, []analyzer.ScriptReplacement{replacement(3, 6, "\xff")}},
		{"indent insertion", input, []analyzer.ScriptReplacement{replacement(3, 1, " ")}},
		{"erase content", input, []analyzer.ScriptReplacement{{Row: 3, EndRow: 3, Column: 1, EndColumn: 12}}},
		{"outside column", input, []analyzer.ScriptReplacement{replacement(3, 99, `"`)}},
		{"outside line", input, []analyzer.ScriptReplacement{replacement(99, 1, `"`)}},
		{"overlap", input, []analyzer.ScriptReplacement{{Row: 3, EndRow: 3, Column: 6, EndColumn: 12, Text: "x"}, {Row: 3, EndRow: 3, Column: 8, EndColumn: 10, Text: "y"}}},
		{"shared insertion", input, []analyzer.ScriptReplacement{replacement(3, 6, `"`), replacement(3, 6, "{")}},
		{"empty line", input, []analyzer.ScriptReplacement{replacement(5, 1, "echo")}},
		{"plain", "run: echo $VALUE\n", []analyzer.ScriptReplacement{replacement(3, 6, `"`)}},
		{"quoted", "run: 'echo $VALUE'\n", []analyzer.ScriptReplacement{replacement(3, 6, `"`)}},
		{"folded", "run: >\n  echo $VALUE\n", []analyzer.ScriptReplacement{replacement(3, 6, `"`)}},
		{"expression", "run: |\n  echo ${{ env.VALUE }} $OTHER\n", []analyzer.ScriptReplacement{replacement(3, 6, `"`)}},
		{"partial fix", input, []analyzer.ScriptReplacement{replacement(3, 6, `"`), replacement(1, 1, "#")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			script := scriptFixture(t, tc.input)
			prepared := generated(mask.Expressions(script.Text))
			if fix, ok := scriptFix(script.Source, prepared, analyzer.ScriptFix{Description: "Fix", Replacements: tc.replacements}); ok {
				t.Fatalf("unsafe fix exposed: %+v", fix)
			}
		})
	}
	// Without a line map the engine saw the masked script itself.
	script := scriptFixture(t, input)
	fix, ok := scriptFix(script.Source, profile.Prepared{Source: mask.Expressions(script.Text)}, analyzer.ScriptFix{Replacements: []analyzer.ScriptReplacement{replacement(1, 6, `"`)}})
	if !ok || len(fix.Edits) != 1 {
		t.Fatalf("identity mapping lost the fix: %+v", fix)
	}
}

// reportingBackend is a process-like backend whose results the test scripts.
type reportingBackend struct {
	directory  string
	result     func(analyzer.ScriptRequest) process.Result
	concurrent bool
	mu         sync.Mutex
	requests   []analyzer.ScriptRequest
	vars       []map[string]string
}

func (b *reportingBackend) Check(ctx context.Context, request analyzer.ScriptRequest) ([]analyzer.ScriptDiagnostic, error) {
	result, err := b.Run(ctx, request, nil)
	return result.Diagnostics, err
}

func (b *reportingBackend) Run(_ context.Context, request analyzer.ScriptRequest, vars map[string]string) (process.Result, error) {
	b.mu.Lock()
	b.requests = append(b.requests, request)
	b.vars = append(b.vars, vars)
	b.mu.Unlock()
	result := b.result(request)
	result.Directory = b.directory
	return result, nil
}

func (b *reportingBackend) Concurrent() bool { return b.concurrent }

func buildRunner(t *testing.T, backend analyzer.ScriptBackend, options engineOptions) tools.Runner {
	t.Helper()
	return buildEngineRunner(t, analyzer.ScriptEngineDescriptor{Rule: rule.Descriptor{Name: "tool"}, Shells: []string{"bash"}}, backend, options)
}

func buildEngineRunner(t *testing.T, descriptor analyzer.ScriptEngineDescriptor, backend analyzer.ScriptBackend, options engineOptions) tools.Runner {
	t.Helper()
	registration, err := registerEngine(descriptor, backend, options)
	if err != nil {
		t.Fatal(err)
	}
	runner, err := registration.Build(tools.Context{})
	if err != nil {
		t.Fatal(err)
	}
	return runner
}

func TestReportedSourceFindings(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "lib.sh"), []byte("echo $x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	backend := &reportingBackend{directory: directory, result: func(analyzer.ScriptRequest) process.Result {
		return process.Result{Sources: []string{"lib.sh"}, Diagnostics: []analyzer.ScriptDiagnostic{
			{Filename: "lib.sh", Source: "lib.sh", Code: "X2086", Severity: "info", Message: "quote it", Location: analyzer.ScriptPosition{Row: 1, Column: 6}, EndLocation: analyzer.ScriptPosition{Row: 1, Column: 8}},
			{Filename: "missing.sh", Source: "missing.sh", Code: "X1", Severity: "warning", Message: "gone", Location: analyzer.ScriptPosition{Row: 3, Column: 1}},
		}}
	}}
	var recorded []string
	script := scriptFixture(t, "run: |\n  source lib.sh\n")
	script.Scope = &tools.Scope{Reader: fsread.New(t.Context(), nil, func(path string) { recorded = append(recorded, path) })}
	script.Execution.Directory, script.Execution.DirectoryKnown = directory, true
	runner := buildRunner(t, backend, engineOptions{findings: findingCapabilities{reportedSources: true, messageTemplate: "tool: {code}:{severity}:{row}:{column}: {message}"}})
	findings, err := runner.Analyze(t.Context(), tools.Batch{Scripts: []tools.Script{script}})
	if err != nil {
		t.Fatal(err)
	}
	if len(backend.requests) != 1 || backend.requests[0].Directory != directory {
		t.Fatalf("process backend did not receive the script directory: %+v", backend.requests)
	}
	if len(findings) != 2 {
		t.Fatalf("findings: %+v", findings)
	}
	read, missing := findings[0], findings[1]
	if read.Message != "tool: X2086:info:1:6: quote it" || read.Start != (diagnostic.Position{Line: 1, Column: 6}) || read.End != (diagnostic.Position{Line: 1, Column: 8}) ||
		!read.RawScript || string(read.Content) != "echo $x\n" || !strings.HasSuffix(read.Path, "lib.sh") || !filepath.IsAbs(read.Path) {
		t.Errorf("reported finding: %+v", read)
	}
	for _, f := range findings {
		if !f.UnavailableSource.Present() || f.UnavailableSource.Provider() != "tool" || f.Suppression == nil || f.Suppression.First != script.Key.Line {
			t.Errorf("reported finding provenance: %+v", f)
		}
	}
	if read.UnavailableSource.Reason() != "not-captured" || missing.UnavailableSource.Reason() != "read-failed" || len(missing.Content) != 0 || missing.End != (diagnostic.Position{}) {
		t.Errorf("unavailable reasons: %q %q %+v", read.UnavailableSource.Reason(), missing.UnavailableSource.Reason(), missing)
	}
	if !slices.ContainsFunc(recorded, func(path string) bool { return strings.HasSuffix(path, "lib.sh") }) {
		t.Errorf("reported source not recorded: %q", recorded)
	}

	undeclared := buildRunner(t, backend, engineOptions{})
	findings, err = undeclared.Analyze(t.Context(), tools.Batch{Scripts: []tools.Script{script}})
	if err == nil || !strings.Contains(err.Error(), "does not declare reported sources") || len(findings) != 0 {
		t.Fatalf("undeclared reported sources accepted: %+v %v", findings, err)
	}
}

// Template effects describe the snippet, so a provider declaring both the
// template contract and reported sources keeps its findings in read files.
func TestReportedSourcesUnderTemplateContract(t *testing.T) {
	directory := t.TempDir()
	at := analyzer.ScriptPosition{Row: 1, Column: 1}
	backend := &reportingBackend{directory: directory, result: func(analyzer.ScriptRequest) process.Result {
		return process.Result{Diagnostics: []analyzer.ScriptDiagnostic{
			{Code: "X1", Severity: "warning", Message: "snippet", Location: at, TemplateEffect: analyzer.TemplateEffectNone},
			{Source: "lib.sh", Code: "X2", Severity: "warning", Message: "read file", Location: at},
			{Source: "lib.sh", Code: "X3", Severity: "warning", Message: "effect", Location: at, TemplateEffect: analyzer.TemplateEffectOpaqueName},
		}}
	}}
	registration, err := registerEngine(analyzer.ScriptEngineDescriptor{Rule: rule.Descriptor{Name: "tool"}, Shells: []string{"bash"}, TemplateContract: analyzer.TemplateEffectsContract}, backend, engineOptions{findings: findingCapabilities{reportedSources: true}})
	if err != nil {
		t.Fatal(err)
	}
	runner, err := registration.Build(tools.Context{})
	if err != nil {
		t.Fatal(err)
	}
	script := scriptFixture(t, "run: |\n  source lib.sh\n")
	script.Execution.Directory, script.Execution.DirectoryKnown = directory, true
	findings, err := runner.Analyze(t.Context(), tools.Batch{Scripts: []tools.Script{script}})
	if err == nil || !strings.Contains(err.Error(), `template effect "opaque-name" for reported source`) {
		t.Errorf("reported source with a template effect accepted: %v", err)
	}
	if len(findings) != 2 || findings[0].Code != "X1" || findings[1].Code != "X2" || !findings[1].RawScript {
		t.Fatalf("findings: %+v", findings)
	}
}

func TestLineOriginsAndMessages(t *testing.T) {
	script := scriptFixture(t, "run: |\n  echo $VALUE\n")
	diagnostics := []analyzer.ScriptDiagnostic{
		{Code: "X1", Severity: "warning", Message: "on directive", Location: analyzer.ScriptPosition{Row: 1, Column: 1}},
		{Code: "X2", Severity: "warning", Message: "on startup", Location: analyzer.ScriptPosition{Row: 2, Column: 1}},
		{Code: "X3", Severity: "error", Message: "startup fails", Location: analyzer.ScriptPosition{Row: 2, Column: 1}},
		{Code: "X4", Severity: "info", Message: "in script", Location: analyzer.ScriptPosition{Row: 3, Column: 6}, EndLocation: analyzer.ScriptPosition{Row: 3, Column: 12},
			Fixes: []analyzer.ScriptFix{{Description: "quote", Replacements: []analyzer.ScriptReplacement{replacement(3, 6, `"`), replacement(3, 12, `"`)}}}},
		{Code: "X5", Severity: "info", Message: "end on startup", Location: analyzer.ScriptPosition{Row: 3, Column: 1}, EndLocation: analyzer.ScriptPosition{Row: 2, Column: 1}},
	}
	backend := &reportingBackend{result: func(analyzer.ScriptRequest) process.Result { return process.Result{Diagnostics: diagnostics} }}
	prepared := generated(script.Text)
	prepared.Vars = map[string]string{"dialect": "bash"}
	runner := buildRunner(t, backend, engineOptions{
		prepare:  func(tools.Script) (profile.Prepared, error) { return prepared, nil },
		filename: "actionlint.sh",
		findings: findingCapabilities{fixes: true, messageTemplate: "{code}:{severity}:{row}:{column}: {message}"},
	})
	findings, err := runner.Analyze(t.Context(), tools.Batch{Scripts: []tools.Script{script}})
	if err == nil || !strings.Contains(err.Error(), "tools.tool.config: X1: on directive") || !strings.Contains(err.Error(), "tools.tool.config: X3: startup fails") || strings.Contains(err.Error(), "X2") {
		t.Fatalf("line origins: %v", err)
	}
	if backend.requests[0].Filename != "actionlint.sh" || backend.vars[0]["dialect"] != "bash" {
		t.Errorf("request %+v vars %v", backend.requests[0], backend.vars[0])
	}
	if len(findings) != 2 {
		t.Fatalf("findings: %+v", findings)
	}
	scriptFinding := findings[0]
	if scriptFinding.Message != "X4:info:1:6: in script" || scriptFinding.Start != (diagnostic.Position{Line: 2, Column: 8}) || scriptFinding.End != (diagnostic.Position{Line: 2, Column: 14}) || len(scriptFinding.Fixes) != 1 {
		t.Errorf("script finding: %+v", scriptFinding)
	}
	if findings[1].Code != "X5" || findings[1].End != (diagnostic.Position{}) {
		t.Errorf("end on a generated line kept: %+v", findings[1])
	}

	undeclared := buildRunner(t, backend, engineOptions{prepare: func(tools.Script) (profile.Prepared, error) { return prepared, nil }})
	if _, err := undeclared.Analyze(t.Context(), tools.Batch{Scripts: []tools.Script{script}}); err == nil || !strings.Contains(err.Error(), "does not declare them") {
		t.Fatalf("undeclared fixes accepted: %v", err)
	}
}

func TestConcurrentProcessChecksKeepScriptOrder(t *testing.T) {
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	backend := &reportingBackend{concurrent: true, result: func(request analyzer.ScriptRequest) process.Result {
		started <- struct{}{}
		<-release
		return process.Result{Diagnostics: []analyzer.ScriptDiagnostic{{Code: request.Source, Message: request.Source, Location: analyzer.ScriptPosition{Row: 1, Column: 1}}}}
	}}
	runner := buildRunner(t, backend, engineOptions{})
	go func() {
		for range 2 {
			select {
			case <-started:
			case <-time.After(10 * time.Second):
				// A serial runner never starts the second check; release it anyway.
			}
		}
		close(release)
	}()
	scripts := []tools.Script{{Text: "first", Execution: tools.Execution{Shell: "bash"}, Key: &workflowlang.Pos{Line: 1, Col: 1}}, {Text: "second", Execution: tools.Execution{Shell: "bash"}, Key: &workflowlang.Pos{Line: 2, Col: 1}}}
	begin := time.Now()
	findings, err := runner.Analyze(t.Context(), tools.Batch{Scripts: scripts})
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(begin) > 5*time.Second {
		t.Fatal("checks ran serially")
	}
	if len(findings) != 2 || findings[0].Message != "first" || findings[1].Message != "second" || findings[0].Script != 0 || findings[1].Script != 1 {
		t.Fatalf("findings out of script order: %+v", findings)
	}
}

// A snippet engine that requests the directory resource receives the
// working directory, and its declared capabilities admit fixes and findings
// in files it read, as a manifest's do for a process provider.
func TestEngineCapabilitiesAdmitFixesSourcesAndDirectory(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "lib.sh"), []byte("echo $x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var requests []analyzer.ScriptRequest
	var engineRead []byte
	backend := routingBackend(func(_ context.Context, request analyzer.ScriptRequest) ([]analyzer.ScriptDiagnostic, error) {
		requests = append(requests, request)
		if request.DirectoryFS == nil {
			return nil, errors.New("no directory grant")
		}
		var err error
		if engineRead, err = fs.ReadFile(request.DirectoryFS, "lib.sh"); err != nil {
			return nil, err
		}
		return []analyzer.ScriptDiagnostic{
			{Filename: request.Filename, Code: "X1", Severity: "info", Message: "quote", Location: analyzer.ScriptPosition{Row: 1, Column: 6}, EndLocation: analyzer.ScriptPosition{Row: 1, Column: 8},
				Fixes: []analyzer.ScriptFix{{Description: "quote", Replacements: []analyzer.ScriptReplacement{replacement(1, 6, `"`), replacement(1, 8, `"`)}}}},
			{Filename: "lib.sh", Source: "lib.sh", Code: "X2", Severity: "info", Message: "sourced", Location: analyzer.ScriptPosition{Row: 1, Column: 6}, EndLocation: analyzer.ScriptPosition{Row: 1, Column: 8}},
		}, nil
	})
	var recorded []string
	script := scriptFixture(t, "run: |\n  echo $x\n")
	script.Execution.Directory, script.Execution.DirectoryKnown = directory, true
	script.Scope = &tools.Scope{Workspace: directory, Reader: fsread.New(t.Context(), nil, func(path string) { recorded = append(recorded, path) })}
	descriptor := analyzer.ScriptEngineDescriptor{Rule: rule.Descriptor{Name: "tool"}, Shells: []string{"bash"}, Fixes: true, ReportedSources: true, Resources: []analyzer.ToolResourceKind{analyzer.ToolDirectory}}
	registration, err := RegisterEngine(descriptor, backend)
	if err != nil {
		t.Fatal(err)
	}
	if !registration.RequiresExecution {
		t.Fatal("an engine requesting the directory needs execution facts")
	}
	// The preparation's excluded codes travel with the request.
	prepare := func(tools.Script) (profile.Prepared, error) {
		return profile.Prepared{Source: "echo $x\n", Excluded: []string{"X9"}}, nil
	}
	runner := buildEngineRunner(t, descriptor, backend, engineOptions{prepare: prepare})
	findings, err := runner.Analyze(t.Context(), tools.Batch{Scripts: []tools.Script{script}})
	if err != nil {
		t.Fatal(err)
	}
	if len(requests) != 1 || requests[0].Directory != directory || !slices.Equal(requests[0].Excluded, []string{"X9"}) {
		t.Fatalf("engine requests %+v", requests)
	}
	if len(findings) != 2 || len(findings[0].Fixes) != 1 || !findings[1].RawScript || !strings.HasSuffix(findings[1].Path, "lib.sh") {
		t.Fatalf("findings %+v", findings)
	}
	// The display copy is the bytes the engine read, and the scope recorded
	// the read for watchers.
	if string(engineRead) != "echo $x\n" || string(findings[1].Content) != string(engineRead) {
		t.Fatalf("engine read %q, finding shows %q", engineRead, findings[1].Content)
	}
	if !slices.Contains(recorded, filepath.Join(directory, "lib.sh")) {
		t.Fatalf("recorded %q", recorded)
	}
}

// directoryEngine is a snippet engine requesting the directory resource that
// reports a finding in source.
func directoryEngine(t *testing.T, source string, requests *[]analyzer.ScriptRequest) tools.Runner {
	t.Helper()
	backend := routingBackend(func(_ context.Context, request analyzer.ScriptRequest) ([]analyzer.ScriptDiagnostic, error) {
		*requests = append(*requests, request)
		return []analyzer.ScriptDiagnostic{{Filename: source, Source: source, Code: "X2", Severity: "info", Message: "sourced", Location: analyzer.ScriptPosition{Row: 1, Column: 1}, EndLocation: analyzer.ScriptPosition{Row: 1, Column: 2}}}, nil
	})
	descriptor := analyzer.ScriptEngineDescriptor{Rule: rule.Descriptor{Name: "tool"}, Shells: []string{"bash"}, ReportedSources: true, Resources: []analyzer.ToolResourceKind{analyzer.ToolDirectory}}
	return buildEngineRunner(t, descriptor, backend, engineOptions{})
}

// A working directory outside the workspace gets no grant: the check fails
// before the engine runs.
func TestDirectoryGrantStaysInWorkspace(t *testing.T) {
	workspace, outside := t.TempDir(), t.TempDir()
	var requests []analyzer.ScriptRequest
	runner := directoryEngine(t, "lib.sh", &requests)
	script := scriptFixture(t, "run: |\n  echo $x\n")
	script.Execution.Directory, script.Execution.DirectoryKnown = outside, true
	script.Scope = &tools.Scope{Workspace: workspace}
	_, err := runner.Analyze(t.Context(), tools.Batch{Scripts: []tools.Script{script}})
	if err == nil || !strings.Contains(err.Error(), "outside workspace root") || len(requests) != 0 {
		t.Fatalf("requests %+v err %v", requests, err)
	}
	// A symlink below the workspace that leaves it is refused as well.
	link := filepath.Join(workspace, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("cannot create symlinks: %v", err)
	}
	script.Execution.Directory = link
	if _, err := runner.Analyze(t.Context(), tools.Batch{Scripts: []tools.Script{script}}); err == nil || len(requests) != 0 {
		t.Fatalf("symlinked directory: requests %+v err %v", requests, err)
	}
}

// A reported source is shown only through the grant: a symlink in the
// working directory pointing out of the workspace is not read, and without
// a grant nothing is reread by path.
func TestReportedSourceDisplayStaysInGrant(t *testing.T) {
	workspace, outside := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var requests []analyzer.ScriptRequest
	runner := directoryEngine(t, "lib.sh", &requests)
	script := scriptFixture(t, "run: |\n  echo $x\n")
	script.Execution.Directory, script.Execution.DirectoryKnown = workspace, true
	script.Scope = &tools.Scope{Workspace: workspace, Reader: fsread.New(t.Context(), nil, nil)}
	if err := os.Symlink(filepath.Join(outside, "secret"), filepath.Join(workspace, "lib.sh")); err == nil {
		findings, err := runner.Analyze(t.Context(), tools.Batch{Scripts: []tools.Script{script}})
		if err != nil || len(findings) != 1 || len(findings[0].Content) != 0 || findings[0].UnavailableSource.Reason() != "read-failed" {
			t.Fatalf("symlinked source: findings %+v err %v", findings, err)
		}
	}
	// A missing working directory gets no grant and is not sent.
	script.Execution.Directory = filepath.Join(workspace, "missing")
	requests = nil
	findings, err := runner.Analyze(t.Context(), tools.Batch{Scripts: []tools.Script{script}})
	if err != nil || len(requests) != 1 || requests[0].Directory != "" || requests[0].DirectoryFS != nil {
		t.Fatalf("missing directory: requests %+v err %v", requests, err)
	}
	if len(findings) != 1 || len(findings[0].Content) != 0 || findings[0].UnavailableSource.Reason() != "read-failed" {
		t.Fatalf("source without a grant: findings %+v", findings)
	}
}

func TestSnippetEnginesGetNoDirectory(t *testing.T) {
	var requests []analyzer.ScriptRequest
	backend := routingBackend(func(_ context.Context, request analyzer.ScriptRequest) ([]analyzer.ScriptDiagnostic, error) {
		requests = append(requests, request)
		return nil, errors.New("partial")
	})
	runner := buildRunner(t, backend, engineOptions{})
	_, err := runner.Analyze(t.Context(), tools.Batch{Scripts: []tools.Script{{Text: "x", Execution: tools.Execution{Shell: "bash", Directory: t.TempDir(), DirectoryKnown: true}}}})
	if err == nil || len(requests) != 1 || requests[0].Directory != "" || requests[0].Filename != "actionlint-script" {
		t.Fatalf("requests %+v err %v", requests, err)
	}
}
