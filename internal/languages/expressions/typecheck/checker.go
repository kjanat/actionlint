package typecheck

import (
	"fmt"
	"slices"
	"strings"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/exprtypes"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/functions"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/values"
	"actionlint.kjanat.dev/v2/internal/languages/messages"
)

// parseFormatFuncSpecifiers returns references from a syntactically valid template.
// The argument count is checked separately by the call's format plan.
func parseFormatFuncSpecifiers(format string, capacity int) map[int]struct{} {
	references := make(map[int]struct{}, capacity)
	plan := functions.PlanFormat(format, 256)
	if !plan.Valid() {
		return references
	}
	for part := range plan.Parts() {
		if part.Index >= 0 {
			references[part.Index] = struct{}{}
		}
	}
	return references
}

// Checker checks names and types in a caller-supplied scope and records
// function-call facts, evaluation outcomes and language issues.
type Checker struct {
	constants             EvaluationResults
	skipEvaluation        bool
	conditionalEvaluation bool
	funcs                 map[string][]*functions.FuncSignature
	vars                  map[string]exprtypes.Type
	errs                  []*Error
	nodeTypes             map[syntax.Node]exprtypes.Type
	availableContexts     []string
	functionAvailability  map[string]functions.Availability
	functionOverloads     map[string][]*functions.FuncSignature
	narrowContext         func(syntax.Node, bool, func(string) (exprtypes.Type, bool)) (map[string]exprtypes.Type, bool)
	configVars            []string
	configSecrets         []string
	callFacts             map[*syntax.FuncCallNode]functions.FunctionCallFacts
	callSignatures        map[*syntax.FuncCallNode][]*functions.FuncSignature
	evaluationInvalid     bool
	validationFailed      bool
	staticInvalid         bool
}

// NewChecker creates a type checker with caller-supplied context facts.
// A nil configuration permits unknown configured variable and secret names.
func NewChecker(scope Scope, cfg *Options) *Checker {
	knownFunctions := make(map[string][]*functions.FuncSignature, len(functions.Builtins))
	for name, function := range functions.Builtins {
		knownFunctions[name] = function.Signatures
	}
	c := &Checker{
		funcs:         knownFunctions,
		nodeTypes:     make(map[syntax.Node]exprtypes.Type),
		narrowContext: scope.NarrowContext,
	}
	c.SetContextTypes(scope.ContextTypes())
	c.SetAvailability(scope)
	if cfg != nil {
		c.configVars = cfg.Variables
		c.configSecrets = cfg.Secrets
	}
	return c
}

func errorAtExpr(kind findingKind, e syntax.Node, msg string) *Error {
	span := syntax.ExpressionSpan(e)
	return &Error{
		Message: msg,
		Rule:    kind.rule, Code: kind.code,
		Offset:    span.Start,
		EndOffset: span.End,
		Position:  span.Position,
	}
}

func errorfAtExpr(kind findingKind, e syntax.Node, format string, args ...any) *Error {
	return errorAtExpr(kind, e, fmt.Sprintf(format, args...))
}

// validationError records the semantic failure independently of its finding.
func (sema *Checker) validationError(err *Error) {
	sema.validationFailed = true
	sema.staticInvalid = true
	sema.errs = append(sema.errs, err)
}

// evaluationError records a runtime failure only when the expression is demanded.
// A possible argument failure stays local to that argument's evaluation facts.
func (sema *Checker) evaluationError(err *Error) {
	if sema.skipEvaluation {
		return
	}
	sema.evaluationInvalid = true
	if !sema.conditionalEvaluation {
		sema.validationFailed = true
		sema.errs = append(sema.errs, err)
	}
}

func (sema *Checker) errorf(kind findingKind, e syntax.Node, format string, args ...any) {
	sema.validationError(errorfAtExpr(kind, e, format, args...))
}

func errorfAtCallee(kind findingKind, n *syntax.FuncCallNode, format string, args ...any) *Error {
	return errorAtToken(kind, n.Token(), fmt.Sprintf(format, args...))
}

func (sema *Checker) calleeErrorf(kind findingKind, n *syntax.FuncCallNode, format string, args ...any) {
	sema.validationError(errorfAtCallee(kind, n, format, args...))
}

// SetContextTypes replaces the expression's context names and types with a caller-supplied scope.
// Names are case-insensitive. The Checker copies the map.
func (sema *Checker) SetContextTypes(contexts map[string]exprtypes.Type) {
	sema.vars = make(map[string]exprtypes.Type, len(contexts))
	for name, ty := range contexts {
		sema.vars[strings.ToLower(name)] = ty
	}
}

// ContextType returns a context's supplied type and whether it was declared.
// Names are case-insensitive; returned type values are immutable.
func (sema *Checker) ContextType(name string) (exprtypes.Type, bool) {
	ty, found := sema.vars[strings.ToLower(name)]
	return ty, found
}

// SetContextType replaces one context type without changing name availability.
func (sema *Checker) SetContextType(name string, ty exprtypes.Type) {
	sema.vars[strings.ToLower(name)] = ty
}

// SetContextAvailability selects which supplied context names can be referenced.
// An empty list rejects all context references. Names must use lowercase.
// The checker copies the list.
func (sema *Checker) SetContextAvailability(avail []string) {
	sema.availableContexts = slices.Clone(avail)
}

func (sema *Checker) checkAvailableContext(n *syntax.VariableNode) {
	ctx := strings.ToLower(n.Name)
	if slices.Contains(sema.availableContexts, ctx) {
		return
	}

	var notes string
	switch len(sema.availableContexts) {
	case 0:
		notes = "no context is available here"
	case 1:
		notes = "available context is " + messages.Quotes(sema.availableContexts)
	default:
		notes = "available contexts are " + messages.Quotes(sema.availableContexts)
	}
	sema.errorf(unavailableContext,
		n,
		"context %q is not allowed here. %s. see https://docs.github.com/en/actions/learn-github-actions/contexts#context-availability for more details",
		n.Name,
		notes,
	)
}

func (sema *Checker) checkVariable(n *syntax.VariableNode) exprtypes.Type {
	if value, found := sema.vars[n.Name]; found {
		return value
	}
	return exprtypes.AnyType()
}

func (sema *Checker) checkObjectDeref(n *syntax.ObjectDerefNode) exprtypes.Type {
	ty := sema.check(n.Receiver)
	switch ty.Kind() {
	case exprtypes.KindAny:
		return exprtypes.AnyType()
	case exprtypes.KindObject:
		object, _ := ty.Object()
		if t, ok := object.Property(n.Property); ok {
			return t
		}
		if mapped, ok := object.Mapped(); ok {
			if v, ok := n.Receiver.(*syntax.VariableNode); ok {
				switch v.Name {
				case "vars":
					sema.checkConfigVariables(n, n.Property)
				case "secrets":
					sema.checkConfigSecrets(n, n.Property)
				}
			}
			return mapped
		}
		if receiver, ok := n.Receiver.(*syntax.VariableNode); ok {
			// User-defined properties can resemble context names without referring
			// to them, such as inputs.github_token or matrix.runner_os.
			switch receiver.Name {
			case "github", "job", "runner", "strategy":
				if suggestion := sema.contextPropertySuggestion(n.Property); suggestion != "" {
					sema.errorf(unknownProperty, n, "property %q is not defined in this context; did you mean %q?", n.PropertyName(), suggestion)
					return exprtypes.AnyType()
				}
			}
		}
		sema.missingProperty(n, n.Receiver, n.PropertyName(), object)
		return exprtypes.AnyType()
	case exprtypes.KindArray:
		array, _ := ty.Array()
		if !array.Deref() {
			sema.errorf(operandType, n, "receiver of object dereference %q must be type of object but got %q", n.Property, ty.String())
			return exprtypes.AnyType()
		}
		elem := array.Elem()
		if elem.Kind() == exprtypes.KindAny {
			// When element type is any, map the any type to any. Reuse `ty`
			return ty
		}
		et, ok := elem.Object()
		if !ok {
			sema.errorf(operandType,
				n,
				"property filtered by %q at object filtering must be type of object but got %q",
				n.Property,
				elem.String(),
			)
			return exprtypes.AnyType()
		}
		// Map the element type of the dereferenced array.
		mappedElem := exprtypes.AnyType()
		if t, ok := et.Property(n.Property); ok {
			mappedElem = t
		} else if mapped, ok := et.Mapped(); ok {
			mappedElem = mapped
		} else {
			sema.missingProperty(n, n.Receiver, n.PropertyName(), et)
		}
		return exprtypes.NewFilteredArrayType(mappedElem).Type()
	case exprtypes.KindNull, exprtypes.KindNumber, exprtypes.KindBool, exprtypes.KindString:
	}
	sema.errorf(operandType, n, "receiver of object dereference %q must be type of object but got %q", n.Property, ty.String())
	return exprtypes.AnyType()
}

// Suggest only explicit properties in contexts available at this expression.
// Matching the context prefix preserves distinctions such as job.workflow_sha
// (the called workflow) versus github.workflow_sha (the caller).
func (sema *Checker) contextPropertySuggestion(name string) string {
	return ContextPropertySuggestion(name, sema.availableContexts, sema.vars)
}

func ContextPropertySuggestion(name string, contexts []string, variables map[string]exprtypes.Type) string {
	var suggestion string
	for _, context := range contexts {
		property, ok := strings.CutPrefix(name, context+"_")
		if !ok {
			continue
		}
		object, ok := variables[context].Object()
		if !ok {
			continue
		}
		if _, exists := object.Property(property); !exists {
			continue
		}
		if suggestion != "" {
			return ""
		}
		suggestion = context + "." + property
	}
	return suggestion
}

func (sema *Checker) checkConfigVariables(n syntax.Node, prop string) {
	// https://docs.github.com/en/actions/learn-github-actions/variables#naming-conventions-for-configuration-variables
	if strings.HasPrefix(prop, "github_") {
		sema.errorf(invalidVariableName,
			n,
			"configuration variable name %q must not start with the GITHUB_ prefix (case insensitive). note: see the convention at https://docs.github.com/en/actions/learn-github-actions/variables#naming-conventions-for-configuration-variables",
			prop,
		)
		return
	}
	for _, r := range prop {
		// Note: `prop` was already converted to lower case
		// Note: First character cannot be number, but it was already checked by parser
		if '0' <= r && r <= '9' || 'a' <= r && r <= 'z' || r == '_' {
			continue
		}
		sema.errorf(invalidVariableName,
			n,
			"configuration variable name %q can only contain alphabets, decimal numbers, and '_'. note: see the convention at https://docs.github.com/en/actions/learn-github-actions/variables#naming-conventions-for-configuration-variables",
			prop,
		)
		return
	}

	if sema.configVars == nil {
		return
	}
	if len(sema.configVars) == 0 {
		sema.errorf(unknownConfigVariable,
			n,
			"no configuration variable is allowed since the variables list is empty in actionlint.yaml. you may forget adding the variable %q to the list",
			prop,
		)
		return
	}

	for _, v := range sema.configVars {
		if strings.EqualFold(v, prop) {
			return
		}
	}

	sema.errorf(unknownConfigVariable,
		n,
		"undefined configuration variable %q. defined configuration variables in actionlint.yaml are %s",
		prop,
		messages.SortedQuotes(sema.configVars),
	)
}

func (sema *Checker) checkConfigSecrets(n syntax.Node, prop string) {
	if sema.configSecrets == nil {
		return
	}
	if len(sema.configSecrets) == 0 {
		sema.errorf(unknownSecret,
			n,
			"no secret is allowed since the secrets list is empty in actionlint.yaml. you may forget adding the secret %q to the list",
			prop,
		)
		return
	}
	if slices.ContainsFunc(sema.configSecrets, func(s string) bool {
		return strings.EqualFold(s, prop)
	}) {
		return
	}
	sema.errorf(unknownSecret,
		n,
		"undefined secret %q. defined secrets in actionlint.yaml are %s",
		prop,
		messages.SortedQuotes(sema.configSecrets),
	)
}

func (sema *Checker) checkArrayDeref(n *syntax.ArrayDerefNode) exprtypes.Type {
	ty := sema.check(n.Receiver)
	switch ty.Kind() {
	case exprtypes.KindAny:
		return exprtypes.NewFilteredArrayType(exprtypes.AnyType()).Type()
	case exprtypes.KindArray:
		array, _ := ty.Array()
		return exprtypes.NewFilteredArrayType(array.Elem()).Type()
	case exprtypes.KindObject:
		// Object filtering is available for objects, not only arrays (#66)
		object, _ := ty.Object()

		if mapped, ok := object.Mapped(); ok {
			// For map object or loose object at receiver of .*
			switch mapped.Kind() {
			case exprtypes.KindAny, exprtypes.KindObject:
				return exprtypes.NewFilteredArrayType(mapped).Type()
			case exprtypes.KindNull, exprtypes.KindNumber, exprtypes.KindBool, exprtypes.KindString, exprtypes.KindArray:
			}
			sema.errorf(operandType, n, "elements of object at receiver of object filtering `.*` must be type of object but got %q. the type of receiver was %q", mapped.String(), ty.String())
			return exprtypes.AnyType()
		}

		// Payload object filters select scalar members as well as object members.
		if path := strings.ToLower(syntax.DisplayReference(n.Receiver)); path == "github.event" || strings.HasPrefix(path, "github.event.") || strings.HasPrefix(path, "github.event[") {
			return exprtypes.NewFilteredArrayType(exprtypes.AnyType()).Type()
		}
		// For strict object at receiver of .*
		found := false
		for _, t := range object.Properties() {
			if t.Kind() == exprtypes.KindObject {
				found = true
				break
			}
		}
		if !found {
			sema.errorf(operandType, n, "object type %q cannot be filtered by object filtering `.*` since it has no object element", ty.String())
			return exprtypes.AnyType()
		}

		return exprtypes.NewFilteredArrayType(exprtypes.AnyType()).Type()
	case exprtypes.KindNull, exprtypes.KindNumber, exprtypes.KindBool, exprtypes.KindString:
	}
	sema.errorf(operandType, n, "receiver of object filtering `.*` must be type of array or object but got %q", ty.String())
	return exprtypes.AnyType()
}

func (sema *Checker) checkIndexAccess(n *syntax.IndexAccessNode) exprtypes.Type {
	idx := sema.check(n.Index)

	ty := sema.check(n.Operand)
	switch ty.Kind() {
	case exprtypes.KindAny:
		return exprtypes.AnyType()
	case exprtypes.KindArray:
		array, _ := ty.Array()
		if k := idx.Kind(); k == exprtypes.KindAny || k == exprtypes.KindNumber {
			return array.Elem()
		}
		sema.errorf(operandType, n.Index, "index access of array must be type of number but got %q", idx.String())
		return exprtypes.AnyType()
	case exprtypes.KindObject:
		object, _ := ty.Object()
		switch idx.Kind() {
		case exprtypes.KindAny:
			return exprtypes.AnyType()
		case exprtypes.KindString:
			// Index access with String literal like foo['bar']
			if lit, ok := n.Index.(*syntax.StringNode); ok {
				if prop, ok := object.Property(strings.ToLower(lit.Value)); ok {
					return prop
				}
				if mapped, ok := object.Mapped(); ok {
					if v, ok := n.Operand.(*syntax.VariableNode); ok {
						prop := strings.ToLower(lit.Value)
						switch v.Name {
						case "vars":
							sema.checkConfigVariables(n, prop)
						case "secrets":
							sema.checkConfigSecrets(n, prop)
						}
					}
					return mapped
				}
				sema.missingProperty(n, n.Operand, lit.Value, object)
			}
			if mapped, ok := object.Mapped(); ok {
				return mapped
			}
			return exprtypes.AnyType() // Fallback
		case exprtypes.KindNull, exprtypes.KindNumber, exprtypes.KindBool, exprtypes.KindObject, exprtypes.KindArray:
		}
		sema.errorf(operandType, n.Index, "property access of object must be type of string but got %q", idx.String())
		return exprtypes.AnyType()
	case exprtypes.KindNull, exprtypes.KindNumber, exprtypes.KindBool, exprtypes.KindString:
	}
	sema.errorf(operandType, n, "index access operand must be type of object or array but got %q", ty.String())
	return exprtypes.AnyType()
}

type signatureMatch struct {
	signature  *functions.FuncSignature
	arityValid bool
	arguments  []functions.ParameterMatch
	err        *Error
}

func checkFuncSignature(n *syntax.FuncCallNode, sig *functions.FuncSignature, args []functions.CallArgument) signatureMatch {
	match := signatureMatch{signature: sig}
	if err := checkCallArity(n, sig); err != nil {
		match.err = err
		return match
	}
	la := len(args)
	match.arityValid = true
	for i, arg := range args {
		p := sig.ParameterAt(i, la)
		m := functions.MatchParameter(p, arg, sig.CompositeStringFallback)
		match.arguments = append(match.arguments, m)
		if !m.Accepted && match.err == nil {
			match.err = errorfAtExpr(argumentType, n.Args[i],
				"%s argument of function call is not assignable. %q cannot be assigned to %q. called function type is %q",
				messages.Ordinal(i+1), arg.Source.String(), p.String(), sig.String())
		}
	}
	return match
}

// commonReturn reports a return contract only when every candidate declares
// the same type. Type.Merge has expression-specific coercion semantics and is
// not a union of overload return contracts.
func commonReturn(matches []signatureMatch) (exprtypes.Type, bool) {
	if len(matches) == 0 {
		return exprtypes.AnyType(), false
	}
	ret := matches[0].signature.Ret
	for _, match := range matches[1:] {
		if !ret.Equal(match.signature.Ret) {
			return exprtypes.AnyType(), false
		}
	}
	return ret, true
}

func (sema *Checker) checkBuiltinFuncCall(n *syntax.FuncCallNode, match signatureMatch) exprtypes.Type {
	sig := match.signature
	if sema.skipEvaluation {
		return sig.Ret
	}
	function := functions.Builtins[strings.ToLower(n.Callee)]
	inspection := function.InspectArguments(sig, match.arguments)
	sema.reportCallProblems(n, inspection.Problems)
	return inspection.ReturnType
}

func (sema *Checker) checkDemandedExpression(node syntax.Node, demand functions.ArgumentDemand) (exprtypes.Type, bool) {
	var ty exprtypes.Type
	invalid := sema.withEvaluationDemand(demand, func() { ty = sema.check(node) })
	return ty, invalid
}

// withEvaluationDemand isolates failures from conditional and skipped operations
// while preserving failures already established in the enclosing expression.
func (sema *Checker) withEvaluationDemand(demand functions.ArgumentDemand, operation func()) bool {
	previousInvalid := sema.evaluationInvalid
	previousConditional := sema.conditionalEvaluation
	previousSkipped := sema.skipEvaluation
	sema.evaluationInvalid = false
	sema.conditionalEvaluation = previousConditional || demand == functions.DemandConditional
	sema.skipEvaluation = previousSkipped || demand == functions.DemandSkipped
	operation()
	invalid := sema.evaluationInvalid
	sema.evaluationInvalid = previousInvalid || (invalid && demand == functions.DemandRequired)
	sema.conditionalEvaluation = previousConditional
	sema.skipEvaluation = previousSkipped
	return invalid
}

func (sema *Checker) checkExpressionBranch(node syntax.Node, demand functions.ArgumentDemand) exprtypes.Type {
	ty, _ := sema.checkDemandedExpression(node, demand)
	return ty
}

func (sema *Checker) checkCallArgument(node syntax.Node, demand functions.ArgumentDemand) functions.CallArgument {
	ty, invalid := sema.checkDemandedExpression(node, demand)
	arg := functions.CallArgument{Source: ty, Demand: demand}
	if invalid && demand != functions.DemandSkipped {
		arg.State = functions.ArgumentInvalid
	} else if demand != functions.DemandSkipped {
		if value, found := sema.constants.Result(node); found && value.State == values.EvaluationKnown {
			arg.Value, arg.State = value.Value, functions.ArgumentKnown
		}
	}
	return arg
}

func (sema *Checker) checkCaseCall(n *syntax.FuncCallNode, sig *functions.FuncSignature) exprtypes.Type {
	var alternatives []exprtypes.Type
	args := make([]functions.CallArgument, 0, len(n.Args))
	ty, _ := sema.checkCaseBranches(n.Args, &alternatives, &args, len(n.Args))
	match := checkFuncSignature(n, sig, args)
	sema.recordCallArguments(n, match)
	if !sema.skipEvaluation {
		function := functions.Builtins[strings.ToLower(n.Callee)]
		inspection := function.InspectArguments(sig, match.arguments)
		sema.reportCallProblems(n, inspection.Problems)
	}
	facts := sema.callFacts[n]
	facts.RefinedAlternatives = alternatives
	sema.callFacts[n] = facts
	return ty
}

// checkCaseBranches checks one predicate/value pair and recurses into the rest.
// The result type merges every reachable value branch with the final default.
// Predicates never contribute. Branches made unreachable by constant predicates
// are still checked but skip evaluation, so their types are left out of the
// merge. The second result reports whether any branch in args was reachable.
func (sema *Checker) checkCaseBranches(args []syntax.Node, alternatives *[]exprtypes.Type, checked *[]functions.CallArgument, count int) (exprtypes.Type, bool) {
	check := func(node syntax.Node) functions.CallArgument {
		demand := functions.DemandForArgument(functions.EvaluateCase, len(*checked), count, *checked)
		arg := sema.checkCallArgument(node, demand)
		*checked = append(*checked, arg)
		return arg
	}
	if len(args) == 1 {
		arg := check(args[0])
		reachable := arg.Demand != functions.DemandSkipped
		if reachable {
			*alternatives = append(*alternatives, arg.Source)
		}
		return arg.Source, reachable
	}
	check(args[0])
	var valueArgument functions.CallArgument
	valueType := sema.withBranchContext(args[0], true, func() exprtypes.Type {
		valueArgument = check(args[1])
		return valueArgument.Source
	})
	valueReachable := valueArgument.Demand != functions.DemandSkipped
	if valueReachable {
		*alternatives = append(*alternatives, valueType)
	}
	// Later predicates and the default are reachable only when this predicate
	// is false. Keep those guards nested so all earlier exclusions apply.
	var restType exprtypes.Type
	var restReachable bool
	sema.withBranchContext(args[0], false, func() exprtypes.Type {
		restType, restReachable = sema.checkCaseBranches(args[2:], alternatives, checked, count)
		return restType
	})
	switch {
	case valueReachable && restReachable:
		return valueType.Merge(restType), true
	case valueReachable:
		return valueType, true
	case !restReachable:
		return exprtypes.AnyType(), false
	default:
		return restType, restReachable
	}
}

func (sema *Checker) checkFuncCall(n *syntax.FuncCallNode) exprtypes.Type {
	sigs := sema.callSignatures[n]
	if len(sigs) == 0 {
		return exprtypes.AnyType()
	}

	declared := make([]exprtypes.Type, 0, len(sigs))
	for _, sig := range sigs {
		if !slices.ContainsFunc(declared, func(ty exprtypes.Type) bool { return ty.Equal(sig.Ret) }) {
			declared = append(declared, sig.Ret)
		}
	}
	demand := functions.DemandRequired
	if sema.skipEvaluation {
		demand = functions.DemandSkipped
	} else if sema.conditionalEvaluation {
		demand = functions.DemandConditional
	}
	sema.callFacts[n] = functions.FunctionCallFacts{DeclaredReturns: declared, Demand: demand}

	policy := sigs[0].EvaluationPolicy
	if policy == functions.EvaluateCase && len(n.Args) >= 3 && len(n.Args)%2 == 1 && (sigs[0].MaxParams == 0 || len(n.Args) <= sigs[0].MaxParams) {
		return sema.checkCaseCall(n, sigs[0])
	}
	args := make([]functions.CallArgument, len(n.Args))
	if len(n.Args) > 0 {
		args[0] = sema.checkCallArgument(n.Args[0], functions.DemandRequired)
		blocked := args[0].State == functions.ArgumentInvalid
		for _, index := range functions.ArgumentExecutionOrder(policy, len(n.Args), args) {
			demand := functions.DemandForArgument(policy, index, len(n.Args), args)
			if blocked {
				demand = functions.DemandSkipped
			}
			args[index] = sema.checkCallArgument(n.Args[index], demand)
			blocked = blocked || (demand == functions.DemandRequired && args[index].State == functions.ArgumentInvalid)
		}
	}

	var candidates, accepted []signatureMatch
	for _, sig := range sigs {
		match := checkFuncSignature(n, sig, args)
		candidates = append(candidates, match)
		if match.err == nil {
			accepted = append(accepted, match)
		}
	}
	if len(accepted) == 0 {
		// Arity is checked during parsing even in an unevaluated branch.
		// Conversion failures only matter when the call is evaluated.
		matchingArity := slices.DeleteFunc(slices.Clone(candidates), func(candidate signatureMatch) bool { return !candidate.arityValid })
		if len(matchingArity) > 0 {
			for _, candidate := range matchingArity {
				sema.evaluationError(candidate.err)
			}
			sema.recordCallArguments(n, matchingArity[0])
		} else if len(candidates) > 0 {
			sema.recordCallArguments(n, candidates[0])
		}
		ret, _ := commonReturn(candidates)
		return ret
	}
	_, common := commonReturn(accepted)
	if !common {
		return exprtypes.AnyType()
	}
	match := accepted[0]
	// Prefer the overload requiring fewer conversions or fallback behaviors.
	score := func(candidate signatureMatch) int {
		n := 0
		for _, arg := range candidate.arguments {
			if arg.Conversion != functions.ConversionNone {
				n++
			}
		}
		return n
	}
	for _, candidate := range accepted[1:] {
		if score(candidate) < score(match) {
			match = candidate
		}
	}
	// Refinement uses the selected call and cannot replace its declared return
	// contract when matching overloads have different returns.
	refined := sema.checkBuiltinFuncCall(n, match)
	sema.recordCallArguments(n, match)
	facts := sema.callFacts[n]
	if !refined.Equal(match.signature.Ret) {
		facts.RefinedAlternatives = []exprtypes.Type{refined}
	}
	sema.callFacts[n] = facts
	return refined
}

func (sema *Checker) recordCallArguments(n *syntax.FuncCallNode, match signatureMatch) {
	facts := sema.callFacts[n]
	facts.SignatureMatched = match.err == nil
	for _, arg := range match.arguments {
		value := values.EvaluationResult{State: values.EvaluationState(arg.State), Value: arg.Value}
		if arg.Argument.Demand == functions.DemandSkipped {
			value = values.EvaluationResult{State: values.EvaluationUnknown}
		}
		facts.Arguments = append(facts.Arguments, functions.FunctionArgumentFacts{
			Source:      arg.Argument.Source,
			SourceValue: values.EvaluationResult{State: values.EvaluationState(arg.Argument.State), Value: arg.Argument.Value},
			Accepted:    arg.Accepted,
			Demand:      arg.Argument.Demand,
			Parameter:   arg.Parameter,
			Converted:   arg.Converted,
			Conversion:  arg.Conversion,
			Value:       value,
		})
	}
	sema.callFacts[n] = facts
}

func (sema *Checker) checkNotOp(n *syntax.NotOpNode) exprtypes.Type {
	ty := sema.check(n.Operand)
	if !exprtypes.BoolType().Assignable(ty) {
		sema.errorf(operandType, n, "type of operand of ! operator %q is not assignable to type \"bool\"", ty.String())
	}
	return exprtypes.BoolType()
}

func (sema *Checker) checkCompareOp(n *syntax.CompareOpNode) exprtypes.Type {
	sema.check(n.Left)
	sema.check(n.Right)

	return exprtypes.BoolType()
}

// checkWithNarrowing checks type of given expression with type narrowing. Type narrowing narrows
// down the type of the expression by assuming its value. For example, `l && r` is typed as
// `typeof(l) | typeof(r)` usually. However when the expression is assumed to be true, its type can
// be narrowed down to `typeof(r)`.
// This analysis is useful to make type checking more accurate. For example, `some_var && 60 || 20`
// can be typed as `number` instead of `typeof(some_var) | number`. (#384)
func (sema *Checker) checkWithNarrowing(n syntax.Node, isTruthy bool) exprtypes.Type {
	switch n := n.(type) {
	case *syntax.LogicalOpNode:
		switch n.Kind {
		case syntax.LogicalOpNodeKindAnd:
			// When `l && r` is true, narrow its type to `typeof(r)`
			if isTruthy {
				left := sema.check(n.Left)
				right := sema.withBranchContext(n.Left, true, func() exprtypes.Type { return sema.checkExpressionBranch(n.Right, sema.logicalRightDemand(n)) })
				sema.nodeTypes[n] = left.Merge(right)
				return right
			}
		case syntax.LogicalOpNodeKindOr:
			// When `l || r` is false, narrow its type to `typeof(r)`
			if !isTruthy {
				left := sema.check(n.Left)
				right := sema.withBranchContext(n.Left, false, func() exprtypes.Type { return sema.checkExpressionBranch(n.Right, sema.logicalRightDemand(n)) })
				sema.nodeTypes[n] = left.Merge(right)
				return right
			}
		case syntax.LogicalOpNodeKindInvalid:
			// Invalid operators are not narrowed.
		}
		return sema.checkLogicalOp(n)
	case *syntax.NotOpNode:
		sema.nodeTypes[n] = exprtypes.BoolType()
		return sema.checkWithNarrowing(n.Operand, !isTruthy)
	default:
		return sema.check(n)
	}
}

func (sema *Checker) checkLogicalOp(n *syntax.LogicalOpNode) (ty exprtypes.Type) {
	defer func() { sema.nodeTypes[n] = ty }()

	switch n.Kind {
	case syntax.LogicalOpNodeKindAnd:
		// When `l` is false in `l && r`, its type is `typeof(l)`. Otherwise `typeof(r)`.
		// Narrow the type of LHS expression by assuming its value is falsy.
		left := sema.checkWithNarrowing(n.Left, false)
		right := sema.withBranchContext(n.Left, true, func() exprtypes.Type { return sema.checkExpressionBranch(n.Right, sema.logicalRightDemand(n)) })
		return left.Merge(right)
	case syntax.LogicalOpNodeKindOr:
		// When `l` is true in `l || r`, its type is `typeof(l)`. Otherwise it is `typeof(r)`.
		// Narrow the type of LHS expression by assuming its value is truthy.
		left := sema.checkWithNarrowing(n.Left, true)
		right := sema.withBranchContext(n.Left, false, func() exprtypes.Type { return sema.checkExpressionBranch(n.Right, sema.logicalRightDemand(n)) })
		return left.Merge(right)
	case syntax.LogicalOpNodeKindInvalid:
		// Invalid operators still check both operands.
		fallthrough
	default:
		sema.check(n.Left)
		sema.check(n.Right)
		return exprtypes.AnyType()
	}
}

func (sema *Checker) check(expr syntax.Node) (ty exprtypes.Type) {
	defer func() { sema.nodeTypes[expr] = ty }()

	switch e := expr.(type) {
	case *syntax.VariableNode:
		return sema.checkVariable(e)
	case *syntax.NullNode:
		return exprtypes.NullType()
	case *syntax.BoolNode:
		return exprtypes.BoolType()
	case *syntax.StringNode:
		return exprtypes.StringType()
	case *syntax.IntNode, *syntax.FloatNode:
		return exprtypes.NumberType()
	case *syntax.ObjectDerefNode:
		return sema.checkObjectDeref(e)
	case *syntax.ArrayDerefNode:
		return sema.checkArrayDeref(e)
	case *syntax.IndexAccessNode:
		return sema.checkIndexAccess(e)
	case *syntax.FuncCallNode:
		return sema.checkFuncCall(e)
	case *syntax.NotOpNode:
		return sema.checkNotOp(e)
	case *syntax.CompareOpNode:
		return sema.checkCompareOp(e)
	case *syntax.LogicalOpNode:
		return sema.checkLogicalOp(e)
	default:
		panic("unreachable")
	}
}

// ExpressionCheckResult keeps inferred types, findings and failure phases
// separate. An invalid call can still have a declared return type.
type ExpressionCheckResult struct {
	Type       exprtypes.Type
	Types      InferredTypes
	Calls      CheckedCalls
	Valid      bool
	Findings   []*Error
	Evaluation values.EvaluationResult
	// Call is nil when the root expression has no known function contract.
	Call         *functions.FunctionCallFacts
	FailurePhase FailurePhase
}

// FailurePhase identifies whether validation rejected the expression or a
// demanded evaluation failed. Findings alone do not determine this phase.
type FailurePhase uint8

const (
	FailureNone FailurePhase = iota
	FailureStaticValidation
	FailureEvaluation
)

// CheckResult validates names and infers types using the supplied value results.
func (sema *Checker) CheckResult(input EvaluationResults) ExpressionCheckResult {
	expr := input.Expression()
	sema.errs = []*Error{}
	sema.callFacts = make(map[*syntax.FuncCallNode]functions.FunctionCallFacts)
	sema.nodeTypes = make(map[syntax.Node]exprtypes.Type)
	sema.evaluationInvalid = false
	sema.validationFailed = false
	sema.staticInvalid = false
	sema.validateReferences(expr)
	sema.constants = input
	ty := sema.check(expr)
	errs := detachErrors(sema.errs)

	evaluation := values.EvaluationResult{State: values.EvaluationUnknown}
	phase := FailureNone
	switch {
	case sema.staticInvalid:
		evaluation.State = values.EvaluationNotAttempted
		phase = FailureStaticValidation
	case sema.evaluationInvalid:
		evaluation.State = values.EvaluationInvalid
		phase = FailureEvaluation
	default:
		evaluation, _ = sema.constants.Result(expr)
		if evaluation.State == values.EvaluationInvalid {
			phase = FailureEvaluation
		}
	}
	result := ExpressionCheckResult{Type: ty, Types: InferredTypes{byNode: sema.nodeTypes}, Valid: !sema.validationFailed, Findings: errs, Evaluation: evaluation, FailurePhase: phase}
	if call, ok := expr.(*syntax.FuncCallNode); ok {
		if facts, found := sema.callFacts[call]; found {
			if evaluation.State == values.EvaluationKnown {
				facts.ReturnDomain = functions.ReturnDomainExcludesValue
				for _, declared := range facts.DeclaredReturns {
					if returnContainsValue(declared, evaluation.Value) {
						facts.ReturnDomain = functions.ReturnDomainContainsValue
						break
					}
				}
			}
			if facts.ReturnDomain == functions.ReturnDomainContainsValue {
				actual := exprtypes.TypeOfJSONValue(evaluation.Value)
				if !actual.Equal(result.Type) {
					facts.RefinedAlternatives = []exprtypes.Type{actual}
				}
			}
			sema.callFacts[call] = facts
			detached := facts.Clone()
			result.Call = &detached
		}
	}
	result.Calls = CheckedCalls{byNode: sema.callFacts}
	return result
}

// returnContainsValue checks actual values against the declared shape without
// applying expression coercions or the compatibility rules in Assignable.
func returnContainsValue(declared exprtypes.Type, value any) bool {
	if declared.Kind() == exprtypes.KindAny {
		return true
	}
	switch v := value.(type) {
	case nil:
		return declared.Kind() == exprtypes.KindNull
	case bool:
		return declared.Kind() == exprtypes.KindBool
	case float64:
		return declared.Kind() == exprtypes.KindNumber
	case string:
		return declared.Kind() == exprtypes.KindString
	case []any:
		array, ok := declared.Array()
		if !ok {
			return false
		}
		for _, item := range v {
			if !returnContainsValue(array.Elem(), item) {
				return false
			}
		}
		return true
	case map[string]any:
		object, ok := declared.Object()
		if !ok {
			return false
		}
		for name, item := range v {
			if property, exists := object.Property(strings.ToLower(name)); exists {
				if !returnContainsValue(property, item) {
					return false
				}
			} else {
				mapped, ok := object.Mapped()
				if !ok || !returnContainsValue(mapped, item) {
					return false
				}
			}
		}
		return true
	default:
		return false
	}
}

// Check returns the inferred type and findings using precomputed value results.
func (sema *Checker) Check(input EvaluationResults) (exprtypes.Type, []*Error) {
	result := sema.CheckResult(input)
	return result.Type, result.Findings
}

// IsConstant reports whether the expression is constant. For example, these expressions
// are constants.
//   - 1 == 2
//   - (!true && 'foo') || 'bar'
//   - startsWith('foobar', 'foo')
//   - format('{} + {} = {}', 1, 2, 3)
func (sema *Checker) IsConstant(expr syntax.Node) bool {
	switch e := expr.(type) {
	case *syntax.NullNode, *syntax.BoolNode, *syntax.IntNode, *syntax.FloatNode, *syntax.StringNode:
		return true
	case *syntax.VariableNode, *syntax.ObjectDerefNode, *syntax.ArrayDerefNode, *syntax.IndexAccessNode:
		return false
	case *syntax.NotOpNode:
		return sema.IsConstant(e.Operand)
	case *syntax.CompareOpNode:
		return sema.IsConstant(e.Left) && sema.IsConstant(e.Right)
	case *syntax.LogicalOpNode:
		return sema.IsConstant(e.Left) && sema.IsConstant(e.Right)
	case *syntax.FuncCallNode:
		for _, a := range e.Args {
			if !sema.IsConstant(a) {
				return false
			}
		}
		sigs, ok := sema.funcs[strings.ToLower(e.Callee)]
		if !ok {
			return false
		}
		for _, s := range sigs {
			if !s.IsConstFunc {
				return false
			}
		}
		return true
	default:
		panic("unreachable")
	}
}
