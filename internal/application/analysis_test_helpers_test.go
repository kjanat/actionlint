package application

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"actionlint.kjanat.dev/v2/rule"
	sourcemodel "actionlint.kjanat.dev/v2/source"

	actionlocal "actionlint.kjanat.dev/v2/internal/action/local"
	"actionlint.kjanat.dev/v2/internal/analyzerhost"
	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
	"actionlint.kjanat.dev/v2/internal/fsread"
	"actionlint.kjanat.dev/v2/internal/workflow/calls"
	"actionlint.kjanat.dev/v2/internal/workspace"

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

type capturedWorkflowCache struct {
	*calls.Cache
	snapshots *capturedSnapshots
}

func newCapturedWorkflowCache(t testing.TB, root, cwd string, debug io.Writer) *capturedWorkflowCache {
	t.Helper()
	if cwd == "" {
		cwd = sourceFixtureBase(t)
	}
	snapshots := newCapturedSnapshots(t, cwd)
	cache := calls.NewCache(root, cwd, debug)
	cache.SetSources(t.Context(), snapshots.Sources.Store, cwd)
	return &capturedWorkflowCache{Cache: cache, snapshots: snapshots}
}

// checkWithCaches checks one prepared source against explicitly shared dependency caches.
func checkWithCaches(t testing.TB, ctx context.Context, request analysisRequest, source sourceUnit, proc *analyzerhost.ProcessPool, actions *actionlocal.Cache, workflows *capturedWorkflowCache) ([]*findingmodel.Finding, error) {
	t.Helper()
	log := analysisLogger{logOut: request.LogWriter, logLevel: request.LogLevel}
	if log.logOut == nil {
		log.logOut = io.Discard
	}
	session := analysisSession{analysisLogger: log, ctx: ctx, workingDir: request.WorkingDir, reader: fsread.New(ctx, request.ReadFile, nil), tools: request.Tools, analyzers: request.Analyzers, loadScriptEngine: request.LoadScriptEngine, ignorePats: request.IgnorePatterns, onRulesCreated: request.OnRulesCreated}
	if session.workingDir == "" {
		session.workingDir = sourceFixtureBase(t)
	}
	declarations := newCapturedSnapshots(t, session.workingDir)
	var workflowCache *calls.Cache
	if workflows != nil {
		declarations, workflowCache = workflows.snapshots, workflows.Cache
	}
	session.captures, session.snapshots = declarations.Sources.Store, declarations.SourceSnapshots
	address := source.InputPath
	if address == "" {
		address = source.Path
	}
	address = declarations.Sources.Path(address)
	captured, err := declarations.Sources.Capture(address, source.Content, sourcemodel.YAML)
	if err != nil {
		return nil, err
	}
	if actions != nil {
		actions.Configure(actionlocal.Options{Captures: session.captures, Context: ctx, WorkingDir: session.workingDir, Reader: session.reader, CustomReader: request.ReadFile != nil})
	}
	if proc == nil {
		proc = analyzerhost.NewProcessPool(ctx, 1)
		defer proc.Wait()
	}
	cfg := source.Config
	if cfg == nil && source.Project != nil {
		cfg = source.Project.Config()
	}
	var rules []rule.Descriptor
	findings, _, err := session.check(source.Path, address, captured, source.Project, cfg, proc, actions, workflowCache, &rules)
	return findings, err
}

// readFixtureSources reads snapshots for tests that supply their own project and config.
func readFixtureSources(t testing.TB, paths []string, project *workspace.Project, cfg *repoconfig.Config) []sourceUnit {
	t.Helper()
	if cfg == nil && project != nil {
		cfg = project.Config()
	}
	sources := make([]sourceUnit, 0, len(paths))
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
		sources = append(sources, sourceUnit{Path: display, InputPath: path, Content: content, Project: project, Config: cfg})
	}
	return sources
}

func init() { color.NoColor = true }

// runTestAnalysis supplies the process pool that production callers must own.
// It invokes application analysis without input selection or rendering.
func runTestAnalysis(ctx context.Context, request analysisRequest) (*analysisResult, error) {
	if request.Processes == nil {
		request.Processes = analyzerhost.NewProcessPool(ctx, runtime.NumCPU())
		defer request.Processes.Wait()
	}
	return runAnalysis(ctx, request)
}

// testFindings exposes traversal findings for assertions about their provenance.
// Selection, rendering and invocation ownership are tested through their own APIs.
func testFindings(result *analysisResult, err error) ([]*findingmodel.Finding, error) {
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
	request := analysisRequest{}
	var lConfig *repoconfig.Config
	var err error

	if config != "" {
		lConfig, err = repoconfig.Parse([]byte(config))
		if err != nil {
			t.Fatal(err)
		}
	}
	request.Sources = []sourceUnit{{Path: "test.yaml", InputPath: "test.yaml", Content: []byte(source), Project: nil, Config: lConfig}}
	errs, err := testFindings(runTestAnalysis(t.Context(), request))
	if err != nil {
		t.Fatal(err)
	}
	return errs
}
