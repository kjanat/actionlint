package component

import (
	"context"
	"fmt"

	"actionlint.kjanat.dev/v2/internal/model/checkinputs"
	"actionlint.kjanat.dev/v2/internal/plugins/protocol/component/kjanat/actionlint-script/environments"
)

const ScriptEnvironmentsInterface = environments.InterfaceName

type environmentContextKey struct{}

type (
	documentEnvironmentNames map[string][]checkinputs.ScriptEnvironment
	snippetEnvironmentNames  []checkinputs.ScriptEnvironment
)

type scriptEnvironments struct{}

func (scriptEnvironments) ForScript(ctx context.Context, occurrence string) (environments.ResultListEnvironmentString, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	scope, _ := ctx.Value(environmentContextKey{}).(documentEnvironmentNames)
	scriptEnvs, ok := scope[occurrence]
	if !ok {
		return environments.ResultListEnvironmentStringErr{Value: "script occurrence is outside this analysis call"}, nil
	}
	return environmentResult(scriptEnvs)
}

func (scriptEnvironments) Current(ctx context.Context) (environments.ResultListEnvironmentString, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	scriptEnvs, available := ctx.Value(environmentContextKey{}).(snippetEnvironmentNames)
	if !available {
		return environments.ResultListEnvironmentStringErr{Value: "no snippet environment is available in this call"}, nil
	}
	return environmentResult(scriptEnvs)
}

// scriptEnvironmentBytes includes string bytes and record storage, bounding
// responses even when declaration keys are empty strings.
func scriptEnvironmentBytes(scriptEnvs []checkinputs.ScriptEnvironment) uint64 {
	var size uint64
	for _, environment := range scriptEnvs {
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

func environmentResult(scriptEnvs []checkinputs.ScriptEnvironment) (environments.ResultListEnvironmentString, error) {
	if scriptEnvironmentBytes(scriptEnvs) > maxScriptResultBytes {
		return environments.ResultListEnvironmentStringErr{Value: "script environment response exceeds 8 MiB"}, nil
	}
	result := make([]environments.Environment, len(scriptEnvs))
	for i, environment := range scriptEnvs {
		os, err := environmentEnum(string(environment.OperatingSystem), []environments.OperatingSystem{
			environments.OperatingSystemLinux, environments.OperatingSystemMacos, environments.OperatingSystemWindows,
		})
		if err != nil {
			return nil, err
		}
		comparisons := []environments.NameComparison{environments.NameComparisonSensitive, environments.NameComparisonInsensitive, environments.NameComparisonUnknown}
		merge, err := environmentEnum(string(environment.MergeComparison), comparisons)
		if err != nil {
			return nil, err
		}
		lookup, err := environmentEnum(string(environment.LookupComparison), comparisons)
		if err != nil {
			return nil, err
		}
		result[i] = environments.Environment{OperatingSystem: os, MergeComparison: merge, LookupComparison: lookup, UnknownNames: environment.UnknownNames, Names: make([]environments.VariableName, len(environment.Names))}
		for j, variable := range environment.Names {
			scope, err := environmentEnum(string(variable.Scope), []environments.EnvironmentScope{
				environments.EnvironmentScopeWorkflow, environments.EnvironmentScopeJob, environments.EnvironmentScopeCaller,
				environments.EnvironmentScopeStep, environments.EnvironmentScopeRuntime,
			})
			if err != nil {
				return nil, err
			}
			result[i].Names[j] = environments.VariableName{Name: variable.Name, Scope: scope, CaseKnown: variable.CaseKnown}
		}
	}
	return environments.ResultListEnvironmentStringOk{Value: result}, nil
}

func documentEnvironments(input checkinputs.AnalyzerInput) documentEnvironmentNames {
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
