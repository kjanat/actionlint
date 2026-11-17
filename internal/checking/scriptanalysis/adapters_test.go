package scriptanalysis_test

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/configuration/rulebinding"

	"actionlint.kjanat.dev/v2/internal/checking/scriptanalysis"
	"actionlint.kjanat.dev/v2/internal/checking/scriptanalysis/contract"
	"actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/sourcetext"
	"actionlint.kjanat.dev/v2/internal/model/checkinputs"
	"actionlint.kjanat.dev/v2/internal/model/rule"
	"actionlint.kjanat.dev/v2/internal/results"
	"actionlint.kjanat.dev/v2/internal/results/finding"
	"actionlint.kjanat.dev/v2/internal/results/policy"
)

type partialFactAnalyzer struct {
	t       *testing.T
	failure error
}

type sourceFactAnalyzer struct {
	t    *testing.T
	path string
}

func (a sourceFactAnalyzer) Descriptor() checkinputs.Descriptor {
	return checkinputs.Descriptor{Name: "shellcheck"}
}

func (a sourceFactAnalyzer) AnalyzeDocument(_ context.Context, input checkinputs.AnalyzerInput) ([]results.Diagnostic, error) {
	a.t.Helper()
	if err := input.ValidateInvocations(); err != nil {
		a.t.Fatal(err)
	}
	facts := slices.Concat(input.Scripts, input.Invoked)
	if len(facts) != 1 || facts[0].Path != a.path {
		a.t.Fatalf("fact adapter changed source path: %+v", facts)
	}
	return []results.Diagnostic{{Rule: "shellcheck", Message: "source finding", Start: facts[0].Start}}, nil
}

type sourceTool struct {
	t                *testing.T
	path, actionPath string
	content          []byte
}

func (a sourceTool) Analyze(_ context.Context, batch scriptanalysis.Batch) ([]contract.ToolFinding, error) {
	a.t.Helper()
	if len(batch.Scripts) != 2 {
		a.t.Fatalf("native adapter script count: %d", len(batch.Scripts))
	}
	var findings []contract.ToolFinding
	for i, script := range batch.Scripts {
		if script.Path != a.path || string(script.Content) != string(a.content) || script.Scope == nil || script.Scope.Deployment.ActionPath() != a.actionPath {
			a.t.Fatalf("native adapter changed source deployment: %+v", script)
		}
		findings = append(findings, contract.ToolFinding{Rule: "shellcheck", Message: "source finding", Path: script.Path, Start: *script.Value(), Script: i, Source: results.DiagnosticSource{Content: script.Content}})
	}
	return findings, nil
}

func TestScriptAdaptersPreserveActionDeployment(t *testing.T) {
	t.Parallel()
	rules, err := configuration.NewRuleSet([]rule.Declaration{{
		Name: "shellcheck", Category: "correctness", Recommended: true,
		Scope: rule.SourceConfiguration,
	}})
	if err != nil {
		t.Fatal(err)
	}
	processor := policy.Processor{RuleSet: rules}
	for _, nested := range []bool{false, true} {
		for _, mapped := range []bool{false, true} {
			t.Run(map[bool]string{false: "standalone", true: "nested"}[nested]+"/"+map[bool]string{false: "plain", true: "mapped"}[mapped], func(t *testing.T) {
				t.Parallel()
				graph, collection, snapshot, path := scriptanalysis.ActionBatchFixture(t, mapped)
				if nested {
					graph, collection = scriptanalysis.NestedActionBatch(t, collection, snapshot)
				}
				batch, err := scriptanalysis.NewBatch(graph, collection)
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
				for name, adapter := range map[string]func(*testing.T) scriptanalysis.BatchAnalyzer{
					"fact": func(t *testing.T) scriptanalysis.BatchAnalyzer {
						t.Helper()
						return scriptanalysis.FactAnalyzer{Analyzer: sourceFactAnalyzer{t: t, path: factPath}}
					},
					"native": func(t *testing.T) scriptanalysis.BatchAnalyzer {
						t.Helper()
						return contract.Tool{Runner: sourceTool{t: t, path: path, actionPath: actionPath, content: content}}
					},
				} {
					t.Run(name, func(t *testing.T) {
						t.Parallel()
						adapter := adapter(t)
						for _, disabled := range []bool{false, true} {
							findings, err := adapter.AnalyzeScripts(t.Context(), batch)
							if err != nil || len(findings) != 2 {
								t.Fatalf("adapter findings: %v %v", findings, err)
							}
							for _, finding := range findings {
								if finding.Path != path || string(finding.Source.Content) != string(content) || finding.IgnoreOrigin == nil || finding.IgnoreOrigin.Metadata {
									t.Fatalf("adapter lost display source or changed origin: %+v", finding)
								}
							}
							cfg := &configuration.Config{}
							if disabled {
								cfg.Lint.Rules.Disable = []string{"shellcheck"}
							}
							cfg, err = rules.ResolveRules(configuration.Presets{}, cfg)
							if err != nil {
								t.Fatal(err)
							}
							registry := rulebinding.NewRegistry(rules)
							scope := registry.Prepare(nil, cfg, cfg)
							for _, finding := range findings {
								finding.Binding = scope.Rule(finding.Rule).ID()
							}
							resultPolicy := policy.Policy{Path: "workflow.yml", WorkingDir: filepath.Dir(path), Scope: scope, Bindings: registry.Seal(), SourceBindings: map[string]*rulebinding.Scope{path: scope}}
							published, err := policy.Publish([]policy.Batch{{Processor: processor, Policy: resultPolicy, InputPath: "workflow.yml", Findings: findings, Sources: map[string][]byte{path: content}}}, nil)
							var filtered []*finding.Finding
							if len(published.Findings) > 0 {
								filtered = published.Findings[0]
							}
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

func (a partialFactAnalyzer) Descriptor() checkinputs.Descriptor {
	return checkinputs.Descriptor{Name: "test"}
}

func (a partialFactAnalyzer) AnalyzeDocument(_ context.Context, input checkinputs.AnalyzerInput) ([]results.Diagnostic, error) {
	a.t.Helper()
	if err := input.ValidateInvocations(); err != nil {
		a.t.Fatal(err)
	}
	facts := slices.Concat(input.Scripts, input.Invoked)
	if len(facts) != 1 || facts[0].Text != "echo ok" {
		a.t.Fatalf("changed script facts: %+v", input)
	}
	return []results.Diagnostic{{Rule: "test", Message: "partial finding", Start: sourcetext.Position{Line: 1, Column: 1}}}, a.failure
}

type partialTool struct {
	t       *testing.T
	failure error
}

func (a partialTool) Analyze(_ context.Context, batch scriptanalysis.Batch) ([]contract.ToolFinding, error) {
	a.t.Helper()
	if len(batch.Scripts) != 1 || batch.Scripts[0].Text() != "echo ok" {
		a.t.Fatalf("changed tool batch: %+v", batch)
	}
	if batch.Scripts[0].Path != "" || batch.Scripts[0].Content != nil {
		a.t.Fatal("workflow script lost the native adapter's root-source fallback")
	}
	return []contract.ToolFinding{{Rule: "test", Message: "partial finding", Start: sourcetext.Position{Line: 1, Column: 1}, Script: 0, Source: results.DiagnosticSource{Content: batch.Scripts[0].Content}}}, a.failure
}

func TestScriptAdaptersPreserveOwnershipAndPartialFindings(t *testing.T) {
	t.Parallel()
	graph, collection := scriptanalysis.BatchFixture(t, false)
	batch, err := scriptanalysis.NewBatch(graph, collection)
	if err != nil {
		t.Fatal(err)
	}
	declaration, exists := collection.Declarations.Declaration(graph.Scripts()[0].Declaration)
	position, hasPosition := declaration.Position()
	if !exists || !hasPosition {
		t.Fatal("fixture source absent")
	}
	owner := &finding.Owner{Job: "test", Step: &position}
	origin := &finding.IgnoreOrigin{Job: "test", StepPosition: &position}
	failure := errors.New("partial analysis failure")
	for name, adapter := range map[string]func(*testing.T) scriptanalysis.BatchAnalyzer{
		"fact": func(t *testing.T) scriptanalysis.BatchAnalyzer {
			t.Helper()
			return scriptanalysis.FactAnalyzer{Analyzer: partialFactAnalyzer{t: t, failure: failure}}
		},
		"tool": func(t *testing.T) scriptanalysis.BatchAnalyzer {
			t.Helper()
			return contract.Tool{Runner: partialTool{t: t, failure: failure}}
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			adapter := adapter(t)
			findings, err := (scriptanalysis.Service{Analyzers: []scriptanalysis.BatchAnalyzer{adapter}}).Run(t.Context(), batch)
			if !errors.Is(err, failure) || len(findings) != 1 || findings[0].Message != "partial finding" || !reflect.DeepEqual(findings[0].Owner, owner) || !reflect.DeepEqual(findings[0].IgnoreOrigin, origin) || findings[0].Source.Content != nil {
				t.Fatalf("partial finding lost its independent owner or source: %+v %v", findings, err)
			}
		})
	}
}

func TestUnpreparedBatchCannotExecute(t *testing.T) {
	t.Parallel()
	for name, adapter := range map[string]func(*testing.T) scriptanalysis.BatchAnalyzer{
		"fact": func(t *testing.T) scriptanalysis.BatchAnalyzer {
			t.Helper()
			return scriptanalysis.FactAnalyzer{Analyzer: partialFactAnalyzer{t: t}}
		},
		"tool": func(t *testing.T) scriptanalysis.BatchAnalyzer {
			t.Helper()
			return contract.Tool{Runner: partialTool{t: t}}
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			adapter := adapter(t)
			if _, err := adapter.AnalyzeScripts(t.Context(), scriptanalysis.Batch{}); !errors.Is(err, scriptanalysis.ErrInvalidBatch) {
				t.Fatalf("adapter accepted unprepared batch: %v", err)
			}
			if _, err := (scriptanalysis.Service{Analyzers: []scriptanalysis.BatchAnalyzer{adapter}}).Run(t.Context(), scriptanalysis.Batch{}); !errors.Is(err, scriptanalysis.ErrInvalidBatch) {
				t.Fatalf("scheduler accepted unprepared batch: %v", err)
			}
		})
	}
}
