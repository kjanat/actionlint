package functions

import (
	exprtypes "actionlint.kjanat.dev/v2/internal/languages/expressions/types"
	exprvalues "actionlint.kjanat.dev/v2/internal/languages/expressions/values"
)

// DemandForArgument is shared by semantic checking and constant evaluation. Earlier
// arguments supply their source values: conversion must not hide array shape or
// the boolean contract of a case predicate.
func DemandForArgument(policy FunctionEvaluationPolicy, index, count int, args []CallArgument) ArgumentDemand {
	if index == 0 || policy == EvaluateAllArguments {
		return DemandRequired
	}
	if policy == EvaluateCase {
		demand := DemandRequired
		for i := 0; i < index; i += 2 {
			predicate := args[i]
			if predicate.State == ArgumentInvalid || !MatchParameter(exprtypes.BoolType(), predicate, false).Accepted {
				return DemandSkipped
			}
			if predicate.State != ArgumentKnown {
				demand = DemandConditional
				continue
			}
			value, boolean := predicate.Value.(bool)
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
	if first.State == ArgumentInvalid {
		return DemandSkipped
	}
	if first.State != ArgumentKnown {
		if policy == EvaluateStringPredicate || policy == EvaluateContains {
			switch first.Source.Kind() {
			case exprtypes.KindAny:
				return DemandConditional
			case exprtypes.KindBool, exprtypes.KindNull, exprtypes.KindNumber, exprtypes.KindString:
				return DemandRequired
			case exprtypes.KindObject:
				return DemandSkipped
			case exprtypes.KindArray:
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
		switch value := first.Value.(type) {
		case map[string]any:
			return DemandSkipped
		case []any:
			if policy == EvaluateStringPredicate || len(value) == 0 {
				return DemandSkipped
			}
		}
	case EvaluateJoin:
		if items, array := first.Value.([]any); !array || len(items) < 2 {
			return DemandSkipped
		}
	case EvaluateFormat:
		plan, known := ArgumentFormatPlan(first, count-1)
		if !known {
			return DemandConditional
		}
		if !plan.Valid() || !plan.Demands(index-1) {
			return DemandSkipped
		}
	}
	return DemandRequired
}

// ArgumentExecutionOrder lists arguments after the first in runtime order.
// Unreferenced arguments follow the demanded arguments so static checking still
// visits every node. Argument facts remain indexed by their source position.
func ArgumentExecutionOrder(policy FunctionEvaluationPolicy, count int, args []CallArgument) []int {
	order := make([]int, 0, max(count-1, 0))
	seen := make([]bool, count)
	if policy == EvaluateFormat && count > 0 {
		if plan, known := ArgumentFormatPlan(&args[0], count-1); known && plan.Valid() {
			for part := range plan.Parts() {
				if index := part.Index + 1; part.Index >= 0 && !seen[index] {
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

func ArgumentFormatPlan(first *CallArgument, count int) (FormatPlan, bool) {
	if first.Format != nil {
		return *first.Format, true
	}
	format, known := exprvalues.FunctionString(first.Value)
	if first.State != ArgumentKnown || !known {
		return FormatPlan{}, false
	}
	plan := PlanFormat(format, count)
	first.Format = &plan
	return plan, true
}
