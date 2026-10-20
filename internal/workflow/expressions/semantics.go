package expressions

import (
	"strings"

	exprlang "actionlint.kjanat.dev/v2/internal/expression"
	contextspec "actionlint.kjanat.dev/v2/internal/expression/context"
	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
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

		ty, offsetAfter, ok := rule.checkSemantics(s, line, col, checkUntrusted, workflowKey)
		// Parsed expressions have a complete boundary even when semantic checks fail.
		// Scan later interpolations for independent rules, but retain aggregate invalidity.
		if !ok && ty == nil {
			return nil, false
		}
		valid = valid && ok
		if ty == nil || offsetAfter == 0 {
			return nil, true
		}
		position := workflowlang.Pos{Line: line, Col: col - 3}
		if mapped := rule.expressionPosition(offset-3, false); mapped != nil {
			position = *mapped
		}
		ts = append(ts, typedExpr{ty, position})

		s = s[offsetAfter:]
		offset += offsetAfter
	}

	return ts, valid
}

func (rule *Checker) exprError(err *exprlang.Error, lineBase, colBase int) {
	pos := convertExprLineColToPos(err.Line, err.Column, lineBase, colBase)
	var end *workflowlang.Pos
	if mapped := rule.expressionPosition(rule.expressionOffset+err.Offset, false); mapped != nil {
		pos = mapped
		end = rule.expressionPosition(rule.expressionOffset+err.EndOffset, true)
	}
	kind := err.Rule
	if kind == "" {
		kind = "expression"
	}
	diagnostic := findingmodel.At(pos, kind, err.Message)
	diagnostic.Code = err.Code
	diagnostic.EndPosition = end
	rule.errs = append(rule.errs, diagnostic)
}

func (rule *Checker) checkSemanticsOfExprNode(expr exprlang.Node, line, col int, checkUntrusted bool, workflowKey string) (exprlang.Type, bool) {
	c := exprlang.NewChecker(checkUntrusted, &rule.options.Semantics)
	if rule.eventTy != nil {
		c.UpdateEvent(rule.eventTy)
	}
	if rule.matrixTy != nil {
		c.UpdateMatrix(rule.matrixTy)
	}
	if rule.stepsTy != nil {
		c.UpdateSteps(rule.stepsTy)
	}
	if rule.needsTy != nil {
		c.UpdateNeeds(rule.needsTy)
	}
	if rule.secretsTy != nil {
		c.UpdateSecrets(rule.secretsTy)
	}
	if rule.inputsTy != nil {
		c.UpdateInputs(rule.inputsTy)
	}
	if rule.dispatchInputsTy != nil {
		c.UpdateDispatchInputs(rule.dispatchInputsTy)
	}
	if rule.jobsTy != nil {
		c.UpdateJobs(rule.jobsTy)
	}
	if workflowKey != "" {
		ctx, _ := contextspec.WorkflowKeyAvailability(workflowKey)
		if len(ctx) == 0 {
			rule.debug("No context availability was found for workflow key %q", workflowKey)
		}
		c.SetWorkflowKeyAvailability(workflowKey)
	}

	ty, errs := c.Check(expr)
	if checkUntrusted && len(errs) == 0 {
		findings := exprlang.CheckScriptInterpolation(expr, ty, func(node exprlang.Node) bool {
			path := exprlang.StaticReference(node)
			if c.IsScriptSafeScalar(node) || safeMatrixScriptReference(node, rule.matrixScriptSafe) {
				return true
			}
			if name, ok := strings.CutPrefix(path, "env."); ok && rule.scriptEnv != nil {
				value, known := workflowlang.InputResolver.Value(workflowlang.EnvMappingView(rule.scriptEnv), name)
				return known && exprlang.SafeScriptLiteral(value)
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
		for _, finding := range exprlang.CheckUnsoundTernaries(expr) {
			rule.exprError(finding, line, col)
		}
	}
	if workflowKey == "" && len(errs) == 0 {
		if _, literal := expr.(*exprlang.StringNode); !literal {
			position := &workflowlang.Pos{Line: line, Col: col}
			if mapped := rule.expressionPosition(rule.expressionOffset, false); mapped != nil {
				position = mapped
			}
			rule.Error(position, "expressions are not allowed in this static workflow field")
			return ty, false
		}
	}

	return ty, len(errs) == 0
}

func (rule *Checker) checkSemantics(src string, line, col int, checkUntrusted bool, workflowKey string) (exprlang.Type, int, bool) {
	l := exprlang.NewLexer(src)
	p := exprlang.NewParser()
	expr, err := p.Parse(l)
	if err != nil {
		rule.exprError(err, line, col)
		return nil, l.Offset(), false
	}
	t, ok := rule.checkSemanticsOfExprNode(expr, line, col, checkUntrusted, workflowKey)
	return t, l.Offset(), ok
}

func convertExprLineColToPos(line, col, lineBase, colBase int) *workflowlang.Pos {
	// Line and column in ExprError are 1-based
	return &workflowlang.Pos{
		Line: line - 1 + lineBase,
		Col:  col - 1 + colBase,
	}
}
