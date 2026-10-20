package expressions

import (
	"strconv"
	"strings"

	exprlang "actionlint.kjanat.dev/v2/internal/expression"
	"actionlint.kjanat.dev/v2/internal/mapping"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/internal/workflow/schema"
	"actionlint.kjanat.dev/v2/internal/yamlscalar"
)

func (rule *Checker) checkOneExpression(s *workflowlang.String, what, workflowKey string) exprlang.Type {
	// checkString is not available since it checks types for embedding values into a string
	if s == nil {
		return nil
	}

	ts, ok := rule.checkExprsIn(s.Value, s.Pos, s.Quoted, false, workflowKey)
	if !ok {
		return nil
	}

	if len(ts) != 1 {
		// This case should be unreachable since only one ${{ }} is included is checked by parser
		rule.Errorf(s.Pos, "one ${{ }} expression should be included in %q value but got %d expressions", what, len(ts))
		return nil
	}

	if value, known := workflowlang.WorkflowExpressionLiteral(s); known {
		return exprlang.TypeOfJSONValue(value)
	}
	return ts[0].ty
}

func (rule *Checker) checkObjectTy(ty exprlang.Type, pos *workflowlang.Pos, what string) exprlang.Type {
	if ty == nil {
		return nil
	}
	switch ty.(type) {
	case *exprlang.ObjectType, exprlang.AnyType:
		return ty
	default:
		rule.Errorf(pos, "type of expression at %q must be object but found type %s", what, ty.String())
		return nil
	}
}

func (rule *Checker) checkArrayTy(ty exprlang.Type, pos *workflowlang.Pos, what string) exprlang.Type {
	if ty == nil {
		return nil
	}
	switch ty.(type) {
	case *exprlang.ArrayType, exprlang.AnyType:
		return ty
	default:
		rule.Errorf(pos, "type of expression at %q must be array but found type %s", what, ty.String())
		return nil
	}
}

func (rule *Checker) checkNumberTy(ty exprlang.Type, pos *workflowlang.Pos, what string) exprlang.Type {
	if ty == nil {
		return nil
	}
	switch ty.(type) {
	case exprlang.NumberType, exprlang.AnyType:
		return ty
	default:
		rule.Errorf(pos, "type of expression at %q must be number but found type %s", what, ty.String())
		return nil
	}
}

func (rule *Checker) checkObjectExpression(s *workflowlang.String, what, workflowKey string) exprlang.Type {
	ty := rule.checkOneExpression(s, what, workflowKey)
	if ty == nil {
		return nil
	}
	return rule.checkObjectTy(ty, s.Pos, what)
}

func (rule *Checker) checkArrayExpression(s *workflowlang.String, what, workflowKey string) exprlang.Type {
	ty := rule.checkOneExpression(s, what, workflowKey)
	if ty == nil {
		return nil
	}
	return rule.checkArrayTy(ty, s.Pos, what)
}

func (rule *Checker) checkNumberExpression(s *workflowlang.String, what, workflowKey string) {
	ty := rule.checkOneExpression(s, what, workflowKey)
	if ty == nil {
		return
	}
	rule.checkNumberTy(ty, s.Pos, what)
}

func (rule *Checker) checkActionInputs(inputs *mapping.Inputs[*workflowlang.String], uses *workflowlang.String) {
	const key = "jobs.<job_id>.steps.with"
	var action string
	if uses != nil {
		action = uses.Value
	}
	workflowlang.InputResolver.Check(inputs, action, mapping.Checks[*workflowlang.String]{
		Mapping: func(source *workflowlang.String) {
			rule.checkWorkflowExpression(source, "with", key, schema.StringMap)
		},
		String: func(source *workflowlang.String) { rule.checkString(source, key) },
		Script: func(source *workflowlang.String) { rule.checkScriptString(source, key) },
	})
}

func (rule *Checker) checkEnv(env *workflowlang.Env, workflowKey string) {
	if env == nil {
		return
	}

	workflowlang.InputResolver.Check(workflowlang.EnvMappingView(env), "", mapping.Checks[*workflowlang.String]{
		Mapping: func(source *workflowlang.String) {
			rule.checkWorkflowExpression(source, "env", workflowKey, schema.StringMap)
		},
		String: func(source *workflowlang.String) { rule.checkString(source, workflowKey) },
	})
}

func (rule *Checker) checkContainer(c *workflowlang.Container, workflowKey, childWorkflowKeyPrefix string) {
	if c == nil {
		return
	}
	rule.checkWorkflowExpression(c.Expression, "container", workflowKey, schema.Container(childWorkflowKeyPrefix != ""))
	childWorkflowKey := workflowKey
	if childWorkflowKeyPrefix != "" {
		childWorkflowKey += "." + childWorkflowKeyPrefix
	}
	rule.checkString(c.Image, workflowKey)
	if c.Credentials != nil {
		k := childWorkflowKey + ".credentials" // e.g. jobs.<job_id>.container.credentials
		if c.Credentials.Expression != nil {
			rule.checkWorkflowExpression(c.Credentials.Expression, "credentials", k, schema.Credentials)
		} else {
			rule.checkString(c.Credentials.Username, k)
			rule.checkString(c.Credentials.Password, k)
		}
	}
	rule.checkEnv(c.Env, childWorkflowKey+".env.<env_id>") // e.g. jobs.<job_id>.container.env.<env_id>
	rule.checkStrings(c.Ports, workflowKey)
	rule.checkStrings(c.Volumes, workflowKey)
	rule.checkWorkflowExpression(c.PortsExpression, "ports", workflowKey, schema.StringList)
	rule.checkWorkflowExpression(c.VolumesExpression, "volumes", workflowKey, schema.StringList)
	rule.checkString(c.Options, workflowKey)
	rule.checkString(c.Command, workflowKey)
	rule.checkString(c.Entrypoint, workflowKey)
}

func (rule *Checker) checkConcurrency(c *workflowlang.Concurrency, workflowKey string) {
	if c == nil {
		return
	}
	shape := schema.Concurrency
	if workflowKey == "jobs.<job_id>.concurrency" {
		shape = schema.JobConcurrency
	}
	rule.checkWorkflowExpression(c.Expression, "concurrency", workflowKey, shape)
	if c.Group != nil && c.Group.IsExpressionAssigned() {
		rule.checkWorkflowExpression(c.Group, "concurrency.group", workflowKey, schema.NonEmpty)
	} else {
		rule.checkString(c.Group, workflowKey)
	}
	if c.Queue != nil && c.Queue.IsExpressionAssigned() {
		rule.checkWorkflowExpression(c.Queue, "concurrency.queue", workflowKey, schema.Queue)
	} else {
		rule.checkString(c.Queue, workflowKey)
	}
	rule.checkBool(c.CancelInProgress, workflowKey)
	if c.Queue == nil || c.CancelInProgress == nil {
		return
	}
	// Literal YAML conflicts are diagnosed by the parser. Resolve known
	// expression values here, retaining unknown values for runtime validation.
	if !c.Queue.ContainsExpression() && c.CancelInProgress.Expression == nil {
		return
	}
	var queue any = c.Queue.Value
	if c.Queue.ContainsExpression() {
		queue, _ = workflowlang.WorkflowExpressionLiteral(c.Queue)
	}
	var cancel any = c.CancelInProgress.Value
	if c.CancelInProgress.Expression != nil {
		cancel, _ = workflowlang.WorkflowExpressionLiteral(c.CancelInProgress.Expression)
	}
	if queue == "max" && cancel == true {
		rule.Error(c.Queue.Pos, `"queue: max" cannot be combined with "cancel-in-progress: true" in "concurrency" section`)
	}
}

func (rule *Checker) checkDefaults(d *workflowlang.Defaults, workflowKey string) {
	if d == nil || d.Run == nil {
		return
	}
	rule.checkWorkflowExpression(d.Run.Expression, "defaults.run", workflowKey, schema.DefaultsRun)
	rule.checkString(d.Run.Shell, workflowKey)
	rule.checkString(d.Run.WorkingDirectory, workflowKey)
}

func (rule *Checker) checkWorkflowCall(c *workflowlang.WorkflowCall) {
	if c == nil || c.Uses == nil {
		return
	}

	rule.checkString(c.Uses, "")

	m, err := rule.workflowMetadata(c.Uses.Value)
	if err != nil {
		// The workflow-call rule reports metadata failures at the uses value.
		rule.debug("Could not infer workflow input types: %v", err)
	}

	for n, i := range c.Inputs {
		ts := rule.checkString(i.Value, "jobs.<job_id>.with.<with_id>")

		if m == nil {
			continue
		}

		mi, ok := m.Inputs[n]
		if !ok || mi == nil {
			continue
		}
		if _, ok := mi.Type.(exprlang.AnyType); ok {
			continue
		}

		v := strings.TrimSpace(i.Value.Value)

		var ty exprlang.Type = exprlang.StringType{}
		switch len(ts) {
		case 0:
			switch v {
			case "null":
				ty = exprlang.NullType{}
			case "true", "false":
				ty = exprlang.BoolType{}
			default:
				if _, err := strconv.ParseFloat(v, 64); err == nil {
					ty = exprlang.NumberType{}
				}
			}
		case 1:
			if i.Value.IsExpressionAssigned() {
				ty = ts[0].ty
			}
		}

		if !mi.Type.Assignable(ty) {
			rule.Errorf(
				i.Value.Pos,
				"input %q is typed as %s by reusable workflow %q. %s value cannot be assigned",
				mi.Name,
				mi.Type.String(),
				c.Uses.Value,
				ty.String(),
			)
		}
	}

	for _, s := range c.Secrets {
		rule.checkString(s.Value, "jobs.<job_id>.secrets.<secrets_id>")
	}
}

func (rule *Checker) checkSnapshot(s *workflowlang.Snapshot) {
	if s == nil {
		return
	}
	rule.checkWorkflowExpression(s.Expression, "snapshot", "jobs.<job_id>.snapshot", schema.Snapshot)
	for _, field := range []struct {
		value *workflowlang.String
		name  string
	}{{s.ImageName, "image-name"}, {s.Version, "version"}} {
		if field.value != nil && field.value.IsExpressionAssigned() {
			rule.checkWorkflowExpression(field.value, "snapshot."+field.name, "jobs.<job_id>.snapshot", schema.NonEmpty)
		} else {
			rule.checkString(field.value, "jobs.<job_id>.snapshot")
		}
	}
	rule.checkIfCondition(s.If, "jobs.<job_id>.snapshot.if")
}

func (rule *Checker) checkWebhookEventFilter(f *workflowlang.WebhookEventFilter) {
	if f == nil {
		return
	}
	rule.checkStrings(f.Values, "")
}

func (rule *Checker) checkStrings(ss []*workflowlang.String, workflowKey string) {
	for _, s := range ss {
		rule.checkString(s, workflowKey)
	}
}

func (rule *Checker) checkIfCondition(str *workflowlang.String, workflowKey string) {
	if str == nil {
		return
	}

	// Note:
	// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#jobsjob_idif
	//
	// > When you use expressions in an if conditional, you may omit the expression syntax (${{ }})
	// > because GitHub automatically evaluates the if conditional as an expression, unless the
	// > expression contains any operators. If the expression contains any operators, the expression
	// > must be contained within ${{ }} to explicitly mark it for evaluation.
	//
	// This document is actually wrong. I confirmed that any strings without surrounding in ${{ }}
	// are evaluated.
	//
	// - run: echo 'run'
	//   if: '!false'
	// - run: echo 'not run'
	//   if: '!true'
	// - run: echo 'run'
	//   if: false || true
	// - run: echo 'run'
	//   if: true && true
	// - run: echo 'not run'
	//   if: true && false

	var condTy exprlang.Type
	restore := rule.scopeExpressionSource(str.Value, str.Pos)
	defer restore()
	expr, err, offset := exprlang.ParseConditionExpression(str.Value)
	if exprlang.ConditionSource(str.Value) != str.Value && offset == 0 {
		rule.expressionSource = nil
	}
	rule.expressionOffset = offset
	line, col := str.Pos.Line, str.Pos.Col+offset
	if offset != 0 && str.Quoted {
		col++
	}
	if err != nil {
		rule.exprError(err, line, col)
		return
	}
	if expr == nil {
		rule.checkString(str, workflowKey)
		return
	}
	if ty, ok := rule.checkSemanticsOfExprNode(expr, line, col, false, workflowKey); ok {
		condTy = ty
	}

	if condTy != nil && !(exprlang.BoolType{}).Assignable(condTy) {
		rule.Errorf(str.Pos, "\"if\" condition should be type \"bool\" but got type %q", condTy.String())
	}
}

func (rule *Checker) checkTemplateEvaluatedType(ts []typedExpr) {
	for _, t := range ts {
		switch t.ty.(type) {
		case *exprlang.ObjectType, *exprlang.ArrayType, exprlang.NullType:
			rule.Errorf(&t.pos, "object, array, and null values should not be evaluated in template with ${{ }} but evaluating the value of type %s", t.ty)
		}
	}
}

func (rule *Checker) checkString(str *workflowlang.String, workflowKey string) []typedExpr {
	if str == nil {
		return nil
	}

	ts, ok := rule.checkExprsIn(str.Value, str.Pos, str.Quoted, false, workflowKey)
	if !ok {
		return nil
	}
	// Contextless schema definitions reject expressions. The runner folds a sole
	// string literal expression into a string, but interpolation remains an expression.
	if workflowKey == "" && len(ts) > 0 && (len(ts) != 1 || !str.IsExpressionAssigned()) {
		rule.Error(str.Pos, "expressions are not allowed in this static workflow field")
		return nil
	}

	rule.checkTemplateEvaluatedType(ts)
	return ts
}

func (rule *Checker) checkScriptString(str *workflowlang.String, workflowKey string) {
	if str == nil {
		return
	}

	ts, ok := rule.checkExprsIn(str.Value, str.Pos, str.Quoted, true, workflowKey)
	if !ok {
		return
	}

	rule.checkTemplateEvaluatedType(ts)
}

func (rule *Checker) checkBool(b *workflowlang.Bool, workflowKey string) {
	if b == nil || b.Expression == nil {
		return
	}

	ty := rule.checkOneExpression(b.Expression, "bool value", workflowKey)
	if ty == nil {
		return
	}

	switch ty.(type) {
	case exprlang.BoolType, exprlang.AnyType:
		// ok
	default:
		rule.Errorf(b.Expression.Pos, "type of expression must be bool but found type %s", ty.String())
	}
}

func (rule *Checker) checkMaxParallel(i *workflowlang.Int, workflowKey string) {
	if i == nil || i.Expression == nil {
		return
	}
	ty := rule.checkOneExpression(i.Expression, "integer value", workflowKey)
	if rule.checkNumberTy(ty, i.Expression.Pos, "integer value") == nil {
		return
	}
	if value, known := workflowlang.WorkflowExpressionLiteral(i.Expression); known {
		for _, message := range schema.LiteralErrors(schema.Positive, value, "max-parallel") {
			rule.Error(i.Expression.Pos, message)
		}
	}
}

func (rule *Checker) checkFloat(f *workflowlang.Float, workflowKey string) {
	if f == nil {
		return
	}
	rule.checkNumberExpression(f.Expression, "float number value", workflowKey)
}

func (rule *Checker) checkRawYAMLValue(v workflowlang.RawYAMLValue) exprlang.Type {
	switch v := v.(type) {
	case *workflowlang.RawYAMLObject:
		m := make(map[string]exprlang.Type, len(v.Props))
		for k, p := range v.Props {
			m[k] = rule.checkRawYAMLValue(p)
		}
		return exprlang.NewStrictObjectType(m)
	case *workflowlang.RawYAMLArray:
		var elem exprlang.Type
		for _, value := range v.Elems {
			if ty := rule.checkMatrixSequenceItem(value); ty != nil {
				if elem == nil {
					elem = ty
				} else {
					elem = elem.Merge(ty)
				}
			}
		}
		if elem == nil {
			elem = exprlang.AnyType{}
		}
		return &exprlang.ArrayType{Elem: elem, Deref: false}
	case *workflowlang.RawYAMLString:
		return rule.checkRawYAMLString(v)
	default:
		panic("unreachable")
	}
}

func (rule *Checker) checkRawYAMLString(y *workflowlang.RawYAMLString) exprlang.Type {
	ts, ok := rule.checkExprsIn(y.Value, y.Pos(), false, false, "jobs.<job_id>.strategy")

	if workflowlang.IsExprAssigned(y.Value) {
		if !ok || len(ts) != 1 {
			return exprlang.AnyType{}
		}
		if value, known := workflowlang.WorkflowExpressionLiteral(&workflowlang.String{Value: y.Value}); known {
			return exprlang.TypeOfJSONValue(value)
		}
		return ts[0].ty
	}

	switch y.Tag {
	case workflowlang.YamlTagBool:
		if yamlscalar.IsCoreSchemaBool(y.Value) {
			return exprlang.BoolType{}
		}
	case workflowlang.YamlTagNull:
		if yamlscalar.IsCoreSchemaNull(y.Value) {
			return exprlang.NullType{}
		}
	case workflowlang.YamlTagInt, workflowlang.YamlTagFloat:
		if yamlscalar.IsCoreSchemaNumber(y.Value) {
			return exprlang.NumberType{}
		}
	}
	return exprlang.StringType{}
}

func (rule *Checker) checkWorkflowExpression(s *workflowlang.String, what, key string, shape schema.Shape) exprlang.Type {
	if s == nil {
		return nil
	}
	ty := rule.checkOneExpression(s, what, key)
	if ty == nil {
		return nil
	}
	errors := shape.ValidateType(ty, what)
	// Inferred array elements can merge to AnyType. Retain known JSON values for
	// per-element and literal constraint checks.
	if len(errors) == 0 {
		if value, known := workflowlang.WorkflowExpressionLiteral(s); known {
			errors = schema.LiteralErrors(shape, value, what)
			if len(errors) == 0 {
				ty = exprlang.TypeOfJSONValue(value)
			}
		}
	}
	for _, message := range errors {
		rule.Error(s.Pos, message)
	}
	return ty
}
