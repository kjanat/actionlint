package inputs

import (
	runnerapi "actionlint.kjanat.dev/v2/internal/model/runners/contract"
	scriptmodel "actionlint.kjanat.dev/v2/internal/model/scripts"
)

// ScriptEnvironment supplies variable names and lookup rules without values.
// UnknownNames includes inherited runner variables, earlier GITHUB_ENV writes
// and unresolved YAML keys.
type ScriptEnvironment struct {
	OperatingSystem  runnerapi.OS               `json:"operating_system"`
	MergeComparison  scriptmodel.NameComparison `json:"merge_comparison"`
	LookupComparison scriptmodel.NameComparison `json:"lookup_comparison"`
	Names            []EnvironmentVariableName  `json:"names"`
	UnknownNames     bool                       `json:"unknown_names"`
}

// EnvironmentVariableName reports the spelling and scope that introduced an
// exported key. CaseKnown is false when an unresolved earlier declaration could
// have chosen another spelling on Windows.
type EnvironmentVariableName struct {
	Name      string                       `json:"name"`
	Scope     scriptmodel.EnvironmentScope `json:"scope"`
	CaseKnown bool                         `json:"case_known"`
}

// ScriptEnvironments copies declaration facts into transport-owned records.
func ScriptEnvironments(alternatives []scriptmodel.EnvironmentNames) []ScriptEnvironment {
	if alternatives == nil {
		return nil
	}
	result := make([]ScriptEnvironment, len(alternatives))
	for i, environment := range alternatives {
		names := environment.Names()
		result[i] = ScriptEnvironment{
			OperatingSystem:  environment.OperatingSystem(),
			MergeComparison:  environment.MergeComparison(),
			LookupComparison: environment.LookupComparison(),
			UnknownNames:     environment.UnknownNames(),
			Names:            make([]EnvironmentVariableName, len(names)),
		}
		for j, name := range names {
			result[i].Names[j] = EnvironmentVariableName{Name: name.Name, Scope: name.Scope, CaseKnown: name.CaseKnown}
		}
	}
	return result
}
