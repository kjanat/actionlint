package component

import (
	"context"
	"fmt"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	environmentbind "actionlint.kjanat.dev/v2/internal/plugins/protocol/component/kjanat/actionlint-script/environments"
)

const ScriptEnvironmentsInterface = environmentbind.InterfaceName

type environmentContextKey struct{}

type (
	documentEnvironmentNames map[string][]checkinputs.ScriptEnvironment
	snippetEnvironmentNames  []checkinputs.ScriptEnvironment
)

type scriptEnvironments struct{}

func (scriptEnvironments) ForScript(ctx context.Context, occurrence string) (environmentbind.ResultListEnvironmentString, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	scope, _ := ctx.Value(environmentContextKey{}).(documentEnvironmentNames)
	environments, ok := scope[occurrence]
	if !ok {
		return environmentbind.ResultListEnvironmentStringErr{Value: "script occurrence is outside this analysis call"}, nil
	}
	return environmentResult(environments)
}

func (scriptEnvironments) Current(ctx context.Context) (environmentbind.ResultListEnvironmentString, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	environments, available := ctx.Value(environmentContextKey{}).(snippetEnvironmentNames)
	if !available {
		return environmentbind.ResultListEnvironmentStringErr{Value: "no snippet environment is available in this call"}, nil
	}
	return environmentResult(environments)
}

// scriptEnvironmentBytes includes string bytes and record storage, bounding
// responses even when declaration keys are empty strings.
func scriptEnvironmentBytes(environments []checkinputs.ScriptEnvironment) uint64 {
	var size uint64
	for _, environment := range environments {
		size += 64
		for _, name := range environment.Names {
			size += 32 + uint64(len(name.Name))
		}
	}
	return size
}

func environmentEnum[T fmt.Stringer](name string, cases []T) (T, error) {
	for _, candidate := range cases {
		if candidate.String() == name {
			return candidate, nil
		}
	}
	var zero T
	return zero, fmt.Errorf("unsupported environment value %q", name)
}

func environmentResult(environments []checkinputs.ScriptEnvironment) (environmentbind.ResultListEnvironmentString, error) {
	if scriptEnvironmentBytes(environments) > maxScriptResultBytes {
		return environmentbind.ResultListEnvironmentStringErr{Value: "script environment response exceeds 8 MiB"}, nil
	}
	result := make([]environmentbind.Environment, len(environments))
	for i, environment := range environments {
		os, err := environmentEnum(string(environment.OperatingSystem), []environmentbind.OperatingSystem{
			environmentbind.OperatingSystemLinux, environmentbind.OperatingSystemMacos, environmentbind.OperatingSystemWindows,
		})
		if err != nil {
			return nil, err
		}
		comparisons := []environmentbind.NameComparison{environmentbind.NameComparisonSensitive, environmentbind.NameComparisonInsensitive, environmentbind.NameComparisonUnknown}
		merge, err := environmentEnum(string(environment.MergeComparison), comparisons)
		if err != nil {
			return nil, err
		}
		lookup, err := environmentEnum(string(environment.LookupComparison), comparisons)
		if err != nil {
			return nil, err
		}
		result[i] = environmentbind.Environment{OperatingSystem: os, MergeComparison: merge, LookupComparison: lookup, UnknownNames: environment.UnknownNames, Names: make([]environmentbind.VariableName, len(environment.Names))}
		for j, variable := range environment.Names {
			scope, err := environmentEnum(string(variable.Scope), []environmentbind.EnvironmentScope{
				environmentbind.EnvironmentScopeWorkflow, environmentbind.EnvironmentScopeJob, environmentbind.EnvironmentScopeCaller,
				environmentbind.EnvironmentScopeStep, environmentbind.EnvironmentScopeRuntime,
			})
			if err != nil {
				return nil, err
			}
			result[i].Names[j] = environmentbind.VariableName{Name: variable.Name, Scope: scope, CaseKnown: variable.CaseKnown}
		}
	}
	return environmentbind.ResultListEnvironmentStringOk{Value: result}, nil
}

func documentEnvironments(input checkinputs.Input) documentEnvironmentNames {
	scope := make(documentEnvironmentNames)
	for _, scripts := range [][]checkinputs.Script{input.Scripts, input.Invoked} {
		for _, script := range scripts {
			if script.Occurrence.ID != "" {
				scope[string(script.Occurrence.ID)] = checkinputs.ScriptEnvironments(script.Environment)
			}
		}
	}
	for _, actionInput := range input.ActionInputs {
		if actionInput.Occurrence.ID != "" {
			scope[string(actionInput.Occurrence.ID)] = checkinputs.ScriptEnvironments(actionInput.Environment)
		}
	}
	return scope
}
