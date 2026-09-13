package actionlint

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// workflowExpressionShape describes evaluated expression values. Unlike
// ExprType.Assignable, it distinguishes strict schema booleans from truthiness
// and optional object properties from required ones.
type workflowExpressionShape interface {
	validate(ExprType, string) []string
}

type workflowExpressionScalar string

func (shape workflowExpressionScalar) validate(ty ExprType, path string) []string {
	if _, unknown := ty.(AnyType); unknown {
		return nil
	}
	valid := false
	switch shape {
	case "any":
		valid = true
	case "string", "non-empty string", "concurrency queue":
		// TemplateReader converts scalar literals to strings in string definitions.
		switch ty.(type) {
		case StringType, NumberType, BoolType, NullType:
			valid = true
		}
	case "bool":
		_, valid = ty.(BoolType)
	case "positive number":
		_, valid = ty.(NumberType)
	}
	if valid {
		return nil
	}
	return []string{fmt.Sprintf("%s must be %s but found %s", path, shape, ty.String())}
}

type workflowExpressionArray struct{ elem workflowExpressionShape }

func (shape workflowExpressionArray) validate(ty ExprType, path string) []string {
	if _, unknown := ty.(AnyType); unknown {
		return nil
	}
	if array, ok := ty.(*ArrayType); ok {
		return shape.elem.validate(array.Elem, path+"[]")
	}
	return []string{fmt.Sprintf("%s must be array but found %s", path, ty.String())}
}

type workflowExpressionObject struct {
	props    map[string]workflowExpressionShape
	required []string
	mapped   workflowExpressionShape
}

func (shape workflowExpressionObject) validate(ty ExprType, path string) []string {
	if _, unknown := ty.(AnyType); unknown {
		return nil
	}
	object, ok := ty.(*ObjectType)
	if !ok {
		return []string{fmt.Sprintf("%s must be object but found %s", path, ty.String())}
	}
	var errors []string
	seen := map[string]bool{}
	for name, value := range object.Props {
		if name == "" {
			errors = append(errors, path+" has an empty property name")
			continue
		}
		key := strings.ToLower(name)
		if seen[key] {
			errors = append(errors, fmt.Sprintf("%s has duplicate property %q", path, key))
		}
		seen[key] = true
		field, ok := shape.props[key]
		if !ok {
			field = shape.mapped
		}
		if field == nil {
			errors = append(errors, fmt.Sprintf("%s has unknown property %q", path, name))
			continue
		}
		errors = append(errors, field.validate(value, path+"."+name)...)
	}
	if object.IsStrict() {
		for _, required := range shape.required {
			if !seen[required] {
				errors = append(errors, fmt.Sprintf("%s requires property %q", path, required))
			}
		}
	} else if shape.mapped != nil {
		errors = append(errors, shape.mapped.validate(object.Mapped, path+".*")...)
	}
	slices.Sort(errors)
	return errors
}

type workflowExpressionUnion []workflowExpressionShape

func (shape workflowExpressionUnion) validate(ty ExprType, path string) []string {
	// Report the selected structural branch's details when its outer kind matches.
	for _, candidate := range shape {
		if errors := candidate.validate(ty, path); len(errors) == 0 {
			return nil
		}
	}
	for _, candidate := range shape {
		switch candidate.(type) {
		case workflowExpressionObject:
			if _, ok := ty.(*ObjectType); ok {
				return candidate.validate(ty, path)
			}
		case workflowExpressionArray:
			if _, ok := ty.(*ArrayType); ok {
				return candidate.validate(ty, path)
			}
		}
	}
	return []string{fmt.Sprintf("%s has unsupported expression type %s", path, ty.String())}
}

const (
	workflowAny      = workflowExpressionScalar("any")
	workflowString   = workflowExpressionScalar("string")
	workflowBool     = workflowExpressionScalar("bool")
	workflowPositive = workflowExpressionScalar("positive number")
	workflowNonEmpty = workflowExpressionScalar("non-empty string")
	workflowQueue    = workflowExpressionScalar("concurrency queue")
)

var (
	workflowStringMap = workflowExpressionObject{mapped: workflowString}
	workflowLabels    = workflowExpressionUnion{workflowNonEmpty, workflowExpressionArray{workflowNonEmpty}}
	workflowRunner    = workflowExpressionUnion{workflowNonEmpty, workflowExpressionArray{workflowNonEmpty}, workflowExpressionObject{props: map[string]workflowExpressionShape{"group": workflowNonEmpty, "labels": workflowLabels}}}
	workflowStrategy  = workflowExpressionObject{props: map[string]workflowExpressionShape{
		"fail-fast": workflowBool, "max-parallel": workflowPositive,
		"matrix": workflowExpressionObject{
			props: map[string]workflowExpressionShape{
				"include": workflowExpressionArray{workflowExpressionObject{mapped: workflowAny}},
				"exclude": workflowExpressionArray{workflowExpressionObject{mapped: workflowAny}},
			},
			mapped: workflowExpressionArray{workflowAny},
		},
	}}
	workflowDefaultsRun = workflowExpressionObject{props: map[string]workflowExpressionShape{"shell": workflowNonEmpty, "working-directory": workflowNonEmpty}}
	workflowCredentials = workflowExpressionObject{props: map[string]workflowExpressionShape{"username": workflowNonEmpty, "password": workflowNonEmpty}}
	workflowConcurrency = workflowExpressionUnion{workflowString, workflowExpressionObject{props: map[string]workflowExpressionShape{"group": workflowNonEmpty, "cancel-in-progress": workflowBool, "queue": workflowQueue}, required: []string{"group"}}}
	workflowEnvironment = workflowExpressionUnion{workflowNonEmpty, workflowExpressionObject{props: map[string]workflowExpressionShape{"name": workflowNonEmpty, "url": workflowString, "deployment": workflowBool}, required: []string{"name"}}}
	workflowSnapshot    = workflowExpressionUnion{workflowNonEmpty, workflowExpressionObject{props: map[string]workflowExpressionShape{"image-name": workflowNonEmpty, "version": workflowNonEmpty, "if": workflowString}, required: []string{"image-name"}}}
)

func workflowContainerShape(service bool) workflowExpressionShape {
	props := map[string]workflowExpressionShape{
		"image": workflowString, "options": workflowString,
		"env": workflowStringMap, "credentials": workflowCredentials,
		"ports": workflowExpressionArray{workflowNonEmpty}, "volumes": workflowExpressionArray{workflowNonEmpty},
	}
	if service {
		props["command"] = workflowString
		props["entrypoint"] = workflowString
	}
	return workflowExpressionUnion{workflowString, workflowExpressionObject{props: props, required: []string{"image"}}}
}

func (rule *RuleExpression) checkWorkflowExpression(s *String, what, key string, shape workflowExpressionShape) ExprType {
	if s == nil {
		return nil
	}
	ty := rule.checkOneExpression(s, what, key)
	if ty == nil {
		return nil
	}
	errors := shape.validate(ty, what)
	// Inferred array elements can merge to AnyType. Retain known JSON values for
	// per-element and literal constraint checks.
	if len(errors) == 0 {
		if value, known := workflowExpressionLiteral(s); known {
			errors = workflowExpressionLiteralErrors(shape, value, what)
		}
	}
	for _, message := range errors {
		rule.Error(s.Pos, message)
	}
	return ty
}

func workflowExpressionLiteral(s *String) (any, bool) {
	i := strings.Index(s.Value, "${{")
	if i < 0 {
		return nil, false
	}
	expr, err := NewExprParser().Parse(NewExprLexer(s.Value[i+3:]))
	if err != nil {
		return nil, false
	}
	if literal, ok := expr.(*StringNode); ok {
		return literal.Value, true
	}
	if call, ok := expr.(*FuncCallNode); ok && strings.EqualFold(call.Callee, "fromJSON") && len(call.Args) == 1 {
		if literal, ok := call.Args[0].(*StringNode); ok {
			var value any
			if json.Unmarshal([]byte(literal.Value), &value) == nil {
				return value, true
			}
		}
	}
	return nil, false
}

func workflowExpressionLiteralErrors(shape workflowExpressionShape, value any, path string) []string {
	ty := typeOfJSONValue(value)
	if errors := shape.validate(ty, path); len(errors) != 0 {
		return errors
	}
	var errors []string
	switch shape := shape.(type) {
	case workflowExpressionScalar:
		if number, ok := value.(float64); shape == workflowPositive && ok && number <= 0 {
			errors = append(errors, path+" must be greater than zero")
		}
		if shape == workflowNonEmpty && (value == nil || value == "") {
			errors = append(errors, path+" must be a non-empty string")
		}
		if shape == workflowQueue && value != "single" && value != "max" {
			errors = append(errors, path+" must be single or max")
		}
	case workflowExpressionUnion:
		for _, candidate := range shape {
			if len(candidate.validate(ty, path)) == 0 {
				return workflowExpressionLiteralErrors(candidate, value, path)
			}
		}
	case workflowExpressionArray:
		if values, ok := value.([]any); ok {
			for i, item := range values {
				errors = append(errors, workflowExpressionLiteralErrors(shape.elem, item, fmt.Sprintf("%s[%d]", path, i))...)
			}
		}
	case workflowExpressionObject:
		if values, ok := value.(map[string]any); ok {
			for name, item := range values {
				field, ok := shape.props[strings.ToLower(name)]
				if !ok {
					field = shape.mapped
				}
				if field != nil {
					errors = append(errors, workflowExpressionLiteralErrors(field, item, path+"."+name)...)
				}
			}
			if path == "concurrency" && workflowObjectProperty(values, "queue") == "max" && workflowObjectProperty(values, "cancel-in-progress") == true {
				errors = append(errors, "concurrency.queue max cannot be combined with cancel-in-progress true")
			}
		}
	}
	slices.Sort(errors)
	return errors
}

// Workflow schema property names are case-insensitive, including evaluated keys.
func workflowObjectProperty[T any](properties map[string]T, name string) T {
	if value, ok := properties[name]; ok {
		return value
	}
	for key, value := range properties {
		if strings.EqualFold(key, name) {
			return value
		}
	}
	var zero T
	return zero
}

func matrixTypeFromExpression(ty ExprType) *ObjectType {
	object, ok := ty.(*ObjectType)
	if !ok {
		return NewEmptyObjectType()
	}
	result := &ObjectType{Props: map[string]ExprType{}, Mapped: object.Mapped}
	for name, value := range object.Props {
		name = strings.ToLower(name)
		if name == "include" || name == "exclude" {
			continue
		}
		if array, ok := value.(*ArrayType); ok {
			value = array.Elem
		}
		result.Props[name] = value
	}
	includeType := workflowObjectProperty(object.Props, "include")
	if include, ok := includeType.(*ArrayType); ok {
		if combination, ok := include.Elem.(*ObjectType); ok {
			for name, value := range combination.Props {
				name = strings.ToLower(name)
				if current, ok := result.Props[name]; ok {
					value = current.Merge(value)
				}
				result.Props[name] = value
			}
		} else {
			result.Loose()
		}
	} else if _, unknown := includeType.(AnyType); unknown {
		result.Loose()
	}
	return result
}
