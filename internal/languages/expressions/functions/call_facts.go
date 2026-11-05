package functions

import (
	"slices"

	exprtypes "actionlint.kjanat.dev/v2/internal/languages/expressions/types"
	exprvalues "actionlint.kjanat.dev/v2/internal/languages/expressions/values"
)

// ArgumentDemand distinguishes certain evaluation from possible or skipped evaluation.
type ArgumentDemand uint8

const (
	DemandRequired ArgumentDemand = iota
	DemandConditional
	DemandSkipped
)

// ArgumentConversion identifies the conversion applied by a parameter.
type ArgumentConversion uint8

const (
	ConversionNone ArgumentConversion = iota
	ConversionString
	ConversionUnavailable
)

// FunctionArgumentFacts separates the accepted parameter contract from the
// argument's source type, conversion and demand when its call is evaluated.
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

// ReturnDomainState reports whether a known result fits a declared return.
type ReturnDomainState uint8

const (
	ReturnDomainUnknown ReturnDomainState = iota
	ReturnDomainContainsValue
	ReturnDomainExcludesValue
)

// FunctionCallFacts preserves declared returns alongside argument facts and
// value-dependent refinements.
type FunctionCallFacts struct {
	// SignatureMatched distinguishes a selected signature from declared returns of a rejected call.
	SignatureMatched bool
	// Demand describes whether this call is required, conditional or skipped in its enclosing expression.
	Demand              ArgumentDemand
	ReturnDomain        ReturnDomainState
	DeclaredReturns     []exprtypes.Type
	RefinedAlternatives []exprtypes.Type
	Arguments           []FunctionArgumentFacts
}

// Clone detaches the call's slices and runtime argument values.
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
