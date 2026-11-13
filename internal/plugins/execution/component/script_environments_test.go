package component

import (
	"context"
	"errors"
	"strings"
	"testing"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	runnerapi "actionlint.kjanat.dev/v2/internal/model/runners/contract"
	scriptmodel "actionlint.kjanat.dev/v2/internal/model/scripts"
	bind "actionlint.kjanat.dev/v2/internal/plugins/protocol/component/kjanat/actionlint-script/environments"
	"github.com/kjanat/wacogo"
)

func TestScriptEnvironmentResponseLimit(t *testing.T) {
	t.Parallel()
	environments := []checkinputs.ScriptEnvironment{{
		OperatingSystem:  runnerapi.Linux,
		MergeComparison:  scriptmodel.NameSensitive,
		LookupComparison: scriptmodel.NameSensitive,
		Names:            []checkinputs.EnvironmentVariableName{{Name: strings.Repeat("x", int(maxScriptResultBytes)), Scope: scriptmodel.EnvironmentStep, CaseKnown: true}},
	}}
	result, err := environmentResult(environments)
	if _, refused := result.(bind.ResultListEnvironmentStringErr); err != nil || !refused {
		t.Fatalf("oversized environment response was accepted: %T, %v", result, err)
	}
}

func TestScriptEnvironmentHostInterfaceRoundTrip(t *testing.T) {
	t.Parallel()
	ctx := context.WithValue(t.Context(), environmentContextKey{}, snippetEnvironmentNames(checkinputs.ScriptEnvironments(
		scriptmodel.EnvironmentAlternatives("bash", nil, scriptmodel.EnvironmentLayer{Scope: scriptmodel.EnvironmentRuntime, UnknownNames: true}, scriptmodel.EnvironmentLayer{Scope: scriptmodel.EnvironmentStep, Names: []string{"lowercase", "UPPERCASE", "miXedCase"}}),
	)))
	engine := wacogo.NewEngine(ctx)
	t.Cleanup(func() {
		if err := engine.Close(context.WithoutCancel(ctx)); err != nil {
			t.Error(err)
		}
	})
	factory, err := bind.NewFactory(ctx, engine)
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
	result, err := bind.WrapInstance(instance, instance.Core()).Current(ctx)
	value, ok := result.(bind.ResultListEnvironmentStringOk)
	if err != nil || !ok || len(value.Value) != 3 {
		t.Fatalf("environment ABI result = %+v, %v", result, err)
	}
	for _, environment := range value.Value {
		if environment.LookupComparison != bind.NameComparisonSensitive || !environment.UnknownNames || len(environment.Names) != 3 || environment.Names[2].Name != "miXedCase" {
			t.Fatalf("environment facts changed across the component interface: %+v", environment)
		}
	}
}

func TestScriptEnvironmentsAreScopedToSuppliedOccurrences(t *testing.T) {
	t.Parallel()
	environments := scriptmodel.EnvironmentAlternatives("bash", []runnerapi.OS{runnerapi.Windows}, scriptmodel.EnvironmentLayer{Scope: scriptmodel.EnvironmentStep, Names: []string{"miXedCase"}})
	input := checkinputs.Input{Scripts: []checkinputs.Script{{Occurrence: checkinputs.ScriptOccurrence{ID: "selected"}, Environment: environments}}}
	ctx := context.WithValue(t.Context(), environmentContextKey{}, documentEnvironments(input))
	for _, id := range []string{"", "entry", "call", "unselected", "another-invocation"} {
		result, err := (scriptEnvironments{}).ForScript(ctx, id)
		if _, refused := result.(bind.ResultListEnvironmentStringErr); err != nil || !refused {
			t.Fatalf("unsupplied ID %q was accepted: %v, %v", id, result, err)
		}
	}
	for range 2 {
		result, err := (scriptEnvironments{}).ForScript(ctx, "selected")
		ok, accepted := result.(bind.ResultListEnvironmentStringOk)
		if err != nil || !accepted || len(ok.Value) != 1 || ok.Value[0].Names[0].Name != "miXedCase" {
			t.Fatalf("environment facts lost spelling or shared mutable storage: %+v, %v", result, err)
		}
		if ok.Value[0].MergeComparison != bind.NameComparisonInsensitive || ok.Value[0].LookupComparison != bind.NameComparisonSensitive {
			t.Fatalf("Bash lookup was conflated with Windows runner merging: %+v", ok.Value[0])
		}
		ok.Value[0].Names[0].Name = "changed"
	}
	if result, err := (scriptEnvironments{}).Current(ctx); err != nil {
		t.Fatal(err)
	} else if _, refused := result.(bind.ResultListEnvironmentStringErr); !refused {
		t.Fatal("document analysis acquired a snippet's current environment")
	}
}

func TestCurrentScriptEnvironmentDoesNotExposeDocumentScripts(t *testing.T) {
	t.Parallel()
	environments := scriptmodel.EnvironmentAlternatives("pwsh", []runnerapi.OS{runnerapi.Windows}, scriptmodel.EnvironmentLayer{Scope: scriptmodel.EnvironmentWorkflow, Names: []string{"lowercase"}})
	ctx := context.WithValue(t.Context(), environmentContextKey{}, snippetEnvironmentNames(checkinputs.ScriptEnvironments(environments)))
	result, err := (scriptEnvironments{}).Current(ctx)
	value, ok := result.(bind.ResultListEnvironmentStringOk)
	if err != nil || !ok || len(value.Value) != 1 || value.Value[0].LookupComparison != bind.NameComparisonInsensitive || value.Value[0].UnknownNames {
		t.Fatalf("snippet environment = %+v, %v", result, err)
	}
	if result, err := (scriptEnvironments{}).ForScript(ctx, "some-script"); err != nil {
		t.Fatal(err)
	} else if _, refused := result.(bind.ResultListEnvironmentStringErr); !refused {
		t.Fatal("snippet engine acquired an unsupplied document script")
	}
	if result, err := (scriptEnvironments{}).Current(t.Context()); err != nil {
		t.Fatal(err)
	} else if _, refused := result.(bind.ResultListEnvironmentStringErr); !refused {
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
	input := checkinputs.Input{Scripts: []checkinputs.Script{{Occurrence: checkinputs.ScriptOccurrence{ID: "selected"}}}}
	document := context.WithValue(t.Context(), environmentContextKey{}, documentEnvironments(input))
	result, err := (scriptEnvironments{}).ForScript(document, "selected")
	value, ok := result.(bind.ResultListEnvironmentStringOk)
	if err != nil || !ok || len(value.Value) != 0 {
		t.Fatalf("missing facts=%+v, %v", result, err)
	}
	snippet := context.WithValue(t.Context(), environmentContextKey{}, snippetEnvironmentNames(nil))
	result, err = (scriptEnvironments{}).Current(snippet)
	value, ok = result.(bind.ResultListEnvironmentStringOk)
	if err != nil || !ok || len(value.Value) != 0 {
		t.Fatalf("missing snippet facts=%+v, %v", result, err)
	}
	if result, err := (scriptEnvironments{}).ForScript(document, "unselected"); err != nil {
		t.Fatal(err)
	} else if _, refused := result.(bind.ResultListEnvironmentStringErr); !refused {
		t.Fatal("an unselected occurrence obtained environment facts")
	}
}
