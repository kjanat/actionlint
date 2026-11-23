package expressions

import (
	"slices"
	"strings"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/evaluate"

	"actionlint.kjanat.dev/v2/internal/checking/rules/correctness"

	"actionlint.kjanat.dev/v2/internal/checking/rules/security"
	"actionlint.kjanat.dev/v2/internal/checking/rules/suspicious"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/functions"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/exprtypes"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/typecheck"

	"actionlint.kjanat.dev/v2/internal/model/actionmeta"

	"actionlint.kjanat.dev/v2/internal/model/contexts"

	"actionlint.kjanat.dev/v2/internal/model/contexts/value"
	"actionlint.kjanat.dev/v2/internal/model/severity"
)

func (rule *Visitor) checkExprsIn(s string, pos *sourcetext.Position, quoted, checkUntrusted bool, workflowKey string) ([]typedExpr, bool) {
	restore := rule.scopeExpressionSource(s, pos)
	defer restore()

	origin := *pos
	if quoted {
		origin.Column++
	}
	offset := 0
	ts := []typedExpr{}
	valid := true
	for {
		idx := strings.Index(s, "${{")
		if idx == -1 {
			break
		}

		start := idx + 3
		s = s[start:]
		offset += start
		rule.expressionOffset = offset
		base := sourcetext.Position{Line: origin.Line, Column: origin.Column + offset}

		result, offsetAfter, parsed := rule.checkSemantics(s, base, checkUntrusted, workflowKey)
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
			if refinement.state == value.Evaluated && (result.Call == nil || result.Call.ReturnDomain != functions.ReturnDomainExcludesValue) {
				ty = refinement.ty
			}
		} else {
			refinement.state = value.Invalid
		}
		position := sourcetext.Position{Line: base.Line, Column: base.Column - 3}
		if mapped := rule.expressionPosition(offset-3, false); mapped != nil {
			position = *mapped
		}
		ts = append(ts, typedExpr{ty: ty, semantics: result, pos: position, refinement: refinement})

		s = s[offsetAfter:]
		offset += offsetAfter
	}

	return ts, valid
}

func (rule *Visitor) syntaxError(issue *syntax.SyntaxError, base sourcetext.Position) {
	rule.exprError(&typecheck.Error{
		Message: issue.Message, Offset: issue.Offset, EndOffset: issue.EndOffset,
		Position: issue.Position,
		Rule:     "expression-syntax", Code: "invalid-syntax",
	}, base)
}

func (rule *Visitor) exprError(err *typecheck.Error, base sourcetext.Position) {
	rule.exprFinding(err, base, "")
}

func (rule *Visitor) exprFinding(err *typecheck.Error, base sourcetext.Position, level severity.Severity) {
	if rule.compositeScript && !compositeFinding(err.Rule, err.Code) {
		return // Metadata validation owns composite syntax, context and call errors.
	}
	kind := err.Rule
	if kind == "" {
		kind = "expression"
	}
	r := report{rule: kind, code: err.Code, severity: level, message: err.Message, start: documentPosition(base, err.Position)}
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

func (rule *Visitor) checkSemanticsOfExprNode(expr syntax.Node, base sourcetext.Position, checkUntrusted bool, workflowKey string) typecheck.ExpressionCheckResult {
	options := rule.options.Semantics
	if rule.jobReferences != nil {
		options.Variables, options.Secrets = rule.jobReferences.Variables, rule.jobReferences.Secrets
	}
	c := typecheck.NewChecker(contexts.DefaultExpressionScope(), &options)
	if rule.eventTy != nil {
		contexts.SetEventPayloadType(c, *rule.eventTy)
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
		contexts.SetSecretTypes(c, secrets)
	}
	if rule.inputsTy != nil {
		contexts.MergeInputTypes(c, *rule.inputsTy)
	}
	if rule.dispatchInputsTy != nil {
		contexts.SetDispatchInputTypes(c, *rule.dispatchInputsTy)
	}
	if rule.jobsTy != nil {
		c.SetContextType("jobs", rule.jobsTy.Type())
	}
	if workflowKey != "" {
		ctx, _ := contexts.WorkflowKeyAvailability(workflowKey)
		if len(ctx) == 0 {
			rule.debug("No context availability was found for workflow key %q", workflowKey)
		}
		c.SetAvailability(contexts.WorkflowExpressionScope(workflowKey))
	}

	if rule.compositeScript {
		available := actionmeta.AllowedContexts("runs.steps.*.run")
		c.SetContextAvailability(available)
		// Metadata validation reports unavailable contexts. Their property types are unknown.
		for name := range contexts.BuiltinGlobalVariableTypes {
			if !slices.Contains(available, name) {
				c.SetContextType(name, exprtypes.AnyType())
			}
		}
	}
	evaluation := evaluate.EvaluateSubexpressions(expr)
	result := c.CheckResult(evaluation)
	ty, errs := result.Type, result.Findings
	errs = append(errs, correctness.UnusedFormatArguments(expr, result.Calls)...)
	if rule.options.SuspiciousCoercion {
		errs = append(errs, suspicious.SuspiciousCoercion(expr, result.Types, result.Calls)...)
	}
	if rule.options.JoinLiteral {
		errs = append(errs, suspicious.JoinLiteral(expr, result.Calls)...)
	}
	if rule.options.CollectionComparison {
		errs = append(errs, suspicious.CollectionComparison(expr, result.Types, evaluation)...)
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
		rule.exprFinding(finding, base, severity.Info)
	}
	canCheckInterpolation := result.Valid && (rule.options.ScriptInjectionDisabled || !slices.ContainsFunc(errs, func(err *typecheck.Error) bool { return err.Rule == "script-injection" }))
	// Serialized scalars still require a data-argument position. A closed
	// string such as toJSON(null) must not bypass that check in eval or sh -c.
	closedArgumentList := checkUntrusted && canCheckInterpolation && !correctness.IsScalarInterpolation(expr, result.Types) && rule.scriptWordLists[rule.expressionOffset-3] && rule.safeScriptArgumentList(expr)
	if checkUntrusted && canCheckInterpolation && !closedArgumentList {
		scalarArgument := rule.scriptScalarArgs[rule.expressionOffset-3]
		position := rule.scriptInsertion
		if scalarArgument {
			position = correctness.ShellArgumentInsertion
		}
		findings := correctness.ScriptInterpolation(expr, ty, result.Types, position, func(node syntax.Node) bool {
			path := syntax.StaticReference(node)
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
			rule.exprError(finding, base)
		}
	}
	for _, err := range errs {
		rule.exprError(err, base)
	}
	if rule.options.UnsoundTernary {
		for _, finding := range correctness.UnsoundTernary(expr) {
			rule.exprError(finding, base)
		}
	}
	if workflowKey == "" && result.Valid {
		if _, literal := expr.(*syntax.StringNode); !literal {
			position := &base
			if mapped := rule.expressionPosition(rule.expressionOffset, false); mapped != nil {
				position = mapped
			}
			rule.Error(position, "expressions are not allowed in this static workflow field")
			result.Valid = false
		}
	}

	return result
}

func (rule *Visitor) safeScriptStatusSelector(node syntax.Node) bool {
	var context, name string
	switch node := node.(type) {
	case *syntax.ObjectDerefNode:
		variable, ok := node.Receiver.(*syntax.VariableNode)
		if !ok {
			return false
		}
		context, name = variable.Name, node.Property
	case *syntax.IndexAccessNode:
		variable, ok := node.Operand.(*syntax.VariableNode)
		key, keyOK := node.Index.(*syntax.StringNode)
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
func (rule *Visitor) checkSemantics(src string, base sourcetext.Position, checkUntrusted bool, workflowKey string) (result typecheck.ExpressionCheckResult, offset int, parsed bool) {
	l := syntax.NewLexer(src)
	p := syntax.NewParser()
	expr, err := p.ParseExpression(l)
	if err != nil {
		rule.syntaxError(err, base)
		return typecheck.ExpressionCheckResult{Type: exprtypes.AnyType()}, l.Offset(), false
	}
	return rule.checkSemanticsOfExprNode(expr, base, checkUntrusted, workflowKey), l.Offset(), true
}

func documentPosition(base, at sourcetext.Position) sourcetext.Position {
	if at.Line > 1 {
		return sourcetext.Position{Line: base.Line + at.Line - 1, Column: at.Column}
	}
	return sourcetext.Position{Line: base.Line, Column: base.Column + at.Column - 1}
}
