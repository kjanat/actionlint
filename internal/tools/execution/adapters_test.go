package execution

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/diagnostic"
	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/diagnosticpolicy"
	"actionlint.kjanat.dev/v2/internal/documentparse"
	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
	"actionlint.kjanat.dev/v2/internal/tools"
	workflowexecution "actionlint.kjanat.dev/v2/internal/workflow/execution"
	"actionlint.kjanat.dev/v2/internal/workflow/invocation"
)

type partialFactAnalyzer struct {
	t       *testing.T
	failure error
}

type sourceFactAnalyzer struct {
	t    *testing.T
	path string
}

func (a sourceFactAnalyzer) Descriptor() analyzer.Descriptor {
	return analyzer.Descriptor{Name: "shellcheck"}
}

func (a sourceFactAnalyzer) Analyze(_ context.Context, input analyzer.Input) ([]diagnostic.Diagnostic, error) {
	a.t.Helper()
	if err := input.ValidateInvocations(); err != nil {
		a.t.Fatal(err)
	}
	scripts := slices.Concat(input.Scripts, input.Invoked)
	if len(scripts) != 1 || scripts[0].Path != a.path {
		a.t.Fatalf("fact adapter changed source path: %+v", scripts)
	}
	return []diagnostic.Diagnostic{{Rule: "shellcheck", Message: "source finding", Start: scripts[0].Start}}, nil
}

type sourceTool struct {
	t                *testing.T
	path, actionPath string
	content          []byte
}

func (a sourceTool) Analyze(_ context.Context, batch tools.Batch) ([]tools.Finding, error) {
	a.t.Helper()
	if len(batch.Scripts) != 2 {
		a.t.Fatalf("native adapter script count: %d", len(batch.Scripts))
	}
	var findings []tools.Finding
	for i, script := range batch.Scripts {
		if script.Path != a.path || string(script.Content) != string(a.content) || script.Scope == nil || script.Scope.ActionPath != a.actionPath {
			a.t.Fatalf("native adapter changed source deployment: %+v", script)
		}
		findings = append(findings, tools.Finding{Rule: "shellcheck", Message: "source finding", Path: script.Path, Start: diagnostic.Position{Line: script.Value.Line, Column: script.Value.Col}, Script: i, Content: script.Content})
	}
	return findings, nil
}

func nestedActionBatch(t *testing.T, collection Collection, snapshot documentparse.SourceSnapshot, deployments ...Deployment) (invocation.Graph, Collection) {
	t.Helper()
	if len(deployments) == 0 {
		deployments = []Deployment{collection.Scopes[0].Deployment}
	}
	path := filepath.Join(filepath.Dir(snapshot.Reference().Document), "workflow.yml")
	content := []byte("on: push\njobs:\n  Build:\n    runs-on: ubuntu-latest\n    steps:\n" + strings.Repeat("      - uses: $/source\n", len(deployments)))
	document := documentparse.Workflow(path, content)
	registry, _, declarations := newCapturedSnapshots(t, filepath.Dir(path))
	actionPath := snapshot.Capture().Aliases()[0]
	actionContent := snapshot.Content()
	var err error
	snapshot, err = declarations.Document(actionPath, actionContent, documentparse.Action(actionPath, actionPath, actionContent))
	if err != nil {
		t.Fatal(err)
	}
	collection.Declarations = registry
	root, err := declarations.Document(path, content, document)
	if err != nil {
		t.Fatal(err)
	}
	builder, err := invocation.NewBuilder(invocation.EntryWorkflow, root.Reference(), invocation.DefaultBudget())
	if err != nil {
		t.Fatal(err)
	}
	baseScope := *collection.Scopes[0]
	collection.Scopes = []*Scope{{Context: builder.RootContext()}}
	collection.Scripts = nil
	body, _ := snapshot.ActionView()
	workflow, _ := root.WorkflowView()
	job, _ := workflow.Job("build")
	steps := slices.Collect(job.Steps())
	for i, deployment := range deployments {
		site := steps[i].Reference()
		ticket, err := builder.Begin(builder.RootContext(), site, invocation.Facts{Uses: "$/source", HasUses: true, Condition: invocation.ConditionEnabled})
		if err != nil {
			t.Fatal(err)
		}
		owner, admitted := ticket.Context()
		if !admitted {
			t.Fatal("fixture call was not admitted")
		}
		outcome, err := invocation.Expanded(snapshot.Reference())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := builder.Resolve(owner, outcome); err != nil {
			t.Fatal(err)
		}
		scope := baseScope
		scope.Context, scope.Deployment = owner, deployment
		workflowexecution.ActionOrigin(&scope.Paths, owner, deployment.ActionPath())
		collection.Scopes = append(collection.Scopes, &scope)
		for step := range body.Steps() {
			ref := step.Reference()
			id, err := builder.AddScript(owner, ref, invocation.ScriptRun, "")
			if err != nil {
				t.Fatal(err)
			}
			collection.Scripts = append(collection.Scripts, ScriptRequest{Occurrence: id})
		}
		if err := builder.Close(owner); err != nil {
			t.Fatal(err)
		}
	}
	graph, err := builder.Publish()
	if err != nil {
		t.Fatal(err)
	}
	return graph, collection
}

func TestScriptAdaptersPreserveActionDeployment(t *testing.T) {
	for _, nested := range []bool{false, true} {
		for _, mapped := range []bool{false, true} {
			t.Run(map[bool]string{false: "standalone", true: "nested"}[nested]+"/"+map[bool]string{false: "plain", true: "mapped"}[mapped], func(t *testing.T) {
				graph, collection, snapshot, path := actionBatchFixture(t, mapped)
				if nested {
					graph, collection = nestedActionBatch(t, collection, snapshot)
				}
				batch, err := NewBatch(graph, collection)
				if err != nil {
					t.Fatal(err)
				}
				factPath, actionPath := "", filepath.Dir(path)
				if nested {
					factPath = path
				}
				if mapped {
					actionPath = ""
				}
				content := snapshot.Content()
				for name, adapter := range map[string]Analyzer{
					"fact":   FactAnalyzer{Analyzer: sourceFactAnalyzer{t: t, path: factPath}},
					"native": Tool{Runner: sourceTool{t: t, path: path, actionPath: actionPath, content: content}},
				} {
					t.Run(name, func(t *testing.T) {
						for _, disabled := range []bool{false, true} {
							findings, err := adapter.AnalyzeScripts(t.Context(), batch)
							if err != nil || len(findings) != 2 {
								t.Fatalf("adapter findings: %v %v", findings, err)
							}
							for _, finding := range findings {
								if finding.Path != path || string(finding.Source) != string(content) || finding.IgnoreOrigin == nil || finding.IgnoreOrigin.Metadata {
									t.Fatalf("adapter lost display source or changed origin: %+v", finding)
								}
							}
							cfg := &repoconfig.Config{}
							if disabled {
								cfg.Lint.Rules.Disable = []string{"shellcheck"}
							}
							policy := diagnosticpolicy.Policy{Path: "workflow.yml", WorkingDir: filepath.Dir(path), ProjectConfig: cfg}
							filtered, err := diagnosticpolicy.Apply(policy, findings, map[string][]byte{path: content}, map[string]*repoconfig.Config{path: cfg})
							want := 2
							if disabled {
								want = 0
							}
							if err != nil || len(filtered) != want {
								t.Fatalf("display-path policy lookup disabled=%v: findings=%v error=%v", disabled, filtered, err)
							}
						}
					})
				}
			})
		}
	}
}

func (a partialFactAnalyzer) Descriptor() analyzer.Descriptor {
	return analyzer.Descriptor{Name: "test"}
}

func (a partialFactAnalyzer) Analyze(_ context.Context, input analyzer.Input) ([]diagnostic.Diagnostic, error) {
	a.t.Helper()
	if err := input.ValidateInvocations(); err != nil {
		a.t.Fatal(err)
	}
	scripts := slices.Concat(input.Scripts, input.Invoked)
	if len(scripts) != 1 || scripts[0].Text != "echo ok" {
		a.t.Fatalf("changed script facts: %+v", input)
	}
	return []diagnostic.Diagnostic{{Rule: "test", Message: "partial finding", Start: diagnostic.Position{Line: 1, Column: 1}}}, a.failure
}

type partialTool struct {
	t       *testing.T
	failure error
}

func (a partialTool) Analyze(_ context.Context, batch tools.Batch) ([]tools.Finding, error) {
	a.t.Helper()
	if len(batch.Scripts) != 1 || batch.Scripts[0].Text != "echo ok" {
		a.t.Fatalf("changed tool batch: %+v", batch)
	}
	if batch.Scripts[0].Path != "" || batch.Scripts[0].Content != nil {
		a.t.Fatal("workflow script lost the native adapter's root-source fallback")
	}
	return []tools.Finding{{Rule: "test", Message: "partial finding", Start: diagnostic.Position{Line: 1, Column: 1}, Script: 0, Content: batch.Scripts[0].Content}}, a.failure
}

func TestScriptAdaptersPreserveOwnershipAndPartialFindings(t *testing.T) {
	graph, collection := batchFixture(t, false)
	batch, err := NewBatch(graph, collection)
	if err != nil {
		t.Fatal(err)
	}
	declaration, exists := collection.Declarations.Declaration(graph.Scripts()[0].Declaration)
	position, hasPosition := declaration.Pos()
	if !exists || !hasPosition {
		t.Fatal("fixture source absent")
	}
	owner := &findingmodel.Owner{Job: "test", Step: &position}
	origin := &findingmodel.IgnoreOrigin{Job: "test", StepPosition: &position}
	failure := errors.New("partial analysis failure")
	for name, adapter := range map[string]Analyzer{
		"fact": FactAnalyzer{Analyzer: partialFactAnalyzer{t: t, failure: failure}},
		"tool": Tool{Runner: partialTool{t: t, failure: failure}},
	} {
		t.Run(name, func(t *testing.T) {
			findings, err := (Service{Analyzers: []Analyzer{adapter}}).Run(t.Context(), batch)
			if !errors.Is(err, failure) || len(findings) != 1 || findings[0].Message != "partial finding" || !reflect.DeepEqual(findings[0].Owner, owner) || !reflect.DeepEqual(findings[0].IgnoreOrigin, origin) || findings[0].Source != nil {
				t.Fatalf("partial finding lost its independent owner or source: %+v %v", findings, err)
			}
		})
	}
}
