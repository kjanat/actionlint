package workflow

import (
	exprlang "actionlint.kjanat.dev/internal/expression"
	"actionlint.kjanat.dev/internal/mapping"
	"go.yaml.in/yaml/v4"
)

func EnvMappingView(env *Env) *mapping.Inputs[*String] {
	result := &mapping.Inputs[*String]{Expression: env.Expression, Values: make(map[string]mapping.Input[*String], len(env.Vars))}
	for name, variable := range env.Vars {
		result.Values[name] = mapping.Input[*String]{Name: variable.Name, Value: variable.Value}
	}
	for _, insertion := range env.Insertions {
		result.Insertions = append(result.Insertions, EnvMappingView(insertion))
	}
	return result
}

func knownInsertedMappingKeys(node *yaml.Node) map[string]*String {
	return InputResolver.InsertedKeys(node, newString, exprlang.IsMappingInsertion)
}

func knownMappingKey(key *String) (*String, bool) {
	return InputResolver.Key(key)
}
