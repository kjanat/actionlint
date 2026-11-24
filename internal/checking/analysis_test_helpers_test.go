package checking

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/checking/scriptanalysis/contract"
	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/capturefixture/declarations"
	"actionlint.kjanat.dev/v2/internal/model/rule"
	"actionlint.kjanat.dev/v2/internal/results"

	"actionlint.kjanat.dev/v2/internal/base/capture"
	"actionlint.kjanat.dev/v2/internal/base/files"
	"actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/configuration/rulebinding"
	"actionlint.kjanat.dev/v2/internal/input/references/actions/localactions"
	"actionlint.kjanat.dev/v2/internal/input/references/reusableworkflows"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
	"actionlint.kjanat.dev/v2/internal/integrations/hostprocess"
	"actionlint.kjanat.dev/v2/internal/model/parsing"
	"actionlint.kjanat.dev/v2/internal/results/finding"
	"actionlint.kjanat.dev/v2/internal/results/policy"
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

type capturedWorkflowCache struct {
	*reusableworkflows.MetadataCache
	registry  *parsing.SourceSnapshots
	captures  *capture.Store
	snapshots *declarations.CapturedSnapshots
}

func newCapturedWorkflowCache(tb testing.TB, root, cwd string, debug io.Writer) *capturedWorkflowCache {
	tb.Helper()
	if cwd == "" {
		cwd = sourceFixtureBase(tb)
	}
	registry, captures, snapshots := newCapturedSnapshots(tb, cwd)
	cache := reusableworkflows.NewMetadataCache(root, debug)
	cache.SetSources(tb.Context(), captures, cwd)
	return &capturedWorkflowCache{MetadataCache: cache, registry: registry, captures: captures, snapshots: snapshots}
}

func checkWithCaches(tb testing.TB, ctx context.Context, request analysisRequest, source workspace.InputFile, proc *hostprocess.ProcessPool, actions *localactions.ManifestCache, workflows *capturedWorkflowCache) ([]*finding.Finding, error) {
	tb.Helper()
	log := analysisLogger{logOut: request.LogWriter, logLevel: request.LogLevel}
	if log.logOut == nil {
		log.logOut = io.Discard
	}
	checks := request.Checks
	if checks == nil {
		checks = testChecks(tb, nil, nil)
	}
	session := analysisSession{analysisLogger: log, ctx: ctx, workingDir: request.WorkingDir, reader: files.New(ctx, request.ReadFile, nil), checks: checks, loadScriptEngine: request.LoadScriptEngine, ignorePats: request.IgnorePatterns, onRulesCreated: request.OnRulesCreated}
	session.ignoreSelectors = request.IgnoreSelectors
	if session.workingDir == "" {
		session.workingDir = sourceFixtureBase(tb)
	}
	var fixture *declarations.CapturedSnapshots
	var workflowCache *reusableworkflows.MetadataCache
	if workflows != nil {
		session.snapshots, session.captures, fixture = workflows.registry, workflows.captures, workflows.snapshots
		workflowCache = workflows.MetadataCache
	} else {
		session.snapshots, session.captures, fixture = newCapturedSnapshots(tb, session.workingDir)
	}
	address := source.InputPath
	if address == "" {
		address = source.Path
	}
	address = fixture.Path(address)
	captured, err := fixture.Capture(address, source.Content, sourcetext.YAML)
	if err != nil {
		return nil, err
	}
	if actions != nil {
		actions.Configure(localactions.Options{Captures: session.captures, Context: ctx, WorkingDir: session.workingDir, Reader: session.reader, CustomReader: request.ReadFile != nil})
	}
	if proc == nil {
		proc = hostprocess.NewProcessPool(ctx, 1)
		defer proc.Wait()
	}
	cfg := source.Config
	provenance := source.ConfigProvenance
	if cfg == nil && source.Project != nil {
		cfg = source.Project.Config()
		provenance = source.Project.ConfigProvenance()
	}
	var rules []rule.Descriptor
	findings, _, batch, err := session.check(source.Path, address, captured, source.Project, cfg, provenance, proc, actions, workflowCache, &rules)
	if batch == nil {
		return nil, err
	}
	batch.Findings, batch.InputPath = findings, address
	publication, publishErr := policy.Publish([]policy.Batch{*batch}, session.checkCompletion.incompleteChecks())
	return publication.Findings[0], errors.Join(err, publishErr)
}

func readFixtureSources(tb testing.TB, paths []string, project *workspace.Project, cfg *configuration.Config) []workspace.InputFile {
	tb.Helper()
	if cfg == nil && project != nil {
		cfg = project.Config()
	}
	sources := make([]workspace.InputFile, 0, len(paths))
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
		sources = append(sources, workspace.InputFile{Path: display, InputPath: path, Content: content, Project: project, Config: cfg})
	}
	return sources
}

func testChecks(tb testing.TB, analyzers []results.AnalyzerRegistration, tools []contract.Registration) *registeredChecks {
	tb.Helper()
	checks, err := registerChecks(analyzers, tools)
	if err != nil {
		tb.Fatal(err)
	}
	return checks
}

func runTestAnalysis(ctx context.Context, request analysisRequest) (*analysisResult, error) {
	if request.Checks == nil {
		checks, err := registerChecks(nil, nil)
		if err != nil {
			return nil, err
		}
		request.Checks = checks
	}
	if request.Processes == nil {
		request.Processes = hostprocess.NewProcessPool(ctx, runtime.NumCPU())
		defer request.Processes.Wait()
	}
	return runAnalysis(ctx, request)
}

func testFindings(result *analysisResult, err error) ([]*finding.Finding, error) {
	if err != nil {
		return nil, err
	}
	findings := make([]*finding.Finding, 0, len(result.Diagnostics))
	for _, file := range result.files {
		findings = append(findings, file.errors...)
	}
	return findings, nil
}

func analyzeConfiguredWorkflow(t *testing.T, source, config string) []*finding.Finding {
	t.Helper()
	request := analysisRequest{}
	var lConfig *configuration.Config
	var err error

	if config != "" {
		lConfig, err = ConfigurationRules().ParseConfig([]byte(config))
		if err != nil {
			t.Fatal(err)
		}
	}
	request.Sources = []workspace.InputFile{{Path: "test.yaml", InputPath: "test.yaml", Content: []byte(source), Project: nil, Config: lConfig}}
	errs, err := testFindings(runTestAnalysis(t.Context(), request))
	if err != nil {
		t.Fatal(err)
	}
	return errs
}

func publishTestDiagnostics(tb testing.TB, result *analysisResult) {
	tb.Helper()
	for i := range result.files {
		file := &result.files[i]
		if file.publication != nil {
			continue
		}
		rules := ConfigurationRules()
		registry := rulebinding.NewRegistry(rules)
		cfg := file.source.Config
		scope := registry.Prepare(cfg, cfg, cfg)
		for _, finding := range file.errors {
			finding.Binding = scope.Rule(finding.Rule).ID()
		}
		file.publication = &policy.Batch{Processor: policy.Processor{RuleSet: rules}, Policy: policy.DocumentInputs{Path: file.source.Path, ConfigPath: file.source.InputPath, Content: file.source.Content, Scope: scope, Bindings: registry.Seal()}}
	}
	if err := result.publishDiagnostics(); err != nil {
		tb.Fatal(err)
	}
}
