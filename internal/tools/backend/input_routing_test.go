package backend

import (
	"context"
	"errors"
	"strings"
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
				if len(batch.Scripts) != 0 {
					t.Fatal("input-only descriptor reached native runner with unsupported run")
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

func TestInputRouteKeepsPreparedConfigurationFailuresSeparate(t *testing.T) {
	site := analyzer.ScriptInput{Action: "owner/action", Input: "source", Language: "sh"}
	registration := tools.Registration{
		Descriptor: rule.Descriptor{Name: "tool"},
		PrepareInput: func(script tools.Script) (tools.PreparedInput, error) {
			return tools.PreparedInput{Source: "# options\n" + script.Text, Config: []byte(`{"shell":"sh"}`), OriginalLine: func(line int) int {
				if line < 2 {
					return 0
				}
				return line - 1
			}}, nil
		},
		Build: func(tools.Context) (tools.Runner, error) {
			return routingRunner(func(context.Context, tools.Batch) ([]tools.Finding, error) {
				return []tools.Finding{{Message: "native"}}, nil
			}), nil
		},
	}
	engine := routingBackend(func(_ context.Context, request analyzer.ScriptRequest) ([]analyzer.ScriptDiagnostic, error) {
		if request.Source != "# options\ninput" || string(request.Config) != `{"shell":"sh"}` {
			t.Fatalf("prepared request changed: %+v", request)
		}
		return []analyzer.ScriptDiagnostic{
			{Message: "bad configuration", Location: analyzer.ScriptPosition{Row: 1, Column: 1}, EndLocation: analyzer.ScriptPosition{Row: 1, Column: 2}},
			{Message: "input", Location: analyzer.ScriptPosition{Row: 2, Column: 1}, EndLocation: analyzer.ScriptPosition{Row: 2, Column: 2}},
			{Message: "input without end", Location: analyzer.ScriptPosition{Row: 2, Column: 1}},
		}, nil
	})
	registration, err := WithScriptInputs(registration, analyzer.ScriptEngineDescriptor{Rule: registration.Descriptor, Shells: []string{"bash"}, ScriptInputs: []analyzer.ScriptInput{site}}, engine)
	if err != nil {
		t.Fatal(err)
	}
	runner, err := registration.Build(tools.Context{})
	if err != nil {
		t.Fatal(err)
	}
	findings, err := runner.Analyze(t.Context(), tools.Batch{Scripts: []tools.Script{{Input: &site, Text: "input", Key: &workflowlang.Pos{Line: 11, Col: 7}}, {Text: "run", Execution: tools.Execution{Shell: "bash"}}}})
	if err == nil || !strings.Contains(err.Error(), "configuration diagnostic: bad configuration") || strings.Contains(err.Error(), "input without end") || len(findings) != 3 || findings[0].Message != "native" || findings[1].Message != "input" || findings[2].Message != "input without end" || findings[2].Start != (diagnostic.Position{Line: 11, Column: 7}) {
		t.Fatalf("prepared configuration failure lost partial findings or became a source diagnostic: findings=%+v err=%v", findings, err)
	}
}

func TestInputRoutePreservesPartialResultsAndIndices(t *testing.T) {
	nativeFailure, inputFailure := errors.New("native incomplete"), errors.New("input incomplete")
	site := analyzer.ScriptInput{Action: "owner/action", Input: "source", Language: "javascript"}
	descriptor := analyzer.ScriptEngineDescriptor{Rule: rule.Descriptor{Name: "tool"}, Shells: []string{"bash"}, ScriptInputs: []analyzer.ScriptInput{site}}
	nativeCalls, inputCalls := 0, 0
	registration := tools.Registration{Descriptor: descriptor.Rule, RequiresExecution: true, Build: func(tools.Context) (tools.Runner, error) {
		return routingRunner(func(_ context.Context, batch tools.Batch) ([]tools.Finding, error) {
			nativeCalls++
			if len(batch.Scripts) != 1 || batch.Scripts[0].Input != nil {
				t.Fatal("native batch/index context changed")
			}
			return []tools.Finding{{Rule: "tool", Message: "native", Script: 0}}, nativeFailure
		}), nil
	}}
	engine := routingBackend(func(_ context.Context, request analyzer.ScriptRequest) ([]analyzer.ScriptDiagnostic, error) {
		inputCalls++
		if request.Source != "input" || request.Filename != inputFilename(site) {
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

func TestInputFilterCannotHideForeignSource(t *testing.T) {
	site := analyzer.ScriptInput{Action: "owner/action", Input: "source", Language: "python"}
	registration := tools.Registration{Descriptor: rule.Descriptor{Name: "tool"}, PrepareInput: func(script tools.Script) (tools.PreparedInput, error) {
		return tools.PreparedInput{Source: script.Text, AcceptDiagnostic: func(analyzer.ScriptDiagnostic) bool { return false }}, nil
	}, Build: func(tools.Context) (tools.Runner, error) { return routingEmpty{}, nil }}
	engine := routingBackend(func(context.Context, analyzer.ScriptRequest) ([]analyzer.ScriptDiagnostic, error) {
		return []analyzer.ScriptDiagnostic{{Filename: "foreign.py", Code: "F821", Message: "synthetic"}}, nil
	})
	registration, err := WithScriptInputs(registration, analyzer.ScriptEngineDescriptor{ScriptInputs: []analyzer.ScriptInput{site}}, engine)
	if err != nil {
		t.Fatal(err)
	}
	runner, err := registration.Build(tools.Context{})
	if err != nil {
		t.Fatal(err)
	}
	findings, err := runner.Analyze(t.Context(), tools.Batch{Scripts: []tools.Script{{Input: &site, Text: "opaque"}}})
	if len(findings) != 0 || err == nil || !strings.Contains(err.Error(), "unrelated source") {
		t.Fatalf("sanitizer hid invalid source identity: findings=%+v err=%v", findings, err)
	}
}

type describedRoutingBackend struct {
	analyzer.ScriptBackend
	descriptor analyzer.ScriptEngineDescriptor
}

func (b describedRoutingBackend) ScriptDescriptor() analyzer.ScriptEngineDescriptor {
	return b.descriptor
}

func TestConfiguredRoutesValidateEveryScopeOnceWithoutRuns(t *testing.T) {
	first, second := &config.Config{}, &config.Config{}
	site := analyzer.ScriptInput{Action: "owner/action", Input: "code", Language: "python"}
	checks, nativeCalls := 0, 0
	engine := describedRoutingBackend{ScriptBackend: routingBackend(func(context.Context, analyzer.ScriptRequest) ([]analyzer.ScriptDiagnostic, error) {
		checks++
		return nil, nil
	}), descriptor: analyzer.ScriptEngineDescriptor{ScriptInputs: []analyzer.ScriptInput{site}}}
	registration := WithConfiguredEngine(tools.Registration{Descriptor: rule.Descriptor{Name: "tool"}, Build: func(tools.Context) (tools.Runner, error) {
		return routingRunner(func(_ context.Context, batch tools.Batch) ([]tools.Finding, error) {
			nativeCalls++
			if len(batch.Scopes) != 3 || len(batch.Scripts) != 0 {
				t.Fatalf("scope validation changed: %+v", batch)
			}
			return nil, nil
		}), nil
	}}, func(tools.Context) (analyzer.ScriptBackend, error) { return engine, nil })
	runner, err := registration.Build(tools.Context{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = runner.Analyze(t.Context(), tools.Batch{Scopes: []*tools.Scope{{Config: first}, {Config: second}, {}}, Scripts: []tools.Script{{Input: &site, Text: "first", Config: first}, {Input: &site, Text: "second", Config: second}}})
	if err != nil || nativeCalls != 1 || checks != 2 {
		t.Fatalf("scope/input routing counts: native=%d check=%d err=%v", nativeCalls, checks, err)
	}
	_, err = runner.Analyze(t.Context(), tools.Batch{Scopes: []*tools.Scope{{Config: first}, {Config: second}, {}}})
	if err != nil || nativeCalls != 2 || checks != 2 {
		t.Fatalf("scope-only batch skipped validation or called guest: native=%d check=%d err=%v", nativeCalls, checks, err)
	}
}

func TestConfiguredRouteRetainsExplicitShellWithoutExecutionFacts(t *testing.T) {
	calls := 0
	engine := describedRoutingBackend{ScriptBackend: routingBackend(func(context.Context, analyzer.ScriptRequest) ([]analyzer.ScriptDiagnostic, error) {
		t.Fatal("native route bypassed adapter")
		return nil, nil
	}), descriptor: analyzer.ScriptEngineDescriptor{Shells: []string{"python"}}}
	registration := WithConfiguredEngine(tools.Registration{Descriptor: rule.Descriptor{Name: "tool"}, Build: func(tools.Context) (tools.Runner, error) {
		return routingRunner(func(_ context.Context, batch tools.Batch) ([]tools.Finding, error) {
			calls++
			if len(batch.Scripts) != 1 || batch.Scripts[0].Text != "print(1)" {
				t.Fatalf("explicit Python shell lost without execution facts: %+v", batch.Scripts)
			}
			return nil, nil
		}), nil
	}}, func(tools.Context) (analyzer.ScriptBackend, error) { return engine, nil })
	runner, err := registration.Build(tools.Context{})
	if err != nil {
		t.Fatal(err)
	}
	python, unadvertised := "python", "bash"
	_, err = runner.Analyze(t.Context(), tools.Batch{Scripts: []tools.Script{{Text: "print(1)", Shell: &python}, {Text: "echo no", Shell: &unadvertised}}})
	if err != nil || calls != 1 {
		t.Fatalf("native route calls=%d err=%v", calls, err)
	}
}

func TestConfigureBindsLoaderOnce(t *testing.T) {
	loads, nativeCalls := 0, 0
	registration := tools.Registration{Descriptor: rule.Descriptor{Name: "tool"}, ConfiguredBackend: func(tools.Context) (analyzer.ScriptBackend, error) {
		loads++
		return routingBackend(func(context.Context, analyzer.ScriptRequest) ([]analyzer.ScriptDiagnostic, error) {
			t.Fatal("native adapter bypassed")
			return nil, nil
		}), nil
	}, Build: func(tools.Context) (tools.Runner, error) {
		return routingRunner(func(context.Context, tools.Batch) ([]tools.Finding, error) { nativeCalls++; return nil, nil }), nil
	}}
	registration = Configure(Configure(registration))
	runner, err := registration.Build(tools.Context{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = runner.Analyze(t.Context(), tools.Batch{Scripts: []tools.Script{{Text: "print(1)"}}})
	if err != nil || loads != 1 || nativeCalls != 1 {
		t.Fatalf("composition repeated: loads=%d native=%d err=%v", loads, nativeCalls, err)
	}
}
