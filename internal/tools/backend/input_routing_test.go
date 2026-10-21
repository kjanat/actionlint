package backend

import (
	"context"
	"errors"
	"testing"

	"actionlint.kjanat.dev/analyzer"
	"actionlint.kjanat.dev/diagnostic"
	"actionlint.kjanat.dev/internal/config"
	"actionlint.kjanat.dev/internal/tools"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
	"actionlint.kjanat.dev/rule"
)

type routingRunner func(context.Context, tools.Batch) ([]tools.Finding, error)

func (r routingRunner) Analyze(ctx context.Context, batch tools.Batch) ([]tools.Finding, error) {
	return r(ctx, batch)
}

func TestInputRouteUsesResolvedNativeEligibility(t *testing.T) {
	disabled, enabled := &config.Config{}, &config.Config{}
	disabled.Overrides = []config.Override{{Includes: []string{"other/**"}}}
	site := analyzer.ScriptInput{Action: "owner/action", Input: "source", Language: "javascript"}
	registration := tools.Registration{
		Descriptor: rule.Descriptor{Name: "tool"},
		Enabled: func(c tools.Context) bool {
			if c.ProjectRoot != "" || c.ProjectConfig != nil || len(c.Config.Overrides) != 0 {
				t.Fatal("input eligibility used project planning instead of resolved script configuration")
			}
			return c.Config.Tools.Ruff.Enabled == nil
		},
		Build: func(tools.Context) (tools.Runner, error) {
			return routingRunner(func(_ context.Context, batch tools.Batch) ([]tools.Finding, error) {
				if len(batch.Scripts) != 3 || batch.Scripts[0].Config != disabled {
					t.Fatal("native batch changed")
				}
				return nil, nil
			}), nil
		},
	}
	disabled.Tools.Ruff.Enabled = new(false)
	calls := 0
	backend := routingBackend(func(_ context.Context, request analyzer.ScriptRequest) ([]analyzer.ScriptDiagnostic, error) {
		calls++
		if request.Source != "enabled" {
			t.Fatalf("disabled source reached backend: %q", request.Source)
		}
		return []analyzer.ScriptDiagnostic{{Message: "input", Location: analyzer.ScriptPosition{Row: 1, Column: 1}}}, nil
	})
	registration, err := WithScriptInputs(registration, analyzer.ScriptEngineDescriptor{Rule: registration.Descriptor, ScriptInputs: []analyzer.ScriptInput{site}}, backend)
	if err != nil {
		t.Fatal(err)
	}
	runner, err := registration.Build(tools.Context{ProjectRoot: "repository", ProjectConfig: enabled})
	if err != nil {
		t.Fatal(err)
	}
	findings, err := runner.Analyze(t.Context(), tools.Batch{Scripts: []tools.Script{
		{Input: &site, Text: "disabled", Config: disabled},
		{Text: "unrelated run", Config: enabled},
		{Input: &site, Text: "enabled", Config: enabled, Key: &workflowlang.Pos{Line: 9, Col: 5}},
	}})
	if err != nil || calls != 1 || len(findings) != 1 || findings[0].Script != 2 {
		t.Fatalf("eligibility changed input routing/indices: calls=%d findings=%+v err=%v", calls, findings, err)
	}
	if len(disabled.Overrides) != 1 {
		t.Fatal("eligibility mutated caller configuration")
	}
}

type routingBackend func(context.Context, analyzer.ScriptRequest) ([]analyzer.ScriptDiagnostic, error)

func (b routingBackend) Check(ctx context.Context, request analyzer.ScriptRequest) ([]analyzer.ScriptDiagnostic, error) {
	return b(ctx, request)
}

func TestInputRoutePreservesPartialResultsAndIndices(t *testing.T) {
	nativeFailure, inputFailure := errors.New("native incomplete"), errors.New("input incomplete")
	site := analyzer.ScriptInput{Action: "owner/action", Input: "source", Language: "javascript"}
	descriptor := analyzer.ScriptEngineDescriptor{Rule: rule.Descriptor{Name: "tool"}, Shells: []string{"bash"}, ScriptInputs: []analyzer.ScriptInput{site}}
	nativeCalls, inputCalls := 0, 0
	registration := tools.Registration{Descriptor: descriptor.Rule, RequiresExecution: true, Build: func(tools.Context) (tools.Runner, error) {
		return routingRunner(func(_ context.Context, batch tools.Batch) ([]tools.Finding, error) {
			nativeCalls++
			if len(batch.Scripts) != 2 || batch.Scripts[1].Input != &site {
				t.Fatal("native batch/index context changed")
			}
			return []tools.Finding{{Rule: "tool", Message: "native", Script: 0}}, nativeFailure
		}), nil
	}}
	engine := routingBackend(func(_ context.Context, request analyzer.ScriptRequest) ([]analyzer.ScriptDiagnostic, error) {
		inputCalls++
		if request.Source != "input" || request.Filename != "actionlint-script" {
			t.Fatalf("input route analyzed a native run: %+v", request)
		}
		return []analyzer.ScriptDiagnostic{{Filename: request.Filename, Code: "INPUT", Message: "input", Location: analyzer.ScriptPosition{Row: 1, Column: 1}, EndLocation: analyzer.ScriptPosition{Row: 1, Column: 2}}}, inputFailure
	})
	registration, err := WithScriptInputs(registration, descriptor, engine)
	if err != nil {
		t.Fatal(err)
	}
	descriptor.ScriptInputs[0].Input = "changed"
	registration.ScriptInputs[0].Input = "also-changed"
	if !registration.RequiresExecution {
		t.Fatal("native execution requirement lost")
	}
	runner, err := registration.Build(tools.Context{})
	if err != nil {
		t.Fatal(err)
	}
	findings, err := runner.Analyze(t.Context(), tools.Batch{Scripts: []tools.Script{
		{Text: "run", Execution: tools.Execution{Shell: "bash"}},
		{Input: &site, Text: "input", Key: &workflowlang.Pos{Line: 11, Col: 7}},
	}})
	if !errors.Is(err, nativeFailure) || !errors.Is(err, inputFailure) || nativeCalls != 1 || inputCalls != 1 || len(findings) != 2 {
		t.Fatalf("partial outcomes lost or routes duplicated: native=%d input=%d findings=%+v err=%v", nativeCalls, inputCalls, findings, err)
	}
	if findings[0].Script != 0 || findings[1].Script != 1 || findings[1].Start != (diagnostic.Position{Line: 11, Column: 7}) {
		t.Fatalf("input source/index context lost: %+v", findings)
	}
}
