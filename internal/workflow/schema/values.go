// Package schema validates evaluated workflow values without running analysis.
package schema

import (
	"fmt"
	"math"
	"slices"
	"strings"

	exprlang "actionlint.kjanat.dev/v2/internal/expression"
)

// Shape describes evaluated expression values. Unlike
// ExprType.Assignable, it distinguishes strict schema booleans from truthiness
// and optional object properties from required ones.
type Shape interface {
	ValidateType(exprlang.Type, string) []string
}

type workflowExpressionScalar string

func (shape workflowExpressionScalar) ValidateType(ty exprlang.Type, path string) []string {
	if _, unknown := ty.(exprlang.AnyType); unknown {
		return nil
	}
	valid := false
	switch shape {
	case "any":
		valid = true
	case "string", "non-empty string", "concurrency queue":
		// TemplateReader converts scalar literals to strings in string definitions.
		switch ty.(type) {
		case exprlang.StringType, exprlang.NumberType, exprlang.BoolType, exprlang.NullType:
			valid = true
		}
	case "bool":
		_, valid = ty.(exprlang.BoolType)
	case "positive integer":
		_, valid = ty.(exprlang.NumberType)
	}
	if valid {
		return nil
	}
	return []string{fmt.Sprintf("%s must be %s but found %s", path, shape, ty.String())}
}

type workflowExpressionArray struct {
	elem     Shape
	nonempty bool
}

func (shape workflowExpressionArray) ValidateType(ty exprlang.Type, path string) []string {
	if _, unknown := ty.(exprlang.AnyType); unknown {
		return nil
	}
	if array, ok := ty.(*exprlang.ArrayType); ok {
		return shape.elem.ValidateType(array.Elem, path+"[]")
	}
	return []string{fmt.Sprintf("%s must be array but found %s", path, ty.String())}
}

type workflowExpressionObject struct {
	props    map[string]Shape
	required []string
	mapped   Shape
}

func (shape workflowExpressionObject) ValidateType(ty exprlang.Type, path string) []string {
	if _, unknown := ty.(exprlang.AnyType); unknown {
		return nil
	}
	object, ok := ty.(*exprlang.ObjectType)
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
		errors = append(errors, field.ValidateType(value, path+"."+name)...)
	}
	if object.IsStrict() {
		for _, required := range shape.required {
			if !seen[required] {
				errors = append(errors, fmt.Sprintf("%s requires property %q", path, required))
			}
		}
	} else if shape.mapped != nil {
		errors = append(errors, shape.mapped.ValidateType(object.Mapped, path+".*")...)
	}
	slices.Sort(errors)
	return errors
}

type workflowExpressionUnion []Shape

func (shape workflowExpressionUnion) ValidateType(ty exprlang.Type, path string) []string {
	// Report the selected structural branch's details when its outer kind matches.
	for _, candidate := range shape {
		if errors := candidate.ValidateType(ty, path); len(errors) == 0 {
			return nil
		}
	}
	for _, candidate := range shape {
		switch candidate.(type) {
		case workflowExpressionObject:
			if _, ok := ty.(*exprlang.ObjectType); ok {
				return candidate.ValidateType(ty, path)
			}
		case workflowExpressionArray:
			if _, ok := ty.(*exprlang.ArrayType); ok {
				return candidate.ValidateType(ty, path)
			}
		}
	}
	return []string{fmt.Sprintf("%s has unsupported expression type %s", path, ty.String())}
}

const (
	Any      = workflowExpressionScalar("any")
	String   = workflowExpressionScalar("string")
	Bool     = workflowExpressionScalar("bool")
	Positive = workflowExpressionScalar("positive integer")
	NonEmpty = workflowExpressionScalar("non-empty string")
	Queue    = workflowExpressionScalar("concurrency queue")
)

var (
	StringMap = workflowExpressionObject{mapped: String}
	LabelList = workflowExpressionArray{elem: NonEmpty, nonempty: true}
	Labels    = workflowExpressionUnion{NonEmpty, LabelList}
	Runner    = workflowExpressionUnion{NonEmpty, LabelList, workflowExpressionObject{props: map[string]Shape{"group": NonEmpty, "labels": Labels}}}
	Strategy  = workflowExpressionObject{props: map[string]Shape{
		"fail-fast": Bool, "max-parallel": Positive,
		"matrix": workflowExpressionObject{
			props: map[string]Shape{
				"include": workflowExpressionArray{elem: workflowExpressionObject{mapped: Any}},
				"exclude": workflowExpressionArray{elem: workflowExpressionObject{mapped: Any}},
			},
			mapped: workflowExpressionArray{elem: Any},
		},
	}}
	DefaultsRun        = workflowExpressionObject{props: map[string]Shape{"shell": NonEmpty, "working-directory": NonEmpty}}
	Credentials        = workflowExpressionObject{props: map[string]Shape{"username": NonEmpty, "password": NonEmpty}}
	ConcurrencyMapping = workflowExpressionObject{props: map[string]Shape{"group": NonEmpty, "cancel-in-progress": Bool, "queue": Queue}, required: []string{"group"}}
	Concurrency        = workflowExpressionUnion{String, ConcurrencyMapping}
	JobConcurrency     = workflowExpressionUnion{NonEmpty, ConcurrencyMapping}
	Environment        = workflowExpressionUnion{NonEmpty, workflowExpressionObject{props: map[string]Shape{"name": NonEmpty, "url": String, "deployment": Bool}, required: []string{"name"}}}
	Snapshot           = workflowExpressionUnion{NonEmpty, workflowExpressionObject{props: map[string]Shape{"image-name": NonEmpty, "version": NonEmpty, "if": String}, required: []string{"image-name"}}}
)

func Container(service bool) Shape {
	props := map[string]Shape{
		"image": String, "options": String,
		"env": StringMap, "credentials": Credentials,
		"ports": workflowExpressionArray{elem: NonEmpty}, "volumes": workflowExpressionArray{elem: NonEmpty},
	}
	if service {
		props["command"] = String
		props["entrypoint"] = String
	}
	return workflowExpressionUnion{String, workflowExpressionObject{props: props, required: []string{"image"}}}
}

func LiteralErrors(shape Shape, value any, path string) []string {
	ty := exprlang.TypeOfJSONValue(value)
	if errors := shape.ValidateType(ty, path); len(errors) != 0 {
		return errors
	}
	var errors []string
	switch shape := shape.(type) {
	case workflowExpressionScalar:
		if number, ok := value.(float64); shape == Positive && ok {
			if number <= 0 {
				errors = append(errors, path+" must be greater than zero")
			} else if math.Trunc(number) != number {
				errors = append(errors, path+" must be an integer")
			}
		}
		if shape == NonEmpty && (value == nil || value == "") {
			errors = append(errors, path+" must be a non-empty string")
		}
		if shape == Queue && value != "single" && value != "max" {
			errors = append(errors, path+" must be single or max")
		}
	case workflowExpressionUnion:
		for _, candidate := range shape {
			if len(candidate.ValidateType(ty, path)) == 0 {
				return LiteralErrors(candidate, value, path)
			}
		}
	case workflowExpressionArray:
		if values, ok := value.([]any); ok {
			if shape.nonempty && len(values) == 0 {
				errors = append(errors, path+" must contain at least one value")
			}
			for i, item := range values {
				errors = append(errors, LiteralErrors(shape.elem, item, fmt.Sprintf("%s[%d]", path, i))...)
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
					errors = append(errors, LiteralErrors(field, item, path+"."+name)...)
				}
			}
			if path == "concurrency" && Property(values, "queue") == "max" && Property(values, "cancel-in-progress") == true {
				errors = append(errors, "concurrency.queue max cannot be combined with cancel-in-progress true")
			}
		}
	}
	slices.Sort(errors)
	return errors
}

// Property finds a case-insensitive schema property, including evaluated keys.
func Property[T any](properties map[string]T, name string) T {
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

func MatrixType(ty exprlang.Type) *exprlang.ObjectType {
	object, ok := ty.(*exprlang.ObjectType)
	if !ok {
		return exprlang.NewEmptyObjectType()
	}
	result := &exprlang.ObjectType{Props: map[string]exprlang.Type{}, Mapped: object.Mapped}
	for name, value := range object.Props {
		name = strings.ToLower(name)
		if name == "include" || name == "exclude" {
			continue
		}
		if array, ok := value.(*exprlang.ArrayType); ok {
			value = array.Elem
		}
		result.Props[name] = value
	}
	includeType := Property(object.Props, "include")
	if include, ok := includeType.(*exprlang.ArrayType); ok {
		if combination, ok := include.Elem.(*exprlang.ObjectType); ok {
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
	} else if _, unknown := includeType.(exprlang.AnyType); unknown {
		result.Loose()
	}
	return result
}

// Services is the mapping of named job services to their container declarations.
var Services Shape = workflowExpressionObject{mapped: Container(true)}

// StringList accepts zero or more non-empty string values.
var StringList Shape = workflowExpressionArray{elem: NonEmpty}

// Matrix describes the declaration before expanding individual combinations.
var Matrix Shape = Strategy.props["matrix"]
