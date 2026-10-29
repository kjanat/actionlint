package workflow

import "actionlint.kjanat.dev/v2/internal/mapping"

func EnvMappingView(env *Env) *mapping.Inputs {
	if env == nil {
		return nil
	}
	result := &mapping.Inputs{
		Expression: semanticMappingValue(env.Expression),
		Values:     make(map[string]mapping.Input, len(env.Vars)),
	}
	for name, variable := range env.Vars {
		if variable != nil {
			result.Values[name] = mapping.Input{
				Name: SemanticValue(variable.Name), Value: semanticMappingValue(variable.Value),
				Source: mappingSourceText(variable.Name),
			}
		}
	}
	for _, insertion := range env.Insertions {
		result.Insertions = append(result.Insertions, EnvMappingView(insertion))
	}
	return result
}
