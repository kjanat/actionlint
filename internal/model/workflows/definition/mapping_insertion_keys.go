package definition

import "actionlint.kjanat.dev/v2/internal/model/workflows/mappings"

func EnvMappingView(env *Env) *mappings.DeclaredMapping {
	if env == nil {
		return nil
	}
	result := &mappings.DeclaredMapping{
		Expression: semanticMappingValue(env.Expression),
		Values:     make(map[string]mappings.DeclaredEntry, len(env.Vars)),
	}
	for name, variable := range env.Vars {
		if variable != nil {
			result.Values[name] = mappings.DeclaredEntry{
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
