package scripts

import (
	"context"
	"errors"
	"strings"
	"testing"

	scriptmodel "actionlint.kjanat.dev/v2/internal/model/scripts"

	"actionlint.kjanat.dev/v2/internal/checking/inputs"
	"actionlint.kjanat.dev/v2/internal/checking/scripts"
	"actionlint.kjanat.dev/v2/internal/checking/scripts/contract"
	"actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/model/rule"
	"actionlint.kjanat.dev/v2/internal/plugins/execution/preparation"
)

type routingRunner func(context.Context, scripts.Batch) ([]contract.ToolFinding, error)

func (r routingRunner) Analyze(ctx context.Context, batch scripts.Batch) ([]contract.ToolFinding, error) {
	return r(ctx, batch)
}

func TestInputRouteUsesResolvedNativeEligibility(t *testing.T) {
	t.Parallel()
	disabled, enabled := &configuration.Config{}, &configuration.Config{}
	disabled.Overrides = []configuration.Override{{Includes: []string{"other/**"}}}
	site := inputs.ScriptInput{Action: "owner/action", Input: "source", Language: "javascript"}
	registration := contract.Registration{
		Name: "tool",
		Enabled: func(c contract.Context) bool {
			if c.ProjectRoot != "" || c.ProjectConfig != nil || len(c.Config.Overrides) != 0 {
				t.Fatal("input eligibility used project planning instead of resolved script configuration")
			}
			return c.Config.Tools["ruff"].Enabled == nil
		},
		Build: func(contract.Context) (contract.Runner, error) {
			return routingRunner(func(_ context.Context, batch scripts.Batch) ([]contract.ToolFinding, error) {
				if len(batch.Scripts) != 0 {
					t.Fatal("input-only descriptor reached native runner with unsupported run")
				}
				return nil, nil
			}), nil
		},
	}
	disabled.Tools = configuration.ToolsConfig{"ruff": {Enabled: new(false)}}
	calls := 0
	backend := routingBackend(func(_ context.Context, request inputs.ScriptRequest) ([]inputs.ScriptDiagnostic, error) {
		calls++
		if request.Source != "enabled" {
			t.Fatalf("disabled source reached backend: %q", request.Source)
		}
		return []inputs.ScriptDiagnostic{{Message: "input", Location: inputs.ScriptPosition{Row: 1, Column: 1}}}, nil
	})
	registration, err := WithScriptInputs(registration, inputs.ScriptEngineDescriptor{Rule: registration.Descriptor, ScriptInputs: []inputs.ScriptInput{site}}, backend)
	if err != nil {
		t.Fatal(err)
	}
	runner, err := registration.Build(contract.Context{ProjectRoot: "repository", ProjectConfig: enabled})
	if err != nil {
		t.Fatal(err)
	}
	findings, err := runner.Analyze(t.Context(), scripts.Batch{Scripts: []scripts.Unit{
		{Input: actionInput(site, "disabled", nil), Scope: &scripts.Scope{Config: disabled}},
		{Run: runBody("unrelated run", nil), Scope: &scripts.Scope{Config: enabled}},
		{Input: actionInput(site, "enabled", &source.Position{Line: 9, Column: 5}), Scope: &scripts.Scope{Config: enabled}},
	}})
	if err != nil || calls != 1 || len(findings) != 1 || findings[0].Script != 2 {
		t.Fatalf("eligibility changed input routing/indices: calls=%d findings=%+v err=%v", calls, findings, err)
	}
	if len(disabled.Overrides) != 1 {
		t.Fatal("eligibility mutated caller configuration")
	}
}

type routingBackend func(context.Context, inputs.ScriptRequest) ([]inputs.ScriptDiagnostic, error)

func (b routingBackend) Check(ctx context.Context, request inputs.ScriptRequest) ([]inputs.ScriptDiagnostic, error) {
	return b(ctx, request)
}

func TestInputRouteKeepsPreparedConfigurationFailuresSeparate(t *testing.T) {
	t.Parallel()
	site := inputs.ScriptInput{Action: "owner/action", Input: "source", Language: "sh"}
	registration := contract.Registration{
		Name: "tool",
		PrepareInput: func(_ context.Context, script scripts.Unit) (preparation.Prepared, error) {
			return preparation.Prepared{Source: "# options\n" + script.Text(), Config: []byte(`{"shell":"sh"}`), LineMap: func(line int) preparation.Line {
				if line < 2 {
					return preparation.Line{Origin: preparation.LineDirective}
				}
				return preparation.Line{Line: line - 1, Origin: preparation.LineScript}
			}}, nil
		},
		Build: func(contract.Context) (contract.Runner, error) {
			return routingRunner(func(context.Context, scripts.Batch) ([]contract.ToolFinding, error) {
				return []contract.ToolFinding{{Message: "native"}}, nil
			}), nil
		},
	}
	engine := routingBackend(func(_ context.Context, request inputs.ScriptRequest) ([]inputs.ScriptDiagnostic, error) {
		if request.Source != "# options\ninput" || string(request.Config) != `{"shell":"sh"}` {
			t.Fatalf("prepared request changed: %+v", request)
		}
		return []inputs.ScriptDiagnostic{
			{Message: "bad configuration", Location: inputs.ScriptPosition{Row: 1, Column: 1}, EndLocation: inputs.ScriptPosition{Row: 1, Column: 2}},
			{Message: "input", Location: inputs.ScriptPosition{Row: 2, Column: 1}, EndLocation: inputs.ScriptPosition{Row: 2, Column: 2}},
			{Message: "input without end", Location: inputs.ScriptPosition{Row: 2, Column: 1}},
		}, nil
	})
	registration, err := WithScriptInputs(registration, inputs.ScriptEngineDescriptor{Rule: registration.Descriptor, Shells: []string{"bash"}, ScriptInputs: []inputs.ScriptInput{site}}, engine)
	if err != nil {
		t.Fatal(err)
	}
	runner, err := registration.Build(contract.Context{})
	if err != nil {
		t.Fatal(err)
	}
	findings, err := runner.Analyze(t.Context(), scripts.Batch{Scripts: []scripts.Unit{{Input: actionInput(site, "input", &source.Position{Line: 11, Column: 7})}, {Run: runBody("run", nil), Execution: &scriptmodel.Execution{Shell: "bash", ShellKnown: true}}}})
	if err == nil || !strings.Contains(err.Error(), "tools.tool.config: bad configuration") || strings.Contains(err.Error(), "input without end") || len(findings) != 3 || findings[0].Message != "native" || findings[1].Message != "input" || findings[2].Message != "input without end" || findings[2].Start != (source.Position{Line: 11, Column: 7}) {
		t.Fatalf("prepared configuration failure lost partial findings or became a source diagnostic: findings=%+v err=%v", findings, err)
	}
}

func TestInputRoutePreservesPartialResultsAndIndices(t *testing.T) {
	t.Parallel()
	nativeFailure, inputFailure := errors.New("native incomplete"), errors.New("input incomplete")
	site := inputs.ScriptInput{Action: "owner/action", Input: "source", Language: "javascript"}
	descriptor := inputs.ScriptEngineDescriptor{Rule: rule.Descriptor{Name: "tool"}, Shells: []string{"bash"}, ScriptInputs: []inputs.ScriptInput{site}}
	nativeCalls, inputCalls := 0, 0
	registration := contract.Registration{Descriptor: descriptor.Rule, RequiresExecution: true, Build: func(contract.Context) (contract.Runner, error) {
		return routingRunner(func(_ context.Context, batch scripts.Batch) ([]contract.ToolFinding, error) {
			nativeCalls++
			if len(batch.Scripts) != 1 || batch.Scripts[0].Input != nil {
				t.Fatal("native batch/index context changed")
			}
			return []contract.ToolFinding{{Rule: "tool", Message: "native", Script: 0}}, nativeFailure
		}), nil
	}}
	engine := routingBackend(func(_ context.Context, request inputs.ScriptRequest) ([]inputs.ScriptDiagnostic, error) {
		inputCalls++
		if request.Source != "input" || request.Filename != inputFilename(site, "") {
			t.Fatalf("input route analyzed a native run: %+v", request)
		}
		return []inputs.ScriptDiagnostic{{Filename: request.Filename, Code: "INPUT", Message: "input", Location: inputs.ScriptPosition{Row: 1, Column: 1}, EndLocation: inputs.ScriptPosition{Row: 1, Column: 2}}}, inputFailure
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
	runner, err := registration.Build(contract.Context{})
	if err != nil {
		t.Fatal(err)
	}
	findings, err := runner.Analyze(t.Context(), scripts.Batch{Scripts: []scripts.Unit{
		{Run: runBody("run", nil), Execution: &scriptmodel.Execution{Shell: "bash", ShellKnown: true}},
		{Input: actionInput(site, "input", &source.Position{Line: 11, Column: 7})},
	}})
	if !errors.Is(err, nativeFailure) || !errors.Is(err, inputFailure) || nativeCalls != 1 || inputCalls != 1 || len(findings) != 2 {
		t.Fatalf("partial outcomes lost or routes duplicated: native=%d input=%d findings=%+v err=%v", nativeCalls, inputCalls, findings, err)
	}
	if findings[0].Script != 0 || findings[1].Script != 1 || findings[1].Start != (source.Position{Line: 11, Column: 7}) {
		t.Fatalf("input source/index context lost: %+v", findings)
	}
}

func TestInputFilterCannotHideForeignSource(t *testing.T) {
	t.Parallel()
	site := inputs.ScriptInput{Action: "owner/action", Input: "source", Language: "python"}
	registration := contract.Registration{Name: "tool", PrepareInput: func(_ context.Context, script scripts.Unit) (preparation.Prepared, error) {
		return preparation.Prepared{Source: script.Text(), AcceptDiagnostic: func(inputs.ScriptDiagnostic) bool { return false }}, nil
	}, Build: func(contract.Context) (contract.Runner, error) { return routingEmpty{}, nil }}
	engine := routingBackend(func(context.Context, inputs.ScriptRequest) ([]inputs.ScriptDiagnostic, error) {
		return []inputs.ScriptDiagnostic{{Filename: "foreign.py", Code: "F821", Message: "synthetic"}}, nil
	})
	registration, err := WithScriptInputs(registration, inputs.ScriptEngineDescriptor{ScriptInputs: []inputs.ScriptInput{site}}, engine)
	if err != nil {
		t.Fatal(err)
	}
	runner, err := registration.Build(contract.Context{})
	if err != nil {
		t.Fatal(err)
	}
	findings, err := runner.Analyze(t.Context(), scripts.Batch{Scripts: []scripts.Unit{{Input: actionInput(site, "opaque", nil)}}})
	if len(findings) != 0 || err == nil || !strings.Contains(err.Error(), "unrelated source") {
		t.Fatalf("sanitizer hid invalid source identity: findings=%+v err=%v", findings, err)
	}
}

func TestInputFilterRequiresDeclaredTemplateContract(t *testing.T) {
	t.Parallel()
	site := inputs.ScriptInput{Action: "owner/action", Input: "source", Language: "python"}
	for _, templateContract := range []string{"", "unknown", inputs.TemplateEffectsContract} {
		calls := 0
		registration := contract.Registration{Name: "tool", PrepareInput: func(_ context.Context, script scripts.Unit) (preparation.Prepared, error) {
			return preparation.Prepared{Source: script.Text(), TemplateContract: inputs.TemplateEffectsContract}, nil
		}, Build: func(contract.Context) (contract.Runner, error) { return routingEmpty{}, nil }}
		engine := routingBackend(func(context.Context, inputs.ScriptRequest) ([]inputs.ScriptDiagnostic, error) {
			calls++
			return nil, nil
		})
		registration, err := WithScriptInputs(registration, inputs.ScriptEngineDescriptor{TemplateContract: templateContract, ScriptInputs: []inputs.ScriptInput{site}}, engine)
		if err != nil {
			t.Fatal(err)
		}
		runner, err := registration.Build(contract.Context{})
		if err != nil {
			t.Fatal(err)
		}
		_, err = runner.Analyze(t.Context(), scripts.Batch{Scripts: []scripts.Unit{{Input: actionInput(site, "masked", nil)}}})
		if templateContract == inputs.TemplateEffectsContract {
			if err != nil || calls != 1 {
				t.Fatalf("declared contract rejected: calls=%d err=%v", calls, err)
			}
		} else if err == nil || calls != 0 {
			t.Fatalf("undeclared semantics executed: contract=%q calls=%d err=%v", templateContract, calls, err)
		}
	}
}

func TestInputDeclaredContractRequiresExplicitEffect(t *testing.T) {
	t.Parallel()
	site := inputs.ScriptInput{Action: "owner/action", Input: "source", Language: "python"}
	for _, effect := range []inputs.ScriptTemplateEffect{"", "unknown", inputs.TemplateEffectNone} {
		engine := routingBackend(func(context.Context, inputs.ScriptRequest) ([]inputs.ScriptDiagnostic, error) {
			return []inputs.ScriptDiagnostic{{Code: "custom", Message: "finding", Location: inputs.ScriptPosition{Row: 1, Column: 1}, TemplateEffect: effect}}, nil
		})
		registration, err := RegisterEngine(inputs.ScriptEngineDescriptor{Rule: rule.Descriptor{Name: "tool"}, TemplateContract: inputs.TemplateEffectsContract, ScriptInputs: []inputs.ScriptInput{site}}, engine)
		if err != nil {
			t.Fatal(err)
		}
		runner, err := registration.Build(contract.Context{})
		if err != nil {
			t.Fatal(err)
		}
		findings, err := runner.Analyze(t.Context(), scripts.Batch{Scripts: []scripts.Unit{{Input: actionInput(site, "source", &source.Position{Line: 1, Column: 1})}}})
		if (err == nil) != (effect == inputs.TemplateEffectNone) || (len(findings) == 1) != (effect == inputs.TemplateEffectNone) {
			t.Fatalf("effect %q: findings=%+v err=%v", effect, findings, err)
		}
	}
}

type describedRoutingBackend struct {
	inputs.ScriptBackend
	descriptor inputs.ScriptEngineDescriptor
}

func (b describedRoutingBackend) ScriptDescriptor() inputs.ScriptEngineDescriptor {
	return b.descriptor
}

func TestConfiguredLegacyDescriptorPreservesNativeRoutes(t *testing.T) {
	t.Parallel()
	engine := describedRoutingBackend{descriptor: inputs.ScriptEngineDescriptor{Engine: "tool"}}
	registration := contract.Registration{Name: "tool", Build: func(contract.Context) (contract.Runner, error) {
		return routingRunner(func(_ context.Context, batch scripts.Batch) ([]contract.ToolFinding, error) {
			if len(batch.Scripts) != 1 || batch.Scripts[0].Text() != "native" {
				t.Fatalf("legacy metadata discarded native route: %+v", batch.Scripts)
			}
			return []contract.ToolFinding{{Rule: "tool", Message: "native"}}, nil
		}), nil
	}}
	registration = WithConfiguredEngine(registration, func(contract.Context) (inputs.ScriptBackend, error) { return engine, nil })
	runner, err := registration.Build(contract.Context{})
	if err != nil {
		t.Fatal(err)
	}
	findings, err := runner.Analyze(t.Context(), scripts.Batch{Scripts: []scripts.Unit{{Run: runBody("native", nil), Execution: &scriptmodel.Execution{Shell: "python", ShellKnown: true}}}})
	if err != nil || len(findings) != 1 {
		t.Fatalf("legacy native routing: %+v, %v", findings, err)
	}
	if _, err := RegisterEngine(inputs.ScriptEngineDescriptor{Rule: rule.Descriptor{Name: "tool"}}, engine); err == nil {
		t.Fatal("standalone engine without routes accepted")
	}
}

func TestConfiguredRoutesValidateEveryScopeOnceWithoutRuns(t *testing.T) {
	t.Parallel()
	first, second := &configuration.Config{}, &configuration.Config{}
	site := inputs.ScriptInput{Action: "owner/action", Input: "code", Language: "python"}
	checks, nativeCalls := 0, 0
	engine := describedRoutingBackend{ScriptBackend: routingBackend(func(context.Context, inputs.ScriptRequest) ([]inputs.ScriptDiagnostic, error) {
		checks++
		return nil, nil
	}), descriptor: inputs.ScriptEngineDescriptor{Engine: "tool", Rule: rule.Descriptor{Name: "custom-rule"}, ScriptInputs: []inputs.ScriptInput{site}}}
	registration := WithConfiguredEngine(contract.Registration{Name: "tool", Build: func(contract.Context) (contract.Runner, error) {
		return routingRunner(func(_ context.Context, batch scripts.Batch) ([]contract.ToolFinding, error) {
			nativeCalls++
			if len(batch.Scopes) != 3 || len(batch.Scripts) != 0 {
				t.Fatalf("scope validation changed: %+v", batch)
			}
			return nil, nil
		}), nil
	}}, func(contract.Context) (inputs.ScriptBackend, error) { return engine, nil })
	runner, err := registration.Build(contract.Context{})
	if err != nil {
		t.Fatal(err)
	}
	firstScope, secondScope := &scripts.Scope{Config: first}, &scripts.Scope{Config: second}
	_, err = runner.Analyze(t.Context(), scripts.Batch{Scopes: []*scripts.Scope{firstScope, secondScope, {}}, Scripts: []scripts.Unit{{Input: actionInput(site, "first", nil), Scope: firstScope}, {Input: actionInput(site, "second", nil), Scope: secondScope}}})
	if err != nil || nativeCalls != 1 || checks != 2 {
		t.Fatalf("scope/input routing counts: native=%d check=%d err=%v", nativeCalls, checks, err)
	}
	_, err = runner.Analyze(t.Context(), scripts.Batch{Scopes: []*scripts.Scope{{Config: first}, {Config: second}, {}}})
	if err != nil || nativeCalls != 2 || checks != 2 {
		t.Fatalf("scope-only batch skipped validation or called guest: native=%d check=%d err=%v", nativeCalls, checks, err)
	}
}

func TestConfiguredRouteRetainsExplicitShellWithoutExecutionFacts(t *testing.T) {
	t.Parallel()
	calls := 0
	engine := describedRoutingBackend{ScriptBackend: routingBackend(func(context.Context, inputs.ScriptRequest) ([]inputs.ScriptDiagnostic, error) {
		t.Fatal("native route bypassed adapter")
		return nil, nil
	}), descriptor: inputs.ScriptEngineDescriptor{Engine: "tool", Rule: rule.Descriptor{Name: "tool"}, Shells: []string{"python"}}}
	registration := WithConfiguredEngine(contract.Registration{Name: "tool", Build: func(contract.Context) (contract.Runner, error) {
		return routingRunner(func(_ context.Context, batch scripts.Batch) ([]contract.ToolFinding, error) {
			calls++
			if len(batch.Scripts) != 2 || batch.Scripts[0].Text() != "print(1)" || batch.Scripts[1].Text() != "print(2)" {
				t.Fatalf("explicit Python shell lost without execution facts: %+v", batch.Scripts)
			}
			return nil, nil
		}), nil
	}}, func(contract.Context) (inputs.ScriptBackend, error) { return engine, nil })
	runner, err := registration.Build(contract.Context{})
	if err != nil {
		t.Fatal(err)
	}
	python, unadvertised := "python", "bash"
	_, err = runner.Analyze(t.Context(), scripts.Batch{Scripts: []scripts.Unit{
		{Run: runBody("print(1)", nil), Shell: &python},
		{Run: runBody("print(2)", nil), Execution: &scriptmodel.Execution{Shell: `C:\Python\python.exe {0}`, ShellKnown: true}},
		// The runner splits at the first literal space, without unquoting the executable.
		{Run: runBody("not a runnable quoted executable", nil), Execution: &scriptmodel.Execution{Shell: `"C:\Program Files\Python\python.exe" {0}`, ShellKnown: true}},
		{Run: runBody("echo no", nil), Shell: &unadvertised},
	}})
	if err != nil || calls != 1 {
		t.Fatalf("native route calls=%d err=%v", calls, err)
	}
}

func TestConfigureBindsLoaderOnce(t *testing.T) {
	t.Parallel()
	loads, nativeCalls := 0, 0
	registration := contract.Registration{Name: "tool", ConfiguredBackend: func(contract.Context) (inputs.ScriptBackend, error) {
		loads++
		return routingBackend(func(context.Context, inputs.ScriptRequest) ([]inputs.ScriptDiagnostic, error) {
			t.Fatal("native adapter bypassed")
			return nil, nil
		}), nil
	}, Build: func(contract.Context) (contract.Runner, error) {
		return routingRunner(func(context.Context, scripts.Batch) ([]contract.ToolFinding, error) {
			nativeCalls++
			return nil, nil
		}), nil
	}}
	registration = Configure(Configure(registration))
	runner, err := registration.Build(contract.Context{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = runner.Analyze(t.Context(), scripts.Batch{Scripts: []scripts.Unit{{Run: runBody("print(1)", nil)}}})
	if err != nil || loads != 1 || nativeCalls != 1 {
		t.Fatalf("composition repeated: loads=%d native=%d err=%v", loads, nativeCalls, err)
	}
}

func TestConfiguredRouteRejectsForeignEngineIdentity(t *testing.T) {
	t.Parallel()
	calls := 0
	foreign := describedRoutingBackend{ScriptBackend: routingBackend(func(context.Context, inputs.ScriptRequest) ([]inputs.ScriptDiagnostic, error) {
		calls++
		return nil, nil
	}), descriptor: inputs.ScriptEngineDescriptor{Engine: "other", Rule: rule.Descriptor{Name: "tool"}, ScriptInputs: []inputs.ScriptInput{{Action: "owner/action", Input: "source", Language: "python"}}}}
	registration := WithConfiguredEngine(contract.Registration{Name: "tool", Build: func(contract.Context) (contract.Runner, error) {
		return routingEmpty{}, nil
	}}, func(contract.Context) (inputs.ScriptBackend, error) { return foreign, nil })
	if _, err := registration.InputSelectors(contract.Context{}); err == nil || !strings.Contains(err.Error(), `does not match tool "tool"`) {
		t.Fatalf("foreign selectors accepted: %v", err)
	}
	runner, err := registration.Build(contract.Context{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = runner.Analyze(t.Context(), scripts.Batch{Scripts: []scripts.Unit{{Input: actionInput(inputs.ScriptInput{Action: "owner/action", Input: "source", Language: "python"}, "input", nil)}}})
	if err == nil || calls != 0 {
		t.Fatalf("foreign engine executed: calls=%d err=%v", calls, err)
	}
}
