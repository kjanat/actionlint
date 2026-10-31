package workflow

import (
	"actionlint.kjanat.dev/v2/diagnostic"
	exprlang "actionlint.kjanat.dev/v2/internal/expression"
	"actionlint.kjanat.dev/v2/internal/mapping"
	"actionlint.kjanat.dev/v2/internal/workflow/value"
)

func SemanticValue(node *String) value.Value {
	if node == nil {
		return value.Value{}
	}
	origin := value.Origin{}
	if node.Pos != nil {
		origin.Span.Start = diagnostic.Position{Line: node.Pos.Line, Column: node.Pos.Col}
	}
	if node.SourceScalar.Evaluated {
		origin.FromExpression = true
		return value.FromJSON(node.Value, origin)
	}
	if node.ContainsExpression() {
		origin.FromExpression = true
		return value.Template(node.Value, origin)
	}
	return value.YAMLScalar(node.Value, YamlTagStr, origin)
}

// MappingScalar converts a source scalar for env and action-input mappings.
// SourceScalar is a pointer-free parse-boundary snapshot, so this conversion
// preserves YAML null, boolean, and numeric values after AST normalization.
func MappingScalar(text string, source SourceScalar, origin value.Origin) value.Value {
	if source.Evaluated {
		origin.FromExpression = true
		return value.FromJSON(text, origin)
	}
	if exprlang.ContainsExpression(text) {
		origin.FromExpression = true
		return value.Template(text, origin)
	}
	if source.HasSource && source.Kind == SourceScalarScalar {
		return value.YAMLScalar(source.Value, source.Tag, origin)
	}
	return value.YAMLScalar(text, YamlTagStr, origin)
}

func semanticMappingValue(node *String) value.Value {
	if node == nil {
		return value.Value{}
	}
	origin := value.Origin{}
	if node.Pos != nil {
		origin.Span.Start = diagnostic.Position{Line: node.Pos.Line, Column: node.Pos.Col}
	}
	return MappingScalar(node.Value, node.SourceScalar, origin)
}

func InputMapping(action *ExecAction) *mapping.Inputs {
	if action == nil {
		return nil
	}
	source := &ActionInputMapping{Inputs: action.Inputs, Expression: action.InputsExpression, Insertions: action.InputInsertions}
	return actionMappingValues(source)
}

func mappingSourceText(source *String) string {
	if source == nil {
		return ""
	}
	return source.Value
}

func actionMappingValues(source *ActionInputMapping) *mapping.Inputs {
	if source == nil {
		return nil
	}
	result := &mapping.Inputs{
		Expression: semanticMappingValue(source.Expression),
		Values:     make(map[string]mapping.Input, len(source.Inputs)),
	}
	for name, input := range source.Inputs {
		if input != nil {
			result.Values[name] = mapping.Input{
				Name: SemanticValue(input.Name), Value: semanticMappingValue(input.Value),
				Source: mappingSourceText(input.Name),
			}
		}
	}
	for _, insertion := range source.Insertions {
		result.Insertions = append(result.Insertions, actionMappingValues(insertion))
	}
	return result
}

func ActionInputValue(action *ExecAction, name string) (string, bool) {
	resolved := mapping.Resolve(InputMapping(action)).Lookup(name)
	return resolved.ScalarString()
}

func ResolvedActionInputs(action *ExecAction) mapping.Mapping {
	return mapping.Resolve(InputMapping(action))
}
