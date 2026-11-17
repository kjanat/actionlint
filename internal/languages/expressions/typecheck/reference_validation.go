package typecheck

import (
	"maps"
	"slices"
	"strings"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/functions"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
	"actionlint.kjanat.dev/v2/internal/languages/messages"
)

// validateReferences checks context names, availability and function signatures
// before branch refinement. Execution reachability does not change those rules.
func (sema *Checker) validateReferences(expr syntax.Node) {
	sema.callSignatures = make(map[*syntax.FuncCallNode][]*functions.FuncSignature)
	syntax.Visit(expr, func(node, _ syntax.Node, entering bool) {
		if !entering {
			return
		}
		if variable, ok := node.(*syntax.VariableNode); ok {
			sema.validateContextReference(variable)
			return
		}
		call, ok := node.(*syntax.FuncCallNode)
		if !ok {
			return
		}
		signatures := sema.resolveFunction(call)
		sema.callSignatures[call] = signatures
		if len(signatures) == 0 {
			return
		}
		sema.checkFunctionAvailability(call)
		var failures []*Error
		for _, signature := range signatures {
			err := checkCallArity(call, signature)
			if err == nil {
				return
			}
			failures = append(failures, err)
		}
		for _, err := range failures {
			sema.validationError(err)
		}
	})
}

func (sema *Checker) validateContextReference(n *syntax.VariableNode) {
	_, ok := sema.vars[n.Name]
	if !ok {
		if sigs, known := sema.funcs[n.Name]; known && len(sigs) > 0 {
			name := n.Token().Value
			for _, sig := range sigs {
				if len(sig.Params) == 0 {
					sema.errorf(missingCall, n, "function %q must be called with parentheses; did you mean %q?", name, name+"()")
					return
				}
			}
			sema.errorf(missingCall, n, "function %q must be called with parentheses and arguments", name)
			return
		}
		if inputs, ok := sema.vars["inputs"].Object(); ok && inputs.IsStrict() && slices.Contains(sema.availableContexts, "inputs") {
			if _, declared := inputs.Property(n.Name); declared {
				sema.errorf(unknownVariable, n, "undefined variable %q; did you mean %q?", n.Token().Value, "inputs."+n.Name)
				return
			}
		}
		if suggestion := sema.contextPropertySuggestion(n.Name); suggestion != "" {
			sema.errorf(unknownVariable, n, "undefined variable %q; did you mean %q?", n.Token().Value, suggestion)
			return
		}
		available := make([]string, 0, len(sema.availableContexts))
		for _, name := range sema.availableContexts {
			if _, exists := sema.vars[name]; exists {
				available = append(available, name)
			}
		}
		if suggestion := messages.CloseName(n.Name, available); suggestion != "" {
			sema.errorf(unknownVariable, n, "undefined variable %q; did you mean %q?", n.Token().Value, suggestion)
			return
		}
		ss := make([]string, 0, len(sema.vars))
		for n := range sema.vars {
			ss = append(ss, n)
		}
		sema.errorf(unknownVariable, n, "undefined variable %q. available variables are %s", n.Token().Value, messages.SortedQuotes(ss))
		return
	}

	sema.checkAvailableContext(n)
}

func (sema *Checker) checkFunctionAvailability(n *syntax.FuncCallNode) {
	availability, denied := sema.functionRestriction(n.Callee)
	if !denied {
		return
	}

	sema.calleeErrorf(unavailableFunction,
		n,
		"calling function %q is not allowed here. %q is only available in %s. see https://docs.github.com/en/actions/learn-github-actions/contexts#context-availability for more details",
		n.Callee,
		n.Callee,
		messages.Quotes(availability.AvailableAt),
	)
}

func checkCallArity(n *syntax.FuncCallNode, sig *functions.FuncSignature) *Error {
	lp, la := len(sig.Params), len(n.Args)
	if sig.MaxParams > 0 && la > sig.MaxParams {
		return errorfAtCallee(argumentCount, n, "function %q takes at most %d arguments but %d arguments are given", n.Callee, sig.MaxParams, la)
	}
	if sig.VariableLengthParams && (lp > la) || !sig.VariableLengthParams && lp != la {
		atLeast := ""
		if sig.VariableLengthParams {
			atLeast = "at least "
		}
		return errorfAtCallee(argumentCount, n,
			"number of arguments is wrong. function %q takes %s%d parameters but %d arguments are given",
			sig.String(), atLeast, lp, la)
	}
	if sig.EvaluationPolicy == functions.EvaluateCase && la%2 == 0 {
		return errorfAtCallee(argumentCount, n, "case() requires an odd number of arguments (pred/value pairs + default) but got %d", la)
	}
	return nil
}

func (sema *Checker) resolveFunction(n *syntax.FuncCallNode) []*functions.FuncSignature {
	// Check function name in case insensitive. For example, toJson and toJSON are the same function.
	callee := strings.ToLower(n.Callee)
	sigs, ok := sema.funcs[callee]
	if overloads, overridden := sema.functionOverloads[callee]; overridden {
		sigs, ok = overloads, true
	}
	if !ok {
		knownFunctions := maps.Clone(sema.funcs)
		maps.Copy(knownFunctions, sema.functionOverloads)
		candidates := make([]string, 0, len(knownFunctions))
		for name, signatures := range knownFunctions {
			if _, denied := sema.functionRestriction(name); denied {
				continue
			}
			if len(signatures) > 0 {
				candidates = append(candidates, signatures[0].Name)
			}
		}
		if suggestion := messages.CloseName(callee, candidates); suggestion != "" {
			sema.calleeErrorf(unknownFunction, n, "undefined function %q; did you mean %q?", n.Callee, suggestion)
			return nil
		}
		ss := make([]string, 0, len(knownFunctions))
		for n := range knownFunctions {
			ss = append(ss, n)
		}
		sema.calleeErrorf(unknownFunction, n, "undefined function %q. available functions are %s", n.Callee, messages.SortedQuotes(ss))
		return nil
	}

	return sigs
}
