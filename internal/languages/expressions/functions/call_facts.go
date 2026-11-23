package functions

import (
	"slices"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/exprtypes"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/exprvalues"
)

type ArgumentDemand uint8

const (
	DemandRequired ArgumentDemand = iota
	DemandConditional
	DemandSkipped
)

type ArgumentConversion uint8

const (
	ConversionNone ArgumentConversion = iota
	ConversionString
	ConversionUnavailable
)

type FunctionArgumentFacts struct {
	Source      exprtypes.Type
	SourceValue exprvalues.EvaluationResult
	Accepted    bool
	Demand      ArgumentDemand
	Parameter   exprtypes.Type
	Converted   exprtypes.Type
	Conversion  ArgumentConversion
	Value       exprvalues.EvaluationResult
}

type ReturnDomainState uint8

const (
	ReturnDomainUnknown ReturnDomainState = iota
	ReturnDomainContainsValue
	ReturnDomainExcludesValue
)

type FunctionCallFacts struct {
	SignatureMatched    bool
	Demand              ArgumentDemand
	ReturnDomain        ReturnDomainState
	DeclaredReturns     []exprtypes.Type
	RefinedAlternatives []exprtypes.Type
	Arguments           []FunctionArgumentFacts
}

func (facts FunctionCallFacts) Clone() FunctionCallFacts {
	facts.DeclaredReturns = slices.Clone(facts.DeclaredReturns)
	facts.RefinedAlternatives = slices.Clone(facts.RefinedAlternatives)
	facts.Arguments = slices.Clone(facts.Arguments)
	for index := range facts.Arguments {
		facts.Arguments[index].SourceValue = facts.Arguments[index].SourceValue.Clone()
		facts.Arguments[index].Value = facts.Arguments[index].Value.Clone()
	}
	return facts
}
