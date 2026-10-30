package expressions

import (
	"iter"
	"slices"
	"strconv"
	"strings"

	exprlang "actionlint.kjanat.dev/v2/internal/expression"
	"actionlint.kjanat.dev/v2/internal/mapping"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/internal/workflow/calls"
	workflowdecl "actionlint.kjanat.dev/v2/internal/workflow/declaration"
	"actionlint.kjanat.dev/v2/internal/workflow/schema"
	"actionlint.kjanat.dev/v2/internal/workflow/value"
	yamlSchema "actionlint.kjanat.dev/v2/internal/yaml/schema"
)

func (rule *Checker) checkOneExpression(s workflowdecl.StringView, what, workflowKey string) exprlang.Type {
	// checkString is not available since it checks types for embedding values into a string
	if !s.Present() {
		return nil
	}

	ts, ok := rule.checkExprsIn(s.Value(), viewPosition(s), s.Quoted(), false, workflowKey)
	if !ok {
		return nil
	}

	if len(ts) != 1 {
		// This case should be unreachable since only one ${{ }} is included is checked by parser
		rule.Errorf(viewPosition(s), "one ${{ }} expression should be included in %q value but got %d expressions", what, len(ts))
		return nil
	}

	if value, known := expressionLiteral(s.Value()); known {
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

func (rule *Checker) checkObjectExpression(s workflowdecl.StringView, what, workflowKey string) exprlang.Type {
	ty := rule.checkOneExpression(s, what, workflowKey)
	if ty == nil {
		return nil
	}
	return rule.checkObjectTy(ty, viewPosition(s), what)
}

func (rule *Checker) checkArrayExpression(s workflowdecl.StringView, what, workflowKey string) exprlang.Type {
	ty := rule.checkOneExpression(s, what, workflowKey)
	if ty == nil {
		return nil
	}
	return rule.checkArrayTy(ty, viewPosition(s), what)
}

func (rule *Checker) checkNumberExpression(s workflowdecl.StringView, what, workflowKey string) {
	ty := rule.checkOneExpression(s, what, workflowKey)
	if ty == nil {
		return
	}
	rule.checkNumberTy(ty, viewPosition(s), what)
}

func (rule *Checker) checkActionInputs(action workflowdecl.ExecActionView, uses workflowdecl.StringView) {
	const key = "jobs.<job_id>.steps.with"
	if !action.Present() {
		return
	}
	actionSpec := uses.Value()
	type entry struct {
		name  string
		input workflowdecl.InputView
	}
	var checkMapping func(iter.Seq2[string, workflowdecl.InputView], workflowdecl.StringView, iter.Seq[workflowdecl.ActionInputMappingView])
	checkMapping = func(inputs iter.Seq2[string, workflowdecl.InputView], expression workflowdecl.StringView, insertions iter.Seq[workflowdecl.ActionInputMappingView]) {
		rule.checkWorkflowExpression(expression, "with", key, schema.StringMap)
		var entries []entry
		for name, input := range inputs {
			entries = append(entries, entry{name, input})
		}
		slices.SortFunc(entries, func(a, b entry) int { return strings.Compare(a.name, b.name) })
		for _, current := range entries {
			input := current.input
			if !input.Present() {
				continue
			}
			rule.checkString(input.Name(), key)
			name := input.Name().Semantic()
			if !input.Name().Present() {
				name = value.YAMLScalar(current.name, workflowlang.YamlTagStr, value.Origin{})
			}
			canonical, state := mapping.ResolveKey(name)
			isScriptAction := strings.HasPrefix(actionSpec, "actions/github-script@")
			if isScriptAction && (state == mapping.UnknownKey || state == mapping.KnownKey && strings.EqualFold(canonical, "script")) {
				rule.checkScriptString(input.Value(), key)
			} else {
				rule.checkString(input.Value(), key)
			}
		}
		for insertion := range insertions {
			checkMapping(insertion.Inputs(), insertion.Expression(), insertion.Insertions())
		}
	}
	checkMapping(action.Inputs(), action.InputsExpression(), action.InputInsertions())
}

func (rule *Checker) checkEnv(env workflowdecl.EnvView, workflowKey string) {
	if !env.Present() {
		return
	}

	var checkValues func(workflowdecl.EnvView)
	checkValues = func(current workflowdecl.EnvView) {
		if !current.Present() {
			return
		}
		if current.Expression().Present() {
			rule.checkWorkflowExpression(current.Expression(), "env", workflowKey, schema.StringMap)
		}
		for _, variable := range current.Vars() {
			if variable.Present() {
				rule.checkString(variable.Name(), workflowKey)
				rule.checkString(variable.Value(), workflowKey)
			}
		}
		for insertion := range current.Insertions() {
			checkValues(insertion)
		}
	}
	checkValues(env)
}

func (rule *Checker) checkContainer(c workflowdecl.ContainerView, workflowKey, childWorkflowKeyPrefix string) {
	if !c.Present() {
		return
	}
	rule.checkWorkflowExpression(c.Expression(), "container", workflowKey, schema.Container(childWorkflowKeyPrefix != ""))
	childWorkflowKey := workflowKey
	if childWorkflowKeyPrefix != "" {
		childWorkflowKey += "." + childWorkflowKeyPrefix
	}
	rule.checkString(c.Image(), workflowKey)
	if c.Credentials().Present() {
		k := childWorkflowKey + ".credentials" // e.g. jobs.<job_id>.container.credentials
		if c.Credentials().Expression().Present() {
			rule.checkWorkflowExpression(c.Credentials().Expression(), "credentials", k, schema.Credentials)
		} else {
			rule.checkString(c.Credentials().Username(), k)
			rule.checkString(c.Credentials().Password(), k)
		}
	}
	rule.checkEnv(c.Env(), childWorkflowKey+".env.<env_id>") // e.g. jobs.<job_id>.container.env.<env_id>
	rule.checkStrings(c.Ports(), workflowKey)
	rule.checkStrings(c.Volumes(), workflowKey)
	rule.checkWorkflowExpression(c.PortsExpression(), "ports", workflowKey, schema.StringList)
	rule.checkWorkflowExpression(c.VolumesExpression(), "volumes", workflowKey, schema.StringList)
	rule.checkString(c.Options(), workflowKey)
	rule.checkString(c.Command(), workflowKey)
	rule.checkString(c.Entrypoint(), workflowKey)
}

func (rule *Checker) checkConcurrency(c workflowdecl.ConcurrencyView, workflowKey string) {
	if !c.Present() {
		return
	}
	shape := schema.Concurrency
	if workflowKey == "jobs.<job_id>.concurrency" {
		shape = schema.JobConcurrency
	}
	rule.checkWorkflowExpression(c.Expression(), "concurrency", workflowKey, shape)
	if c.Group().Present() && c.Group().IsExpressionAssigned() {
		rule.checkWorkflowExpression(c.Group(), "concurrency.group", workflowKey, schema.NonEmpty)
	} else {
		rule.checkString(c.Group(), workflowKey)
	}
	if c.Queue().Present() && c.Queue().IsExpressionAssigned() {
		rule.checkWorkflowExpression(c.Queue(), "concurrency.queue", workflowKey, schema.Queue)
	} else {
		rule.checkString(c.Queue(), workflowKey)
	}
	rule.checkBool(c.CancelInProgress(), workflowKey)
	if !c.Queue().Present() || !c.CancelInProgress().Present() {
		return
	}
	// The parser diagnoses literal YAML conflicts; check evaluated expressions here.
	if !c.Queue().ContainsExpression() && !c.CancelInProgress().Expression().Present() {
		return
	}
	var queue any = c.Queue().Value()
	if c.Queue().ContainsExpression() {
		queue, _ = expressionLiteral(c.Queue().Value())
	}
	var cancel any = c.CancelInProgress().Value()
	if c.CancelInProgress().Expression().Present() {
		cancel, _ = expressionLiteral(c.CancelInProgress().Expression().Value())
	}
	if queue == "max" && cancel == true {
		rule.Error(viewPosition(c.Queue()), `"queue: max" cannot be combined with "cancel-in-progress: true" in "concurrency" section`)
	}
}

func (rule *Checker) checkDefaults(d workflowdecl.DefaultsView, workflowKey string) {
	if !d.Present() || !d.Run().Present() {
		return
	}
	rule.checkWorkflowExpression(d.Run().Expression(), "defaults.run", workflowKey, schema.DefaultsRun)
	rule.checkString(d.Run().Shell(), workflowKey)
	rule.checkString(d.Run().WorkingDirectory(), workflowKey)
}

func (rule *Checker) checkWorkflowCall(c workflowdecl.WorkflowCallView) {
	if !c.Present() || !c.Uses().Present() {
		return
	}

	rule.checkString(c.Uses(), "")

	m, err := rule.workflowMetadata(c.Uses().Value())
	if err != nil {
		// The workflow-call rule reports metadata failures at the uses value.
		rule.debug("Could not infer workflow input types: %v", err)
	}

	for n, i := range c.Inputs() {
		ts := rule.checkString(i.Value(), "jobs.<job_id>.with.<with_id>")

		if m == nil {
			continue
		}

		mi, ok := m.Inputs[n]
		if !ok || mi.Type == calls.InputTypeAny {
			continue
		}

		v := strings.TrimSpace(i.Value().Value())

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
			if i.Value().IsExpressionAssigned() {
				ty = ts[0].ty
			}
		}

		if !mi.Type.ExprType().Assignable(ty) {
			rule.Errorf(
				viewPosition(i.Value()),
				"input %q is typed as %s by reusable workflow %q. %s value cannot be assigned",
				mi.Name,
				mi.Type.String(),
				c.Uses().Value(),
				ty.String(),
			)
		}
	}

	for _, s := range c.Secrets() {
		rule.checkString(s.Value(), "jobs.<job_id>.secrets.<secrets_id>")
	}
}

func (rule *Checker) checkSnapshot(s workflowdecl.SnapshotView) {
	if !s.Present() {
		return
	}
	rule.checkWorkflowExpression(s.Expression(), "snapshot", "jobs.<job_id>.snapshot", schema.Snapshot)
	for _, field := range []struct {
		value workflowdecl.StringView
		name  string
	}{{s.ImageName(), "image-name"}, {s.Version(), "version"}} {
		if field.value.Present() && field.value.IsExpressionAssigned() {
			rule.checkWorkflowExpression(field.value, "snapshot."+field.name, "jobs.<job_id>.snapshot", schema.NonEmpty)
		} else {
			rule.checkString(field.value, "jobs.<job_id>.snapshot")
		}
	}
	rule.checkIfCondition(s.If(), "jobs.<job_id>.snapshot.if")
}

func (rule *Checker) checkWebhookEventFilter(f workflowdecl.WebhookEventFilterView) {
	if !f.Present() {
		return
	}
	rule.checkStrings(f.Values(), "")
}

func (rule *Checker) checkStrings(ss iter.Seq[workflowdecl.StringView], workflowKey string) {
	for s := range ss {
		rule.checkString(s, workflowKey)
	}
}

func (rule *Checker) checkIfCondition(str workflowdecl.StringView, workflowKey string) {
	if !str.Present() {
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
	restore := rule.scopeExpressionSource(str.Value(), viewPosition(str))
	defer restore()
	expr, err, offset := exprlang.ParseConditionExpression(str.Value())
	if exprlang.ConditionSource(str.Value()) != str.Value() && offset == 0 {
		rule.expressionSource = workflowdecl.ScriptSourceView{}
	}
	rule.expressionOffset = offset
	line, col := viewPosition(str).Line, viewPosition(str).Col+offset
	if offset != 0 && str.Quoted() {
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
		rule.Errorf(viewPosition(str), "\"if\" condition should be type \"bool\" but got type %q", condTy.String())
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

func (rule *Checker) checkString(str workflowdecl.StringView, workflowKey string) []typedExpr {
	if !str.Present() {
		return nil
	}

	ts, ok := rule.checkExprsIn(str.Value(), viewPosition(str), str.Quoted(), false, workflowKey)
	if !ok {
		return nil
	}
	// Static fields accept a single string literal expression after template folding.
	if workflowKey == "" && len(ts) > 0 && (len(ts) != 1 || !str.IsExpressionAssigned()) {
		rule.Error(viewPosition(str), "expressions are not allowed in this static workflow field")
		return nil
	}

	rule.checkTemplateEvaluatedType(ts)
	return ts
}

func (rule *Checker) checkScriptString(str workflowdecl.StringView, workflowKey string, shell ...string) {
	if !str.Present() {
		return
	}

	previous, previousPosition := rule.scriptScalarArgs, rule.scriptPosition
	rule.scriptScalarArgs = nil
	rule.scriptPosition = exprlang.ScriptUnknownPosition
	if len(shell) > 0 {
		rule.scriptScalarArgs = scalarScriptArguments(str.Value(), shell[0])
		if _, ok := scriptShellVariant(shell[0]); ok {
			rule.scriptPosition = exprlang.ScriptShellWord
		}
	}
	defer func() { rule.scriptScalarArgs, rule.scriptPosition = previous, previousPosition }()
	ts, ok := rule.checkExprsIn(str.Value(), viewPosition(str), str.Quoted(), true, workflowKey)
	if !ok {
		return
	}

	rule.checkTemplateEvaluatedType(ts)
}

func (rule *Checker) checkBool(b workflowdecl.BoolView, workflowKey string) {
	if !b.Present() || !b.Expression().Present() {
		return
	}

	ty := rule.checkOneExpression(b.Expression(), "bool value", workflowKey)
	if ty == nil {
		return
	}

	switch ty.(type) {
	case exprlang.BoolType, exprlang.AnyType:
		// ok
	default:
		rule.Errorf(viewPosition(b.Expression()), "type of expression must be bool but found type %s", ty.String())
	}
}

func (rule *Checker) checkMaxParallel(i workflowdecl.IntView, workflowKey string) {
	if !i.Present() || !i.Expression().Present() {
		return
	}
	ty := rule.checkOneExpression(i.Expression(), "integer value", workflowKey)
	if rule.checkNumberTy(ty, viewPosition(i.Expression()), "integer value") == nil {
		return
	}
	if value, known := expressionLiteral(i.Expression().Value()); known {
		for _, message := range schema.LiteralErrors(schema.Positive, value, "max-parallel") {
			rule.Error(viewPosition(i.Expression()), message)
		}
	}
}

func (rule *Checker) checkFloat(f workflowdecl.FloatView, workflowKey string) {
	if !f.Present() {
		return
	}
	rule.checkNumberExpression(f.Expression(), "float number value", workflowKey)
}

func (rule *Checker) checkRawYAMLValue(v workflowdecl.RawYAMLValueView) exprlang.Type {
	if object, ok := v.RawYAMLObject(); ok {
		properties := map[string]exprlang.Type{}
		for key, property := range object.Props() {
			properties[key] = rule.checkRawYAMLValue(property)
		}
		return exprlang.NewStrictObjectType(properties)
	}
	if array, ok := v.RawYAMLArray(); ok {
		var element exprlang.Type
		for item := range array.Elems() {
			if ty := rule.checkMatrixSequenceItem(item); ty != nil {
				if element == nil {
					element = ty
				} else {
					element = element.Merge(ty)
				}
			}
		}
		if element == nil {
			element = exprlang.AnyType{}
		}
		return &exprlang.ArrayType{Elem: element, Deref: false}
	}
	if scalar, ok := v.RawYAMLString(); ok {
		return rule.checkRawYAMLString(scalar)
	}
	panic("unreachable")
}

func (rule *Checker) checkRawYAMLString(y workflowdecl.RawYAMLStringView) exprlang.Type {
	ts, ok := rule.checkExprsIn(y.Value(), rawPosition(y), false, false, "jobs.<job_id>.strategy")

	if workflowlang.IsExprAssigned(y.Value()) {
		if !ok || len(ts) != 1 {
			return exprlang.AnyType{}
		}
		if value, known := expressionLiteral(y.Value()); known {
			return exprlang.TypeOfJSONValue(value)
		}
		return ts[0].ty
	}

	switch y.Tag() {
	case workflowlang.YamlTagBool:
		if yamlSchema.IsCoreSchemaBool(y.Value()) {
			return exprlang.BoolType{}
		}
	case workflowlang.YamlTagNull:
		if yamlSchema.IsCoreSchemaNull(y.Value()) {
			return exprlang.NullType{}
		}
	case workflowlang.YamlTagInt, workflowlang.YamlTagFloat:
		if yamlSchema.IsCoreSchemaNumber(y.Value()) {
			return exprlang.NumberType{}
		}
	}
	return exprlang.StringType{}
}

func (rule *Checker) checkWorkflowExpression(s workflowdecl.StringView, what, key string, shape schema.Shape) {
	if !s.Present() {
		return
	}
	ty := rule.checkOneExpression(s, what, key)
	if ty == nil {
		return
	}
	errors := shape.ValidateType(ty, what)
	// Inferred arrays can lose element types to AnyType; known JSON values allow
	// per-element and literal constraint checks.
	if len(errors) == 0 {
		if value, known := expressionLiteral(s.Value()); known {
			errors = schema.LiteralErrors(shape, value, what)
		}
	}
	for _, message := range errors {
		rule.Error(viewPosition(s), message)
	}
}
