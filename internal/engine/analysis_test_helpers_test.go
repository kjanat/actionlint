package engine

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	actionlocal "actionlint.kjanat.dev/internal/action/local"
	"actionlint.kjanat.dev/internal/analyzerhost"
	repoconfig "actionlint.kjanat.dev/internal/config"
	findingmodel "actionlint.kjanat.dev/internal/finding"
	"actionlint.kjanat.dev/internal/fsread"
	"actionlint.kjanat.dev/internal/workflow/calls"
	"actionlint.kjanat.dev/internal/workspace"

	"github.com/fatih/color"
)

const commandGoodWorkflow = `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo ok
`

const commandBadWorkflow = `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo '${{ missing.value }}'
`

// checkWithCaches checks one prepared source against explicitly shared dependency caches.
func checkWithCaches(ctx context.Context, request Request, source SourceUnit, proc *analyzerhost.ProcessPool, actions *actionlocal.Cache, workflows *calls.Cache) ([]*findingmodel.Finding, error) {
	log := analysisLogger{logOut: request.LogWriter, logLevel: request.LogLevel}
	if log.logOut == nil {
		log.logOut = io.Discard
	}
	engine := analysisEngine{analysisLogger: log, ctx: ctx, workingDir: request.WorkingDir, reader: fsread.New(ctx, request.ReadFile, nil), buildRules: registeredFactory(request, log), ignorePats: request.IgnorePatterns, onRulesCreated: request.OnRulesCreated}
	if proc == nil {
		proc = analyzerhost.NewProcessPool(ctx, 1)
		defer proc.Wait()
	}
	cfg := source.Config
	if cfg == nil && source.Project != nil {
		cfg = source.Project.Config()
	}
	var rules []RuleInfo
	findings, _, err := engine.check(source.Path, source.Content, source.Project, cfg, proc, actions, workflows, &rules)
	return findings, err
}

// readFixtureSources reads snapshots for tests that supply their own project and config.
func readFixtureSources(t testing.TB, paths []string, project *workspace.Project, cfg *repoconfig.Config) []SourceUnit {
	t.Helper()
	if cfg == nil && project != nil {
		cfg = project.Config()
	}
	sources := make([]SourceUnit, 0, len(paths))
	for _, path := range paths {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		display := path
		if project != nil && filepath.IsAbs(path) {
			if relative, err := filepath.Rel(project.RootDir(), path); err == nil {
				display = relative
			}
		}
		sources = append(sources, SourceUnit{Path: display, InputPath: path, Content: content, Project: project, Config: cfg})
	}
	return sources
}

func init() { color.NoColor = true }

// runTestAnalysis supplies the process pool that production callers must own.
// It invokes the actual engine request API without selection or rendering.
func runTestAnalysis(ctx context.Context, request Request) (*AnalysisResult, error) {
	if request.Processes == nil {
		request.Processes = analyzerhost.NewProcessPool(ctx, runtime.NumCPU())
		defer request.Processes.Wait()
	}
	return Run(ctx, request)
}

// testFindings exposes traversal findings for assertions about their provenance.
// Selection, rendering and invocation ownership are tested through their own APIs.
func testFindings(result *AnalysisResult, err error) ([]*findingmodel.Finding, error) {
	if err != nil {
		return nil, err
	}
	findings := make([]*findingmodel.Finding, 0, len(result.Diagnostics))
	for _, file := range result.files {
		findings = append(findings, file.errors...)
	}
	return findings, nil
}

func analyzeConfiguredWorkflow(t *testing.T, source, config string) []*findingmodel.Finding {
	t.Helper()
	request := Request{}
	var lConfig *repoconfig.Config
	var err error

	if config != "" {
		lConfig, err = repoconfig.Parse([]byte(config))
		if err != nil {
			t.Fatal(err)
		}
	}
	request.Sources = []SourceUnit{{Path: "test.yaml", InputPath: "test.yaml", Content: []byte(source), Project: nil, Config: lConfig}}
	errs, err := testFindings(runTestAnalysis(t.Context(), request))
	if err != nil {
		t.Fatal(err)
	}
	return errs
}
