package declaration

import (
	"strconv"

	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/model/contexts/value"
	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
	mapping "actionlint.kjanat.dev/v2/internal/model/workflows/mappings"
	"actionlint.kjanat.dev/v2/internal/model/workflows/matrix"
)

func origin(pos *sourcepos.Position, expression bool) value.Origin {
	result := value.Origin{FromExpression: expression}
	if pos != nil {
		result.Span.Start = *pos
	}
	return result
}

func scalar(node *workflowlang.String) value.Value {
	if node == nil {
		return value.Value{}
	}
	if node.SourceScalar.Evaluated {
		return value.FromJSON(node.Value, origin(node.Pos, true))
	}
	if node.ContainsExpression() {
		return value.Template(node.Value, origin(node.Pos, true))
	}
	return value.YAMLScalar(node.Value, workflowlang.YamlTagStr, origin(node.Pos, false))
}

func mappingScalar(node *workflowlang.String) value.Value {
	if node == nil {
		return value.Value{}
	}
	return workflowlang.MappingScalar(node.Value, node.SourceScalar, origin(node.Pos, false))
}

func condition(node *workflowlang.String) value.Value {
	if node == nil {
		return value.Value{}
	}
	return value.Condition(node.Value, origin(node.Pos, true))
}

func boolean(node *workflowlang.Bool) value.Value {
	if node == nil {
		return value.Value{}
	}
	if node.Expression != nil {
		return value.Template(node.Expression.Value, origin(node.Expression.Pos, true))
	}
	return value.YAMLScalar(strconv.FormatBool(node.Value), workflowlang.YamlTagBool, origin(node.Pos, false))
}

// Bool decodes a boolean field.
func Bool(node *workflowlang.Bool) value.Value { return boolean(node) }

// Environment resolves an env mapping, including insertions.
func Environment(node *workflowlang.Env) mapping.Mapping { return resolvedEnvironment(node) }

// ResolvedInputs resolves an action step's with: mapping, including insertions.
func ResolvedInputs(node *workflowlang.ExecAction) mapping.Mapping {
	if node == nil {
		return mapping.Mapping{}
	}
	return mapping.Resolve(actionInputs(node))
}

// MatrixResolution resolves a strategy's matrix.
func MatrixResolution(node *workflowlang.Strategy) matrix.Resolution {
	if node == nil {
		return matrix.Resolution{}
	}
	return matrix.Resolve(node)
}

// MatrixMapping resolves a matrix's keys as a mapping.
func MatrixMapping(node *workflowlang.Matrix) mapping.Mapping {
	if node == nil {
		return mapping.Mapping{}
	}
	return mapping.Resolve(matrixMappingInputs(node))
}

// RunnerSelection decodes runs-on.
func RunnerSelection(node *workflowlang.Runner) value.Value { return runnerSelection(node) }

// DefaultsRun decodes defaults.run.
func DefaultsRun(defaults *workflowlang.Defaults) value.Value {
	if defaults == nil {
		return value.Value{}
	}
	return defaultsRunValue(defaults.Run)
}

// Container decodes a job or service container.
func Container(node *workflowlang.Container) value.Value {
	if node == nil {
		return value.Value{}
	}
	return containerValue(node, resolvedEnvironment(node.Env))
}

func runnerSelection(node *workflowlang.Runner) value.Value {
	if node == nil {
		return value.Value{}
	}
	if node.Expression != nil {
		return scalar(node.Expression)
	}
	labels := make([]value.Value, 0, len(node.Labels))
	for _, label := range node.Labels {
		labels = append(labels, scalar(label))
	}
	props := make([]value.Prop, 0, 2)
	if node.LabelsExpr != nil {
		props = append(props, value.Prop{Name: "labels", Value: scalar(node.LabelsExpr)})
	} else if node.Labels != nil {
		props = append(props, value.Prop{Name: "labels", Value: value.Seq(value.Origin{}, labels...)})
	}
	appendSourceProp(&props, "group", node.Group)
	return value.Map(value.Origin{}, props...)
}

func defaultsRunValue(node *workflowlang.DefaultsRun) value.Value {
	if node == nil {
		return value.Value{}
	}
	if node.Expression != nil {
		return scalar(node.Expression)
	}
	props := make([]value.Prop, 0, 2)
	appendSourceProp(&props, "shell", node.Shell)
	appendSourceProp(&props, "working-directory", node.WorkingDirectory)
	return value.Map(origin(node.Pos, false), props...)
}

func containerValue(node *workflowlang.Container, envMapping mapping.Mapping) value.Value {
	if node == nil {
		return value.Value{}
	}
	if node.Expression != nil {
		return scalar(node.Expression)
	}
	props := make([]value.Prop, 0, 10)
	appendSourceProp(&props, "image", node.Image)
	if node.Credentials != nil {
		props = append(props, value.Prop{Name: "credentials", Value: credentialsValue(node.Credentials)})
	}
	if node.Env != nil {
		props = append(props, value.Prop{Name: "env", Value: environmentValue(envMapping, node.Env)})
	}
	ports := make([]value.Value, 0, len(node.Ports))
	for _, port := range node.Ports {
		ports = append(ports, scalar(port))
	}
	volumes := make([]value.Value, 0, len(node.Volumes))
	for _, volume := range node.Volumes {
		volumes = append(volumes, scalar(volume))
	}
	if node.PortsExpression != nil {
		props = append(props, value.Prop{Name: "ports", Value: scalar(node.PortsExpression)})
	} else if node.Ports != nil {
		props = append(props, value.Prop{Name: "ports", Value: value.Seq(origin(node.Pos, false), ports...)})
	}
	if node.VolumesExpression != nil {
		props = append(props, value.Prop{Name: "volumes", Value: scalar(node.VolumesExpression)})
	} else if node.Volumes != nil {
		props = append(props, value.Prop{Name: "volumes", Value: value.Seq(origin(node.Pos, false), volumes...)})
	}
	appendSourceProp(&props, "options", node.Options)
	appendSourceProp(&props, "command", node.Command)
	appendSourceProp(&props, "entrypoint", node.Entrypoint)
	return value.Map(origin(node.Pos, false), props...)
}

func credentialsValue(node *workflowlang.Credentials) value.Value {
	if node.Expression != nil {
		return scalar(node.Expression)
	}
	props := make([]value.Prop, 0, 2)
	appendSourceProp(&props, "username", node.Username)
	appendSourceProp(&props, "password", node.Password)
	return value.Map(origin(node.Pos, false), props...)
}

func environmentValue(mapping mapping.Mapping, node *workflowlang.Env) value.Value {
	if !mapping.Complete() {
		envOrigin := value.Origin{}
		if node.Expression != nil {
			envOrigin = origin(node.Expression.Pos, true)
		}
		return value.DynamicValue("", value.Object, envOrigin)
	}
	props := make([]value.Prop, 0)
	for name, item := range mapping.Entries() {
		props = append(props, value.Prop{Name: name, Value: item})
	}
	return value.Map(value.Origin{}, props...)
}

func appendSourceProp(props *[]value.Prop, name string, source *workflowlang.String) {
	if source != nil {
		*props = append(*props, value.Prop{Name: name, Value: scalar(source)})
	}
}

func resolvedEnvironment(node *workflowlang.Env) mapping.Mapping {
	return mapping.Resolve(environmentInputs(node))
}

func environmentInputs(node *workflowlang.Env) *mapping.Inputs {
	if node == nil {
		return nil
	}
	inputs := &mapping.Inputs{
		Values: make(map[string]mapping.Input, len(node.Vars)), Expression: mappingScalar(node.Expression),
	}
	for key, entry := range node.Vars {
		if entry == nil {
			continue
		}
		inputs.Values[key] = mapping.Input{Name: mappingScalar(entry.Name), Value: mappingScalar(entry.Value), Source: sourceText(entry.Name)}
	}
	for _, insertion := range node.Insertions {
		inputs.Insertions = append(inputs.Insertions, environmentInputs(insertion))
	}
	return inputs
}

func actionInputs(node *workflowlang.ExecAction) *mapping.Inputs {
	if node == nil {
		return nil
	}
	inputs := &mapping.Inputs{
		Values: make(map[string]mapping.Input, len(node.Inputs)), Expression: mappingScalar(node.InputsExpression),
	}
	for key, entry := range node.Inputs {
		if entry == nil {
			continue
		}
		inputs.Values[key] = mapping.Input{Name: mappingScalar(entry.Name), Value: mappingScalar(entry.Value), Source: sourceText(entry.Name)}
	}
	for _, insertion := range node.InputInsertions {
		inputs.Insertions = append(inputs.Insertions, actionMappingInputs(insertion))
	}
	return inputs
}

func actionMappingInputs(node *workflowlang.ActionInputMapping) *mapping.Inputs {
	if node == nil {
		return nil
	}
	inputs := &mapping.Inputs{
		Values: make(map[string]mapping.Input, len(node.Inputs)), Expression: mappingScalar(node.Expression),
	}
	for key, entry := range node.Inputs {
		if entry == nil {
			continue
		}
		inputs.Values[key] = mapping.Input{Name: mappingScalar(entry.Name), Value: mappingScalar(entry.Value), Source: sourceText(entry.Name)}
	}
	for _, insertion := range node.Insertions {
		inputs.Insertions = append(inputs.Insertions, actionMappingInputs(insertion))
	}
	return inputs
}

func sourceText(node *workflowlang.String) string {
	if node == nil {
		return ""
	}
	return node.Value
}

func matrixMappingInputs(node *workflowlang.Matrix) *mapping.Inputs {
	if node == nil {
		return nil
	}
	inputs := &mapping.Inputs{
		Values: make(map[string]mapping.Input, len(node.Rows)+2), Expression: mappingScalar(node.Expression),
	}
	for key, row := range node.Rows {
		var name value.Value
		source := ""
		if row.Name != nil {
			source = row.Name.Value
			name = mappingScalar(row.Name)
			if row.Evaluated {
				name = value.YAMLScalar(row.Name.Value, workflowlang.YamlTagStr, origin(row.Name.Pos, true))
			}
		} else {
			name = value.YAMLScalar(key, workflowlang.YamlTagStr, value.Origin{})
		}
		inputs.Values[key] = mapping.Input{Name: name, Source: source, Evaluated: row.Evaluated}
	}
	for key, filters := range map[string]*workflowlang.MatrixCombinations{"include": node.Include, "exclude": node.Exclude} {
		if filters == nil {
			continue
		}
		name := mappingScalar(filters.Name)
		source := sourceText(filters.Name)
		if filters.Evaluated && filters.Name != nil {
			name = value.YAMLScalar(filters.Name.Value, workflowlang.YamlTagStr, origin(filters.Name.Pos, true))
		}
		inputs.Values[key] = mapping.Input{Name: name, Source: source, Evaluated: filters.Evaluated}
	}
	for _, insertion := range node.Insertions {
		inputs.Insertions = append(inputs.Insertions, matrixMappingInputs(insertion))
	}
	return inputs
}

// Scalar decodes a scalar as a YAML value or template.
func Scalar(node *workflowlang.String) value.Value { return scalar(node) }

// Condition decodes a scalar as an if condition.
func Condition(node *workflowlang.String) value.Value { return condition(node) }
