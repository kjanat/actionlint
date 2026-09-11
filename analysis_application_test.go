package actionlint

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/go-cmp/cmp"
)

type analysisRecordingLog struct {
	active  atomic.Int32
	overlap atomic.Bool
	mu      sync.Mutex
	records []string
}

func (w *analysisRecordingLog) Write(p []byte) (int, error) {
	if w.active.Add(1) != 1 {
		w.overlap.Store(true)
	}
	defer w.active.Add(-1)
	runtime.Gosched()
	w.mu.Lock()
	w.records = append(w.records, string(p))
	w.mu.Unlock()
	return len(p), nil
}

func TestAnalysisConcurrentLogRecords(t *testing.T) {
	for _, level := range []LogLevel{LogLevelVerbose, LogLevelDebug} {
		t.Run(fmt.Sprint(level), func(t *testing.T) {
			var log analysisRecordingLog
			request := AnalysisRequest{}
			for i := range 32 {
				request.Sources = append(request.Sources, SourceUnit{Path: fmt.Sprintf("workflow-%d.yml", i), Content: []byte(commandGoodWorkflow)})
			}
			result, err := analyze(t.Context(), request, &log, level)
			if err != nil || len(result.Diagnostics) != 0 {
				t.Fatalf("analysis failed: %v", err)
			}
			if log.overlap.Load() {
				t.Fatal("concurrent checks wrote to the caller's log writer simultaneously")
			}
			seen := map[string]int{}
			for _, record := range log.records {
				if !strings.HasSuffix(record, "\n") || (!strings.HasPrefix(record, "verbose: ") && !strings.HasPrefix(record, "[")) {
					t.Fatalf("incomplete log record: %q", record)
				}
				if path, ok := strings.CutPrefix(record, "verbose: Linting "); ok {
					seen[strings.TrimSuffix(path, "\n")]++
				}
			}
			for _, source := range request.Sources {
				if seen[source.Path] != 1 {
					t.Errorf("%s: want one complete Linting record, got %d", source.Path, seen[source.Path])
				}
			}
		})
	}
}

func TestLinterAnalysisFacades(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".github", "workflows")
	for _, path := range []string{dir, filepath.Join(root, ".git")} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, ".github", "actionlint.yaml"), []byte("config-variables: []\n"), 0600); err != nil {
		t.Fatal(err)
	}
	content := []byte(strings.Replace(commandBadWorkflow, "missing.value", "vars.UNKNOWN", 1))
	first, second := filepath.Join(dir, "a.yml"), filepath.Join(dir, "b.yml")
	for _, path := range []string{first, second} {
		if err := os.WriteFile(path, content, 0600); err != nil {
			t.Fatal(err)
		}
	}
	relFirst, _ := filepath.Rel(root, first)
	relSecond, _ := filepath.Rel(root, second)
	for _, tc := range []struct {
		name     string
		call     func(*Linter) ([]*Error, error)
		paths    []string
		selected []string
	}{
		{"repository", func(l *Linter) ([]*Error, error) { return l.LintRepository("") }, []string{relFirst, relSecond}, []string{first, second}},
		{"directory", func(l *Linter) ([]*Error, error) { return l.LintDir(dir, nil) }, []string{relFirst, relSecond}, []string{first, second}},
		{"files", func(l *Linter) ([]*Error, error) { return l.LintFiles([]string{second, first}, nil) }, []string{relSecond, relFirst}, []string{second, first}},
		{"file", func(l *Linter) ([]*Error, error) { return l.LintFile(first, nil) }, []string{relFirst}, nil},
		{"stdin", func(l *Linter) ([]*Error, error) { return l.LintStdin(strings.NewReader(string(content))) }, []string{first}, nil},
		{"content", func(l *Linter) ([]*Error, error) { return l.Lint(first, content, nil) }, []string{first}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out strings.Builder
			var selected []string
			calls := 0
			l, err := NewLinter(&out, &LinterOptions{WorkingDir: root, StdinFileName: first, OutputFormat: OutputFormatJSON,
				OnFilesSelected: func(paths []string) {
					calls++
					selected = slices.Clone(paths)
					paths[0] = "callback-must-not-change-input.yml"
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			findings, err := tc.call(l)
			if err != nil || len(findings) != len(tc.paths) {
				t.Fatalf("expected one config-dependent finding per workflow: %v, %v", findings, err)
			}
			if !slices.Equal(selected, tc.selected) || (calls == 0) != (tc.selected == nil) || calls > 1 {
				t.Fatalf("selection callback: %d calls with %v; want %v", calls, selected, tc.selected)
			}
			request := AnalysisRequest{WorkingDir: root}
			for i, path := range tc.paths {
				if findings[i].Filepath != path || findings[i].Kind != "expression" {
					t.Fatalf("wrong finding order or path: %v", findings)
				}
				request.Sources = append(request.Sources, SourceUnit{Path: path, Content: content, Config: &Config{ConfigVariables: []string{}}})
			}
			result, err := Analyze(t.Context(), request)
			if err != nil {
				t.Fatal(err)
			}
			var report CheckResult
			if err := json.Unmarshal([]byte(out.String()), &report); err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(result.Diagnostics, report.Diagnostics); diff != "" {
				t.Fatal(diff)
			}
		})
	}
}

func TestLinterEmptySelection(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var out strings.Builder
	selections, rules := 0, 0
	l, err := NewLinter(&out, &LinterOptions{Context: ctx, OutputFormat: OutputFormatJSON,
		OnFilesSelected: func(paths []string) {
			selections++
			if len(paths) != 0 {
				t.Fatal(paths)
			}
		},
		OnRulesCreated: func(r []Rule) []Rule { rules++; return r },
	})
	if err != nil {
		t.Fatal(err)
	}
	findings, err := l.LintFiles(nil, nil)
	if err != nil || findings == nil || len(findings) != 0 || out.Len() != 0 || selections != 1 || rules != 0 {
		t.Fatalf("empty selection performed work: %v, %v, output=%q, selections=%d, rules=%d", findings, err, &out, selections, rules)
	}
}

func TestLinterTemplateRulesAcrossCalls(t *testing.T) {
	var out strings.Builder
	count := 0
	l, err := NewLinter(&out, &LinterOptions{
		Format: "{{range allKinds}}{{.Name}}:{{.Description}}\n{{end}}",
		OnRulesCreated: func([]Rule) []Rule {
			count++
			rule := NewRuleBase(fmt.Sprintf("custom-%d", count), "custom description")
			return []Rule{&rule}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	for i := range 2 {
		out.Reset()
		if _, err := l.Lint("input.yml", []byte(commandGoodWorkflow), nil); err != nil {
			t.Fatal(err)
		}
		for n := 1; n <= i+1; n++ {
			if !strings.Contains(out.String(), fmt.Sprintf("custom-%d:custom description", n)) {
				t.Fatalf("formatter forgot rules from a completed call: %s", &out)
			}
		}
	}
}

type analysisFailingRule struct {
	RuleBase
	err error
}

func (r *analysisFailingRule) VisitWorkflowPre(*Workflow) error { return r.err }

func TestLinterAnalysisFailure(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, path := range []string{"a.yml", "b.yml"} {
		if err := os.WriteFile(path, []byte(commandGoodWorkflow), 0600); err != nil {
			t.Fatal(err)
		}
	}
	cause := errors.New("custom analysis failed")
	for _, paths := range [][]string{{"a.yml"}, {"a.yml", "b.yml"}} {
		var out strings.Builder
		l, err := NewLinter(&out, &LinterOptions{OutputFormat: OutputFormatJSON,
			OnRulesCreated: func([]Rule) []Rule {
				return []Rule{&analysisFailingRule{NewRuleBase("failing", ""), cause}}
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		findings, err := l.LintFiles(paths, nil)
		if !errors.Is(err, cause) || findings != nil || out.Len() != 0 {
			t.Fatalf("failed analysis rendered a report or lost the error: %v, %v, %q", findings, err, &out)
		}
		if strings.HasPrefix(err.Error(), "fatal error while checking ") != (len(paths) > 1) {
			t.Fatal("legacy error context changed:", err)
		}
	}
}

func TestLegacyTextWriteErrors(t *testing.T) {
	for _, format := range []OutputFormat{OutputFormatText, OutputFormatOneline, OutputFormatJSON, OutputFormatJSONL, OutputFormatSARIF, OutputFormatGitHub} {
		l, err := NewLinter(commandFailingIO{}, &LinterOptions{OutputFormat: format})
		if err != nil {
			t.Fatal(err)
		}
		_, err = l.Lint("input.yml", []byte(commandBadWorkflow), nil)
		text := format == OutputFormatText || format == OutputFormatOneline
		if (err == nil) != text {
			t.Fatalf("%s changed its write-error contract: %v", format, err)
		}
	}
	var log strings.Builder
	l, err := NewLinter(io.Discard, &LinterOptions{Verbose: true, LogWriter: &log})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.LintStdin(strings.NewReader(commandGoodWorkflow)); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(log.String(), "verbose: Reading the input from stdin\nverbose: Linting <stdin>\n") {
		t.Fatal("legacy log ordering changed:", &log)
	}
}
