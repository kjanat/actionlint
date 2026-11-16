package component

import (
	"context"
	"errors"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"
	"actionlint.kjanat.dev/v2/internal/model/checkinputs"
	"actionlint.kjanat.dev/v2/internal/model/runners/runnerapi"
	"actionlint.kjanat.dev/v2/internal/model/scripts"
	"actionlint.kjanat.dev/v2/internal/plugins/protocol/component/kjanat/actionlint-script/environments"
	"github.com/kjanat/wacogo"
)

func TestScriptEnvironmentResponseLimit(t *testing.T) {
	t.Parallel()
	scriptEnvs := []checkinputs.ScriptEnvironment{{
		OperatingSystem:  runnerapi.Linux,
		MergeComparison:  scripts.NameSensitive,
		LookupComparison: scripts.NameSensitive,
		Names:            []checkinputs.EnvironmentVariableName{{Name: strings.Repeat("x", int(maxScriptResultBytes)), Scope: scripts.EnvironmentStep, CaseKnown: true}},
	}}
	result, err := environmentResult(scriptEnvs)
	if _, refused := result.(environments.ResultListEnvironmentStringErr); err != nil || !refused {
		t.Fatalf("oversized environment response was accepted: %T, %v", result, err)
	}
}

func TestScriptEnvironmentHostInterfaceRoundTrip(t *testing.T) {
	t.Parallel()
	ctx := context.WithValue(t.Context(), environmentContextKey{}, snippetEnvironmentNames(checkinputs.ScriptEnvironments(
		scripts.EnvironmentAlternatives("bash", nil, scripts.EnvironmentLayer{Scope: scripts.EnvironmentRuntime, UnknownNames: true}, scripts.EnvironmentLayer{Scope: scripts.EnvironmentStep, Names: []string{"lowercase", "UPPERCASE", "miXedCase"}}),
	)))
	engine := wacogo.NewEngine(ctx)
	t.Cleanup(func() {
		if err := engine.Close(context.WithoutCancel(ctx)); err != nil {
			t.Error(err)
		}
	})
	factory, err := environments.NewFactory(ctx, engine)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := factory.Close(context.WithoutCancel(ctx)); err != nil {
			t.Error(err)
		}
	})
	instance, err := factory.NewInstance(ctx, scriptEnvironments{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := instance.Close(context.WithoutCancel(ctx)); err != nil {
			t.Error(err)
		}
	})
	result, err := environments.WrapInstance(instance, instance.Core()).Current(ctx)
	value, ok := result.(environments.ResultListEnvironmentStringOk)
	if err != nil || !ok || len(value.Value) != 3 {
		t.Fatalf("environment ABI result = %+v, %v", result, err)
	}
	for _, environment := range value.Value {
		if environment.LookupComparison != environments.NameComparisonSensitive || !environment.UnknownNames || len(environment.Names) != 3 || environment.Names[2].Name != "miXedCase" {
			t.Fatalf("environment facts changed across the component interface: %+v", environment)
		}
	}
}

func TestScriptEnvironmentsAreScopedToSuppliedOccurrences(t *testing.T) {
	t.Parallel()
	scriptEnvs := scripts.EnvironmentAlternatives("bash", []runnerapi.OS{runnerapi.Windows}, scripts.EnvironmentLayer{Scope: scripts.EnvironmentStep, Names: []string{"miXedCase"}})
	input := checkinputs.AnalyzerInput{Scripts: []checkinputs.Script{{Occurrence: invocation.Script{ID: "selected"}, Environment: scriptEnvs}}}
	ctx := context.WithValue(t.Context(), environmentContextKey{}, documentEnvironments(input))
	for _, id := range []string{"", "entry", "call", "unselected", "another-invocation"} {
		result, err := (scriptEnvironments{}).ForScript(ctx, id)
		if _, refused := result.(environments.ResultListEnvironmentStringErr); err != nil || !refused {
			t.Fatalf("unsupplied ID %q was accepted: %v, %v", id, result, err)
		}
	}
	for range 2 {
		result, err := (scriptEnvironments{}).ForScript(ctx, "selected")
		ok, accepted := result.(environments.ResultListEnvironmentStringOk)
		if err != nil || !accepted || len(ok.Value) != 1 || ok.Value[0].Names[0].Name != "miXedCase" {
			t.Fatalf("environment facts lost spelling or shared mutable storage: %+v, %v", result, err)
		}
		if ok.Value[0].MergeComparison != environments.NameComparisonInsensitive || ok.Value[0].LookupComparison != environments.NameComparisonSensitive {
			t.Fatalf("Bash lookup was conflated with Windows runner merging: %+v", ok.Value[0])
		}
		ok.Value[0].Names[0].Name = "changed"
	}
	if result, err := (scriptEnvironments{}).Current(ctx); err != nil {
		t.Fatal(err)
	} else if _, refused := result.(environments.ResultListEnvironmentStringErr); !refused {
		t.Fatal("document analysis acquired a snippet's current environment")
	}
}

func TestCurrentScriptEnvironmentDoesNotExposeDocumentScripts(t *testing.T) {
	t.Parallel()
	scriptEnvs := scripts.EnvironmentAlternatives("pwsh", []runnerapi.OS{runnerapi.Windows}, scripts.EnvironmentLayer{Scope: scripts.EnvironmentWorkflow, Names: []string{"lowercase"}})
	ctx := context.WithValue(t.Context(), environmentContextKey{}, snippetEnvironmentNames(checkinputs.ScriptEnvironments(scriptEnvs)))
	result, err := (scriptEnvironments{}).Current(ctx)
	value, ok := result.(environments.ResultListEnvironmentStringOk)
	if err != nil || !ok || len(value.Value) != 1 || value.Value[0].LookupComparison != environments.NameComparisonInsensitive || value.Value[0].UnknownNames {
		t.Fatalf("snippet environment = %+v, %v", result, err)
	}
	if result, err := (scriptEnvironments{}).ForScript(ctx, "some-script"); err != nil {
		t.Fatal(err)
	} else if _, refused := result.(environments.ResultListEnvironmentStringErr); !refused {
		t.Fatal("snippet engine acquired an unsupplied document script")
	}
	if result, err := (scriptEnvironments{}).Current(t.Context()); err != nil {
		t.Fatal(err)
	} else if _, refused := result.(environments.ResultListEnvironmentStringErr); !refused {
		t.Fatal("environment facts were exposed outside a check")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := (scriptEnvironments{}).Current(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled environment query = %v", err)
	}
}

func TestSuppliedScriptWithoutEnvironmentFactsReturnsAnEmptyList(t *testing.T) {
	t.Parallel()
	input := checkinputs.AnalyzerInput{Scripts: []checkinputs.Script{{Occurrence: invocation.Script{ID: "selected"}}}}
	document := context.WithValue(t.Context(), environmentContextKey{}, documentEnvironments(input))
	result, err := (scriptEnvironments{}).ForScript(document, "selected")
	value, ok := result.(environments.ResultListEnvironmentStringOk)
	if err != nil || !ok || len(value.Value) != 0 {
		t.Fatalf("missing facts=%+v, %v", result, err)
	}
	snippet := context.WithValue(t.Context(), environmentContextKey{}, snippetEnvironmentNames(nil))
	result, err = (scriptEnvironments{}).Current(snippet)
	value, ok = result.(environments.ResultListEnvironmentStringOk)
	if err != nil || !ok || len(value.Value) != 0 {
		t.Fatalf("missing snippet facts=%+v, %v", result, err)
	}
	if result, err := (scriptEnvironments{}).ForScript(document, "unselected"); err != nil {
		t.Fatal(err)
	} else if _, refused := result.(environments.ResultListEnvironmentStringErr); !refused {
		t.Fatal("an unselected occurrence obtained environment facts")
	}
}
