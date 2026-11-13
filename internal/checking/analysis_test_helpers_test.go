package checking

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
	sourcemodel "actionlint.kjanat.dev/v2/internal/input/source"

	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/files"
	workspaceactions "actionlint.kjanat.dev/v2/internal/input/references/actions/workspace"
	workflowinputs "actionlint.kjanat.dev/v2/internal/input/references/workflows"
	"actionlint.kjanat.dev/v2/internal/input/source/capture"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
	"actionlint.kjanat.dev/v2/internal/integrations/process"
	"actionlint.kjanat.dev/v2/internal/model/parsing"
	findingmodel "actionlint.kjanat.dev/v2/internal/results/finding"
)

const commandGoodWorkflow = `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo ok
permissions: {}
`

const commandBadWorkflow = `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo '${{ missing.value }}'
permissions: {}
`

// capturedWorkflowCache is a workflow metadata cache reading from captures.
// checkWithCaches analyzes with the same registry and store, so the cache and
// the analysis see one capture of each document.
type capturedWorkflowCache struct {
	*workflowinputs.Cache
	registry  *parsing.SourceSnapshots
	captures  *capture.Store
	snapshots *capturedSnapshots
}

func newCapturedWorkflowCache(tb testing.TB, root, cwd string, debug io.Writer) *capturedWorkflowCache {
	tb.Helper()
	if cwd == "" {
		cwd = sourceFixtureBase(tb)
	}
	registry, captures, snapshots := newCapturedSnapshots(tb, cwd)
	cache := workflowinputs.NewCache(root, debug)
	cache.SetSources(tb.Context(), captures, cwd)
	return &capturedWorkflowCache{Cache: cache, registry: registry, captures: captures, snapshots: snapshots}
}

// checkWithCaches checks one prepared source against explicitly shared dependency caches.
func checkWithCaches(tb testing.TB, ctx context.Context, request analysisRequest, source sourceUnit, proc *process.ProcessPool, actions *workspaceactions.Cache, workflows *capturedWorkflowCache) ([]*findingmodel.Finding, error) {
	tb.Helper()
	log := analysisLogger{logOut: request.LogWriter, logLevel: request.LogLevel}
	if log.logOut == nil {
		log.logOut = io.Discard
	}
	registrations := configurationRegistrations(request.Analyzers, request.Tools)
	ruleSet, err := ConfigurationRules().WithRegisteredRules(registrations...)
	if err != nil {
		tb.Fatal(err)
	}
	session := analysisSession{analysisLogger: log, ctx: ctx, workingDir: request.WorkingDir, reader: files.New(ctx, request.ReadFile, nil), tools: request.Tools, analyzers: request.Analyzers, loadScriptEngine: request.LoadScriptEngine, ignorePats: request.IgnorePatterns, onRulesCreated: request.OnRulesCreated, rules: ruleSet, ruleRegistrations: registrations}
	session.ignoreSelectors = request.IgnoreSelectors
	if session.workingDir == "" {
		session.workingDir = sourceFixtureBase(tb)
	}
	var declarations *capturedSnapshots
	var workflowCache *workflowinputs.Cache
	if workflows != nil {
		session.snapshots, session.captures, declarations = workflows.registry, workflows.captures, workflows.snapshots
		workflowCache = workflows.Cache
	} else {
		session.snapshots, session.captures, declarations = newCapturedSnapshots(tb, session.workingDir)
	}
	address := source.InputPath
	if address == "" {
		address = source.Path
	}
	address = declarations.Path(address)
	captured, err := declarations.Capture(address, source.Content, sourcemodel.YAML)
	if err != nil {
		return nil, err
	}
	if actions != nil {
		actions.Configure(workspaceactions.Options{Captures: session.captures, Context: ctx, WorkingDir: session.workingDir, Reader: session.reader, CustomReader: request.ReadFile != nil})
	}
	if proc == nil {
		proc = process.NewProcessPool(ctx, 1)
		defer proc.Wait()
	}
	cfg := source.Config
	provenance := source.ConfigProvenance
	if cfg == nil && source.Project != nil {
		cfg = source.Project.Config()
		provenance = source.Project.ConfigProvenance()
	}
	var rules []rule.Descriptor
	findings, _, err := session.check(source.Path, address, captured, source.Project, cfg, provenance, proc, actions, workflowCache, &rules)
	return findings, err
}

// readFixtureSources reads snapshots for tests that supply their own project and config.
func readFixtureSources(tb testing.TB, paths []string, project *workspace.Project, cfg *repoconfig.Config) []sourceUnit {
	tb.Helper()
	if cfg == nil && project != nil {
		cfg = project.Config()
	}
	sources := make([]sourceUnit, 0, len(paths))
	for _, path := range paths {
		content, err := os.ReadFile(path)
		if err != nil {
			tb.Fatal(err)
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

// runTestAnalysis supplies the process pool that production callers must own.
// It invokes application analysis without input selection or rendering.
func runTestAnalysis(ctx context.Context, request analysisRequest) (*analysisResult, error) {
	if request.Processes == nil {
		request.Processes = process.NewProcessPool(ctx, runtime.NumCPU())
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
		lConfig, err = ConfigurationRules().Parse([]byte(config))
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
