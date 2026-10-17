package application

import (
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	workflowlang "actionlint.kjanat.dev/internal/workflow"
	workflowvisit "actionlint.kjanat.dev/internal/workflow/visit"
	"actionlint.kjanat.dev/internal/workspace"
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

func TestAnalysisDiagnosticProjects(t *testing.T) {
	for _, tc := range []struct {
		name  string
		roots []string
		want  int
	}{
		{"different projects", []string{"first", "second"}, 2},
		{"same project", []string{"first", "first"}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			request := analysisRequest{WorkingDir: root}
			for _, name := range tc.roots {
				project, err := workspace.NewProject(filepath.Join(root, name))
				if err != nil {
					t.Fatal(err)
				}
				request.Sources = append(request.Sources, sourceUnit{
					Path: ".github/workflows/ci.yml", Content: []byte(commandBadWorkflow), Project: project,
				})
			}
			result, err := runTestAnalysis(t.Context(), request)
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Diagnostics) != tc.want {
				t.Fatalf("want %d findings, got %+v", tc.want, result.Diagnostics)
			}
		})
	}
}

func TestAnalysisConcurrentLogRecords(t *testing.T) {
	for _, level := range []LogLevel{LogLevelVerbose, LogLevelDebug} {
		t.Run(fmt.Sprint(level), func(t *testing.T) {
			var log analysisRecordingLog
			request := analysisRequest{}
			for i := range 32 {
				request.Sources = append(request.Sources, sourceUnit{Path: fmt.Sprintf("workflow-%d.yml", i), Content: []byte(commandGoodWorkflow)})
			}
			request.LogWriter, request.LogLevel = &log, level
			result, err := runTestAnalysis(t.Context(), request)
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

type analysisFailingRule struct {
	workflowvisit.Base
	err error
}

func (r *analysisFailingRule) VisitWorkflowPre(*workflowlang.Workflow) error { return r.err }

func TestAnalysisFailurePreservesCauseAndBatchContext(t *testing.T) {
	cause := errors.New("custom analysis failed")
	for _, paths := range [][]string{{"a.yml"}, {"a.yml", "b.yml"}} {
		request := analysisRequest{OnRulesCreated: func([]workflowvisit.Rule) []workflowvisit.Rule {
			return []workflowvisit.Rule{&analysisFailingRule{workflowvisit.NewBase("failing", ""), cause}}
		}}
		for _, path := range paths {
			request.Sources = append(request.Sources, sourceUnit{Path: path, Content: []byte(commandGoodWorkflow)})
		}
		_, err := runTestAnalysis(t.Context(), request)
		if !errors.Is(err, cause) {
			t.Fatalf("analysis lost cause: %v", err)
		}
		legacy := LegacyAnalysisError(err)
		if !errors.Is(legacy, cause) || strings.HasPrefix(legacy.Error(), "fatal error while checking ") != (len(paths) > 1) {
			t.Fatalf("wrong batch error context: %v", legacy)
		}
	}
}
func TestAnalysisEmptySelectionDoesNotBuildRules(t *testing.T) {
	calls := 0
	result, err := runTestAnalysis(t.Context(), analysisRequest{OnRulesCreated: func(r []workflowvisit.Rule) []workflowvisit.Rule { calls++; return r }})
	if err != nil || result == nil || len(result.Diagnostics) != 0 || calls != 0 {
		t.Fatalf("result=%+v err=%v rules=%d", result, err, calls)
	}
}
