package expressions

import (
	"slices"
	"strings"

	"actionlint.kjanat.dev/v2/internal/checking/rules/correctness"

	"actionlint.kjanat.dev/v2/internal/checking/rules/security"
	"actionlint.kjanat.dev/v2/internal/checking/rules/suspicious"

	exprfunctions "actionlint.kjanat.dev/v2/internal/languages/expressions/functions"

	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"

	exprtypes "actionlint.kjanat.dev/v2/internal/languages/expressions/types"

	exprlang "actionlint.kjanat.dev/v2/internal/languages/expressions/typecheck"

	actionmeta "actionlint.kjanat.dev/v2/internal/model/actions"

	contextspec "actionlint.kjanat.dev/v2/internal/model/contexts"

	value "actionlint.kjanat.dev/v2/internal/model/contexts/values"

	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

func (rule *Checker) checkExprsIn(s string, pos *workflowlang.Pos, quoted, checkUntrusted bool, workflowKey string) ([]typedExpr, bool) {
	restore := rule.scopeExpressionSource(s, pos)
	defer restore()

	line, col := pos.Line, pos.Col
	if quoted {
		col++ // when the string is quoted like 'foo' or "foo", column should be incremented
	}
	offset := 0
	ts := []typedExpr{}
	valid := true
	for {
		idx := strings.Index(s, "${{")
		if idx == -1 {
			break
		}

		start := idx + 3 // 3 means removing "${{"
		s = s[start:]
		offset += start
		rule.expressionOffset = offset
		col := col + offset

		result, offsetAfter, parsed := rule.checkSemantics(s, line, col, checkUntrusted, workflowKey)
		// Parsed expressions have a complete boundary even when semantic checks fail.
		if !parsed {
			return nil, false
		}
		valid = valid && result.Valid
		if offsetAfter == 0 {
			return nil, true
		}
		ty := result.Type
		refinement := expressionRefinement{state: value.Dynamic}
		if result.Valid {
			refinement = rule.expressionType("${{"+s[:offsetAfter], result.Evaluation)
			if refinement.state == value.Evaluated && (result.Call == nil || result.Call.ReturnDomain != exprfunctions.ReturnDomainExcludesValue) {
				ty = refinement.ty
			}
		} else {
			refinement.state = value.Invalid
		}
		position := workflowlang.Pos{Line: line, Col: col - 3}
		if mapped := rule.expressionPosition(offset-3, false); mapped != nil {
			position = *mapped
		}
		ts = append(ts, typedExpr{ty: ty, semantics: result, pos: position, refinement: refinement})

		s = s[offsetAfter:]
		offset += offsetAfter
	}

	return ts, valid
}

func (rule *Checker) syntaxError(issue *exprsyntax.SyntaxError, lineBase, colBase int) {
	rule.exprError(&exprlang.Error{
		Message: issue.Message, Offset: issue.Offset, EndOffset: issue.EndOffset,
		Line: issue.Line, Column: issue.Column,
		Rule: "expression-syntax", Code: "invalid-syntax",
	}, lineBase, colBase)
}

func (rule *Checker) exprError(err *exprlang.Error, lineBase, colBase int) {
	rule.exprFinding(err, lineBase, colBase, "")
}

func (rule *Checker) exprFinding(err *exprlang.Error, lineBase, colBase int, severity string) {
	if rule.compositeScript && err.Rule != "script-injection" && err.Rule != "script-interpolation" {
		return // Metadata validation owns composite syntax, context and call errors.
	}
	kind := err.Rule
	if kind == "" {
		kind = "expression"
	}
	r := report{rule: kind, code: err.Code, severity: severity, message: err.Message, start: convertExprLineColToPos(err.Line, err.Column, lineBase, colBase)}
	if mapped := rule.expressionPosition(rule.expressionOffset+err.Offset, false); mapped != nil {
		r.start = *mapped
		if end := rule.expressionPosition(rule.expressionOffset+err.EndOffset, true); end != nil {
			r.end, r.hasEnd = *end, true
		}
	}
	if kind == "script-interpolation" || kind == "script-injection" {
		rule.addScriptEnvironmentFix(&r)
	}
	rule.errs = append(rule.errs, r)
}

func (rule *Checker) checkSemanticsOfExprNode(expr exprsyntax.Node, line, col int, checkUntrusted bool, workflowKey string) exprlang.ExpressionCheckResult {
	options := rule.options.Semantics
	if rule.jobReferences != nil {
		options.Variables, options.Secrets = rule.jobReferences.Variables, rule.jobReferences.Secrets
	}
	c := exprlang.NewChecker(contextspec.DefaultExpressionScope(), &options)
	if rule.eventTy != nil {
		contextspec.SetEventPayloadType(c, *rule.eventTy)
	}
	if rule.matrixTy != nil {
		c.SetContextType("matrix", rule.matrixTy.Type())
	}
	if rule.stepsTy != nil {
		c.SetContextType("steps", rule.stepsTy.Type())
	}
	if rule.needsTy != nil {
		c.SetContextType("needs", rule.needsTy.Type())
	}
	if rule.secretsTy != nil {
		secrets := *rule.secretsTy
		if rule.jobReferences != nil && rule.jobReferences.EnvironmentSecrets != nil {
			environment := *rule.jobReferences.EnvironmentSecrets
			for name, property := range environment.Properties() {
				secrets = secrets.WithProperty(environment.PropertyName(name), property)
			}
			if mapped, known := environment.Mapped(); known {
				secrets = secrets.WithMapped(mapped)
			}
		}
		contextspec.SetSecretTypes(c, secrets)
	}
	if rule.inputsTy != nil {
		contextspec.MergeInputTypes(c, *rule.inputsTy)
	}
	if rule.dispatchInputsTy != nil {
		contextspec.SetDispatchInputTypes(c, *rule.dispatchInputsTy)
	}
	if rule.jobsTy != nil {
		c.SetContextType("jobs", rule.jobsTy.Type())
	}
	if workflowKey != "" {
		ctx, _ := contextspec.WorkflowKeyAvailability(workflowKey)
		if len(ctx) == 0 {
			rule.debug("No context availability was found for workflow key %q", workflowKey)
		}
		c.SetAvailability(contextspec.WorkflowExpressionScope(workflowKey))
	}

	if rule.compositeScript {
		c.SetContextAvailability(actionmeta.AllowedContexts("runs.steps.*.run"))
	}
	result := c.CheckResult(expr)
	ty, errs := result.Type, result.Findings
	errs = append(errs, correctness.UnusedFormatArguments(expr, result.Calls)...)
	if rule.options.SuspiciousCoercion {
		errs = append(errs, suspicious.SuspiciousCoercion(expr, result.Types, result.Calls)...)
	}
	if strings.HasSuffix(workflowKey, ".if") {
		if rule.options.CaseInsensitiveConditions {
			errs = append(errs, suspicious.CaseInsensitiveConditions(expr, result.Types, result.Calls)...)
		}
		if rule.options.StringConditions {
			errs = append(errs, suspicious.StringConditions(expr, result.Types)...)
		}
		if rule.options.MixedTypeComparisons {
			errs = append(errs, suspicious.MixedTypeComparisons(expr, result.Types)...)
		}
	}
	if checkUntrusted {
		errs = append(errs, security.ScriptInjection(expr, result.Findings)...)
	}
	for _, finding := range correctness.PermissiveJSONCalls(expr) {
		rule.exprFinding(finding, line, col, "info")
	}
	canCheckInterpolation := result.Valid && (rule.options.ScriptInjectionDisabled || !slices.ContainsFunc(errs, func(err *exprlang.Error) bool { return err.Rule == "script-injection" }))
	// Serialized scalars still require a data-argument position. A closed
	// string such as toJSON(null) must not bypass that check in eval or sh -c.
	closedArgumentList := checkUntrusted && canCheckInterpolation && !correctness.IsScalarInterpolation(expr, result.Types) && rule.scriptWordLists[rule.expressionOffset-3] && rule.safeScriptArgumentList(expr)
	if checkUntrusted && canCheckInterpolation && !closedArgumentList {
		scalarArgument := rule.scriptScalarArgs[rule.expressionOffset-3]
		position := rule.scriptInsertion
		if scalarArgument {
			position = correctness.ShellArgumentInsertion
		}
		findings := correctness.ScriptInterpolation(expr, ty, result.Types, position, func(node exprsyntax.Node) bool {
			path := exprsyntax.StaticReference(node)
			if correctness.IsScalarInterpolation(node, result.Types) {
				return scalarArgument
			}
			if rule.safeScriptStatusSelector(node) {
				return true
			}
			if safeMatrixScriptReference(node, rule.matrixScriptSafe, scalarArgument) {
				return true
			}
			if name, ok := strings.CutPrefix(path, "env."); ok {
				return rule.safeScriptEnv(name, scalarArgument)
			}
			return false
		})
		for _, finding := range findings {
			rule.exprError(finding, line, col)
		}
	}
	for _, err := range errs {
		rule.exprError(err, line, col)
	}
	if rule.options.UnsoundTernary {
		for _, finding := range correctness.UnsoundTernary(expr) {
			rule.exprError(finding, line, col)
		}
	}
	if workflowKey == "" && result.Valid {
		if _, literal := expr.(*exprsyntax.StringNode); !literal {
			position := &workflowlang.Pos{Line: line, Col: col}
			if mapped := rule.expressionPosition(rule.expressionOffset, false); mapped != nil {
				position = mapped
			}
			rule.Error(position, "expressions are not allowed in this static workflow field")
			result.Valid = false
		}
	}

	return result
}

func (rule *Checker) safeScriptStatusSelector(node exprsyntax.Node) bool {
	var context, name string
	switch node := node.(type) {
	case *exprsyntax.ObjectDerefNode:
		variable, ok := node.Receiver.(*exprsyntax.VariableNode)
		if !ok {
			return false
		}
		context, name = variable.Name, node.Property
	case *exprsyntax.IndexAccessNode:
		variable, ok := node.Operand.(*exprsyntax.VariableNode)
		key, keyOK := node.Index.(*exprsyntax.StringNode)
		if !ok || !keyOK {
			return false
		}
		context, name = variable.Name, key.Value
	default:
		return false
	}
	var properties *exprtypes.ObjectType
	switch strings.ToLower(context) {
	case "steps":
		properties = rule.stepsTy
	case "needs":
		properties = rule.needsTy
	default:
		return false
	}
	if properties == nil {
		return false
	}
	_, exists := properties.Property(strings.ToLower(name))
	return exists
}

// checkSemantics parses and checks the expression at the start of src. offset
// is where the expression ends in src. parsed is false after a syntax error.
func (rule *Checker) checkSemantics(src string, line, col int, checkUntrusted bool, workflowKey string) (result exprlang.ExpressionCheckResult, offset int, parsed bool) {
	l := exprsyntax.NewLexer(src)
	p := exprsyntax.NewParser()
	expr, err := p.Parse(l)
	if err != nil {
		rule.syntaxError(err, line, col)
		return exprlang.ExpressionCheckResult{Type: exprtypes.AnyType()}, l.Offset(), false
	}
	return rule.checkSemanticsOfExprNode(expr, line, col, checkUntrusted, workflowKey), l.Offset(), true
}

func convertExprLineColToPos(line, col, lineBase, colBase int) workflowlang.Pos {
	// Line and column in ExprError are 1-based
	return workflowlang.Pos{
		Line: line - 1 + lineBase,
		Col:  col - 1 + colBase,
	}
}
