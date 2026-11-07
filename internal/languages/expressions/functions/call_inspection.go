package functions

import (
	exprtypes "actionlint.kjanat.dev/v2/internal/languages/expressions/types"
	runnerjson "actionlint.kjanat.dev/v2/internal/languages/json"
)

// CallInspection describes failures and return-type refinements established from
// already checked arguments. It does not traverse or evaluate expression syntax.
type CallInspection struct {
	ReturnType exprtypes.Type
	Problems   []CallProblemReport
}

// CallProblemReport records a problem and whether its operation is required,
// conditional or skipped within the call. Enclosing expressions can further
// restrict execution without changing the problem itself.
type CallProblemReport struct {
	Problem CallProblem
	Demand  ArgumentDemand
}

func (inspection *CallInspection) addProblem(problem CallProblem, demand ArgumentDemand) {
	if demand != DemandSkipped {
		inspection.Problems = append(inspection.Problems, CallProblemReport{Problem: problem, Demand: demand})
	}
}

// CallProblem describes a function operation independently of diagnostic codes
// and source locations. The caller supplies those when reporting the problem.
//
//sumtype:decl
type CallProblem interface{ callProblem() }

type MissingFormatArgument struct {
	Template     string
	Index, Count int
}

func (MissingFormatArgument) callProblem() {}

type InvalidFormatPlaceholder struct{ Template string }

func (InvalidFormatPlaceholder) callProblem() {}

type UnsupportedFormatSpecifier struct{ Template, Specifier string }

func (UnsupportedFormatSpecifier) callProblem() {}

type InvalidJSON struct {
	Offset  *int
	Message string
}

func (InvalidJSON) callProblem() {}

type JSONMemberCollision struct {
	First, Second string
	Offset        int64
}

func (JSONMemberCollision) callProblem() {}

// InvalidCasePredicate identifies a predicate rejected by the boolean contract.
type InvalidCasePredicate struct {
	Index  int
	Actual exprtypes.Type
}

func (InvalidCasePredicate) callProblem() {}

// InspectArguments accepts a selected signature and its parameter matches.
// Callers validate arity first. Matches can include rejected parameters when the
// function reports that rejection during evaluation, as case() does.
func (function Function) InspectArguments(signature *FuncSignature, arguments []ParameterMatch) CallInspection {
	inspection := CallInspection{ReturnType: signature.Ret}
	if function.inspect != nil {
		function.inspect(arguments, &inspection)
	}
	return inspection
}

func inspectFormat(arguments []ParameterMatch, inspection *CallInspection) {
	argument := arguments[0]
	template, ok := argument.Value.(string)
	if argument.State != ArgumentKnown || !ok {
		return
	}
	count := len(arguments) - 1
	plan, _ := ArgumentFormatPlan(&arguments[0].Argument, count)
	missing := false
	for part := range plan.Parts() {
		if part.Index >= count {
			missing = true
			inspection.addProblem(MissingFormatArgument{Template: template, Index: part.Index, Count: count}, DemandRequired)
		}
	}
	if !plan.Valid() && !missing {
		inspection.addProblem(InvalidFormatPlaceholder{Template: template}, DemandRequired)
	}
	if plan.Valid() {
		for part := range plan.Parts() {
			if part.Index >= 0 && arguments[part.Index+1].Argument.State == ArgumentInvalid {
				break
			}
			if part.Specifier != "" {
				inspection.addProblem(UnsupportedFormatSpecifier{Template: template, Specifier: part.Specifier}, DemandRequired)
				break
			}
		}
	}
}

func inspectFromJSON(arguments []ParameterMatch, inspection *CallInspection) {
	argument := arguments[0]
	text, ok := argument.Value.(string)
	if argument.State != ArgumentKnown || !ok {
		return
	}
	switch read := readFunctionJSON(text).(type) {
	case InvalidJSON:
		inspection.addProblem(read, DemandRequired)
	case unsupportedFunctionJSON:
		inspectJSONMembers(read.parsed, inspection)
	case decodedFunctionJSON:
		inspectJSONMembers(read.parsed, inspection)
		if len(read.parsed.Collisions) == 0 {
			inspection.ReturnType = exprtypes.TypeOfJSONValue(read.parsed.Value)
		}
	}
}

func inspectJSONMembers(parsed runnerjson.ReadResult, inspection *CallInspection) {
	for _, collision := range parsed.Collisions {
		inspection.addProblem(JSONMemberCollision{First: collision.First, Second: collision.Second, Offset: collision.Offset}, DemandRequired)
	}
}
