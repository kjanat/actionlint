package actionlint

import "strings"

// Resolve one input without interpreting evaluated strings as expression source.
func actionInputValue(action *ExecAction, name string) (string, bool) {
	var value string
	found, complete := false, true
	add := func(candidate any, known bool) {
		text, scalar := workflowScalarString(candidate)
		complete = complete && known && scalar && !found
		found = true
		value = text
	}
	var collect func(*ActionInputMapping)
	collect = func(mapping *ActionInputMapping) {
		for key, input := range mapping.Inputs {
			if input.Name != nil && input.Name.ContainsExpression() {
				literal, known := workflowExpressionLiteral(input.Name)
				text, scalar := workflowScalarString(literal)
				if !known || !scalar {
					complete = false
					continue
				}
				key = text
			}
			if !strings.EqualFold(key, name) || input.Value == nil {
				continue
			}
			if input.Value.ContainsExpression() {
				literal, known := workflowExpressionLiteral(input.Value)
				add(literal, known)
			} else {
				add(input.Value.Value, true)
			}
		}
		if mapping.Expression != nil {
			literal, known := workflowExpressionLiteral(mapping.Expression)
			object, isObject := literal.(map[string]any)
			if !known || !isObject {
				complete = false
			} else {
				for key, candidate := range object {
					if strings.EqualFold(key, name) {
						add(candidate, true)
					}
				}
			}
		}
		for _, insertion := range mapping.Insertions {
			collect(insertion)
		}
	}
	collect(&ActionInputMapping{Inputs: action.Inputs, Expression: action.InputsExpression, Insertions: action.InputInsertions})
	return value, complete
}

// Retain known names even when another insertion may provide required inputs.
func actionInputNames(action *ExecAction) (map[string]*Input, bool) {
	inputs := map[string]*Input{}
	var collect func(*ActionInputMapping) bool
	collect = func(mapping *ActionInputMapping) bool {
		complete := true
		for key, input := range mapping.Inputs {
			if input.Name != nil && input.Name.ContainsExpression() {
				name, known := environmentLiteral(input.Name)
				if !known {
					complete = false
					continue
				}
				resolved := *input
				resolvedName := *input.Name
				resolvedName.Value = name
				resolved.Name = &resolvedName
				input = &resolved
				key = strings.ToLower(name)
			}
			inputs[key] = input
		}
		if expression := mapping.Expression; expression != nil {
			value, known := workflowExpressionLiteral(expression)
			object, objectValue := value.(map[string]any)
			if !known || !objectValue {
				complete = false
			} else {
				for name := range object {
					inputs[strings.ToLower(name)] = &Input{Name: &String{Value: name, Pos: expression.Pos}}
				}
			}
		}
		for _, insertion := range mapping.Insertions {
			complete = collect(insertion) && complete
		}
		return complete
	}
	complete := collect(&ActionInputMapping{Inputs: action.Inputs, Expression: action.InputsExpression, Insertions: action.InputInsertions})
	return inputs, complete
}
