package workflow

import mapping "actionlint.kjanat.dev/v2/internal/model/workflows/mappings"

func EnvMappingView(env *Env) *mapping.DeclaredMapping {
	if env == nil {
		return nil
	}
	result := &mapping.DeclaredMapping{
		Expression: semanticMappingValue(env.Expression),
		Values:     make(map[string]mapping.DeclaredEntry, len(env.Vars)),
	}
	for name, variable := range env.Vars {
		if variable != nil {
			result.Values[name] = mapping.DeclaredEntry{
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
