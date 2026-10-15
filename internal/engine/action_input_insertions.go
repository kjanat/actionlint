package engine

import "actionlint.kjanat.dev/internal/mapping"

var inputResolver = mapping.Resolver[*String]{
	Text:          func(s *String) string { return s.Value },
	HasExpression: (*String).ContainsExpression,
	Evaluate:      workflowExpressionLiteral,
	NewText: func(value string, source *String) *String {
		if source == nil {
			return &String{Value: value}
		}
		result := *source
		result.Value = value
		return &result
	},
}

func actionInputMapping(action *ExecAction) *mapping.Inputs[*String] {
	return inputMappingView(&ActionInputMapping{Inputs: action.Inputs, Expression: action.InputsExpression, Insertions: action.InputInsertions})
}

func inputMappingView(source *ActionInputMapping) *mapping.Inputs[*String] {
	result := &mapping.Inputs[*String]{Expression: source.Expression, Values: make(map[string]mapping.Input[*String], len(source.Inputs))}
	for name, input := range source.Inputs {
		result.Values[name] = mapping.Input[*String]{Name: input.Name, Value: input.Value}
	}
	for _, insertion := range source.Insertions {
		result.Insertions = append(result.Insertions, inputMappingView(insertion))
	}
	return result
}

func actionInputValue(action *ExecAction, name string) (string, bool) {
	return inputResolver.Value(actionInputMapping(action), name)
}

func actionInputNames(action *ExecAction) (map[string]*Input, bool) {
	names, complete := inputResolver.Names(actionInputMapping(action))
	inputs := make(map[string]*Input, len(names))
	for name, input := range names {
		inputs[name] = &Input{Name: input.Name, Value: input.Value}
	}
	return inputs, complete
}
