package expression

import (
	"strconv"
	"strings"
)

// FunctionEvaluationPolicy describes argument demand independently of return types.
type FunctionEvaluationPolicy uint8

const (
	EvaluateAllArguments FunctionEvaluationPolicy = iota
	EvaluateStringPredicate
	EvaluateContains
	EvaluateJoin
	EvaluateFormat
	EvaluateCase
)

// ArgumentDemand distinguishes certain evaluation from possible or skipped evaluation.
type ArgumentDemand uint8

const (
	DemandRequired ArgumentDemand = iota
	DemandConditional
	DemandSkipped
)

func logicalRightDemand(node *LogicalOpNode) ArgumentDemand {
	value, known := ConditionConstantValue(node.Left)
	if !known {
		return DemandConditional
	}
	if ExpressionTruthy(value) == (node.Kind == LogicalOpNodeKindOr) {
		return DemandSkipped
	}
	return DemandRequired
}

// argumentDemand is shared by semantic checking and constant evaluation. Earlier
// arguments retain their source values: conversion must not hide array shape or
// the boolean contract of a case predicate.
func argumentDemand(policy FunctionEvaluationPolicy, index, count int, args []callArgument) ArgumentDemand {
	if index == 0 || policy == EvaluateAllArguments {
		return DemandRequired
	}
	if policy == EvaluateCase {
		demand := DemandRequired
		for i := 0; i < index; i += 2 {
			predicate := args[i]
			if predicate.state == argumentInvalid || !matchParameter(BoolType(), predicate, false).accepted {
				return DemandSkipped
			}
			if predicate.state != argumentKnown {
				demand = DemandConditional
				continue
			}
			value, boolean := predicate.value.(bool)
			if index == i+1 {
				if boolean && value {
					return demand
				}
				return DemandSkipped
			}
			if !boolean || value {
				return DemandSkipped
			}
		}
		return demand
	}
	first := &args[0]
	if first.state == argumentInvalid {
		return DemandSkipped
	}
	if first.state != argumentKnown {
		if policy == EvaluateStringPredicate || policy == EvaluateContains {
			switch first.source.Kind() {
			case KindAny:
				return DemandConditional
			case KindBool, KindNull, KindNumber, KindString:
				return DemandRequired
			case KindObject:
				return DemandSkipped
			case KindArray:
				if policy == EvaluateStringPredicate {
					return DemandSkipped
				}
			}
		}
		return DemandConditional
	}
	switch policy {
	case EvaluateAllArguments, EvaluateCase:
		return DemandRequired
	case EvaluateStringPredicate, EvaluateContains:
		switch value := first.value.(type) {
		case map[string]any:
			return DemandSkipped
		case []any:
			if policy == EvaluateStringPredicate || len(value) == 0 {
				return DemandSkipped
			}
		}
	case EvaluateJoin:
		if items, array := first.value.([]any); !array || len(items) < 2 {
			return DemandSkipped
		}
	case EvaluateFormat:
		plan, known := argumentFormatPlan(first, count-1)
		if !known {
			return DemandConditional
		}
		if !plan.valid || !plan.demands(index-1) {
			return DemandSkipped
		}
	}
	return DemandRequired
}

// argumentExecutionOrder lists arguments after the first in runtime order.
// Unreferenced arguments follow the demanded arguments so static checking still
// visits every node. Argument facts remain indexed by their source position.
func argumentExecutionOrder(policy FunctionEvaluationPolicy, count int, args []callArgument) []int {
	order := make([]int, 0, max(count-1, 0))
	seen := make([]bool, count)
	if policy == EvaluateFormat && count > 0 {
		if plan, known := argumentFormatPlan(&args[0], count-1); known && plan.valid {
			for _, part := range plan.parts {
				if index := part.index + 1; part.index >= 0 && !seen[index] {
					order = append(order, index)
					seen[index] = true
				}
			}
		}
	}
	for index := 1; index < count; index++ {
		if !seen[index] {
			order = append(order, index)
		}
	}
	return order
}

func argumentFormatPlan(first *callArgument, count int) (formatPlan, bool) {
	if first.format != nil {
		return *first.format, true
	}
	format, known := constantFunctionString(first.value)
	if first.state != argumentKnown || !known {
		return formatPlan{}, false
	}
	plan := planFormat(format, count)
	first.format = &plan
	return plan, true
}

func (sig *FuncSignature) parameterAt(index, count int) Type {
	if sig.EvaluationPolicy == EvaluateCase {
		if index < count-1 && index%2 == 0 {
			return BoolType()
		}
		return AnyType()
	}
	return sig.Params[min(index, len(sig.Params)-1)]
}

type formatPart struct {
	text      string
	index     int // -1 identifies literal text.
	specifier string
}

type formatPlan struct {
	parts []formatPart
	valid bool
}

func (plan formatPlan) references(index int) bool {
	for _, part := range plan.parts {
		if part.index == index {
			return true
		}
	}
	return false
}

func (plan formatPlan) demands(index int) bool {
	for _, part := range plan.parts {
		if part.index == index {
			return true
		}
		if part.specifier != "" {
			return false
		}
	}
	return false
}

// planFormat validates the entire template before any replacement is evaluated.
// Escaped braces are literal text; placeholders contain unsigned decimal indexes.
func planFormat(format string, count int) formatPlan {
	plan := formatPlan{valid: true}
	var literal strings.Builder
	flush := func() {
		if literal.Len() != 0 {
			plan.parts = append(plan.parts, formatPart{text: literal.String(), index: -1})
			literal.Reset()
		}
	}
	for i := 0; i < len(format); {
		c := format[i]
		if c != '{' && c != '}' {
			literal.WriteByte(c)
			i++
			continue
		}
		if i+1 < len(format) && format[i+1] == c {
			literal.WriteByte(c)
			i += 2
			continue
		}
		end := i + 1
		for end < len(format) && format[end] >= '0' && format[end] <= '9' {
			end++
		}
		if c != '{' || end == i+1 || end >= len(format) || (format[end] != '}' && format[end] != ':') {
			plan.valid = false
			return plan
		}
		index, err := strconv.Atoi(format[i+1 : end])
		if err != nil || index > 255 {
			plan.valid = false
			return plan
		}
		specifier := ""
		if format[end] == ':' {
			var spec strings.Builder
			end++
		scanSpecifier:
			for end < len(format) {
				switch {
				case format[end] != '}':
					spec.WriteByte(format[end])
					end++
				case end+1 < len(format) && format[end+1] == '}':
					spec.WriteByte('}')
					end += 2
				default:
					break scanSpecifier
				}
			}
			if end == len(format) {
				plan.valid = false
				return plan
			}
			specifier = spec.String()
		}
		flush()
		plan.parts = append(plan.parts, formatPart{index: index, specifier: specifier})
		if index >= count {
			plan.valid = false
		}
		i = end + 1
	}
	flush()
	return plan
}
