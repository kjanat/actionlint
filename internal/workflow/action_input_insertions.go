package workflow

import "actionlint.kjanat.dev/v2/internal/mapping"

var InputResolver = mapping.Resolver[*String]{
	Text:          func(s *String) string { return s.Value },
	HasExpression: (*String).ContainsExpression,
	Evaluate:      WorkflowExpressionLiteral,
	NewText: func(value string, source *String) *String {
		if source == nil {
			return &String{Value: value}
		}
		result := *source
		result.Value = value
		return &result
	},
}

func InputMapping(action *ExecAction) *mapping.Inputs[*String] {
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

func ActionInputValue(action *ExecAction, name string) (string, bool) {
	return InputResolver.Value(InputMapping(action), name)
}

func ActionInputNames(action *ExecAction) (map[string]*Input, bool) {
	names, complete := InputResolver.Names(InputMapping(action))
	sources := map[*String]*ScriptSource{}
	var collect func(*ActionInputMapping)
	collect = func(mapping *ActionInputMapping) {
		for _, input := range mapping.Inputs {
			sources[input.Value] = input.Source
		}
		for _, insertion := range mapping.Insertions {
			collect(insertion)
		}
	}
	collect(&ActionInputMapping{Inputs: action.Inputs, Insertions: action.InputInsertions})
	inputs := make(map[string]*Input, len(names))
	for name, input := range names {
		inputs[name] = &Input{Name: input.Name, Value: input.Value, Source: sources[input.Value]}
	}
	return inputs, complete
}
