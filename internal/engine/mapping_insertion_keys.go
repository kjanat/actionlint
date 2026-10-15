package engine

import (
	"actionlint.kjanat.dev/internal/mapping"
	"go.yaml.in/yaml/v4"
)

func envMappingView(env *Env) *mapping.Inputs[*String] {
	result := &mapping.Inputs[*String]{Expression: env.Expression, Values: make(map[string]mapping.Input[*String], len(env.Vars))}
	for name, variable := range env.Vars {
		result.Values[name] = mapping.Input[*String]{Name: variable.Name, Value: variable.Value}
	}
	for _, insertion := range env.Insertions {
		result.Insertions = append(result.Insertions, envMappingView(insertion))
	}
	return result
}

func knownInsertedMappingKeys(node *yaml.Node) map[string]*String {
	return inputResolver.InsertedKeys(node, newString, isMappingInsertion)
}

func knownMappingKey(key *String) (*String, bool) {
	return inputResolver.Key(key)
}
