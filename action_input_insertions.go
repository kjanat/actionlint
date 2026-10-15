package actionlint

import (
	"maps"
	"strings"
)

// Retain known names even when another insertion may provide required inputs.
func actionInputNames(action *ExecAction) (map[string]*Input, bool) {
	inputs := map[string]*Input{}
	var collect func(*ActionInputMapping) bool
	collect = func(mapping *ActionInputMapping) bool {
		complete := true
		maps.Copy(inputs, mapping.Inputs)
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
