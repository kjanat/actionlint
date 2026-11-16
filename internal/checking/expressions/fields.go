package expressions

import (
	"maps"
	"slices"
	"strconv"
	"strings"

	"actionlint.kjanat.dev/v2/internal/checking/rules/correctness"
	"actionlint.kjanat.dev/v2/internal/input/sourcetext"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/exprtypes"

	"actionlint.kjanat.dev/v2/internal/languages/yaml/codec"

	"actionlint.kjanat.dev/v2/internal/model/contexts/value"

	"actionlint.kjanat.dev/v2/internal/model/workflows"

	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"

	"actionlint.kjanat.dev/v2/internal/model/workflows/mappings"
	"actionlint.kjanat.dev/v2/internal/model/workflows/schema"
)

// checkOneExpression checks a value that must be exactly one ${{ }}
// expression. ok is false when s is absent or the expression has errors.
func (rule *Checker) checkOneExpression(s *definition.String, what, workflowKey string) (ty exprtypes.Type, ok bool) {
	// checkString is not available since it checks types for embedding values into a string
	if s == nil {
		return exprtypes.AnyType(), false
	}

	ts, ok := rule.checkExprsIn(s.Value, scalarPosition(s), s.Quoted, false, workflowKey)

	if len(ts) != 1 {
		if !ok {
			return exprtypes.AnyType(), false
		}
		// This case should be unreachable since only one ${{ }} is included is checked by parser
		rule.Errorf(scalarPosition(s), "one ${{ }} expression should be included in %q value but got %d expressions", what, len(ts))
		return exprtypes.AnyType(), false
	}
	if !ok {
		return ts[0].semantics.Type, false
	}

	return ts[0].ty, true
}

// checkObjectTy reports ty unless it is an object or any, and returns whether
// it is one of those.
func (rule *Checker) checkObjectTy(ty exprtypes.Type, pos *sourcetext.Position, what string) bool {
	if k := ty.Kind(); k == exprtypes.KindObject || k == exprtypes.KindAny {
		return true
	}
	rule.Errorf(pos, "type of expression at %q must be object but found type %s", what, ty.String())
	return false
}

// checkArrayTy reports ty unless it is an array or any, and returns whether
// it is one of those.
func (rule *Checker) checkArrayTy(ty exprtypes.Type, pos *sourcetext.Position, what string) bool {
	if k := ty.Kind(); k == exprtypes.KindArray || k == exprtypes.KindAny {
		return true
	}
	rule.Errorf(pos, "type of expression at %q must be array but found type %s", what, ty.String())
	return false
}

// checkNumberTy reports ty unless it is a number or any, and returns whether
// it is one of those.
func (rule *Checker) checkNumberTy(ty exprtypes.Type, pos *sourcetext.Position, what string) bool {
	if k := ty.Kind(); k == exprtypes.KindNumber || k == exprtypes.KindAny {
		return true
	}
	rule.Errorf(pos, "type of expression at %q must be number but found type %s", what, ty.String())
	return false
}

// checkObjectExpression checks a single expression that must be an object.
// ok is false when the expression has errors or another type.
func (rule *Checker) checkObjectExpression(s *definition.String, what, workflowKey string) (ty exprtypes.Type, ok bool) {
	ty, ok = rule.checkOneExpression(s, what, workflowKey)
	return ty, ok && rule.checkObjectTy(ty, scalarPosition(s), what)
}

// checkArrayExpression checks a single expression that must be an array.
// ok is false when the expression has errors or another type.
func (rule *Checker) checkArrayExpression(s *definition.String, what, workflowKey string) (ty exprtypes.Type, ok bool) {
	ty, ok = rule.checkOneExpression(s, what, workflowKey)
	return ty, ok && rule.checkArrayTy(ty, scalarPosition(s), what)
}

func (rule *Checker) checkNumberExpression(s *definition.String, what, workflowKey string) {
	if ty, ok := rule.checkOneExpression(s, what, workflowKey); ok {
		rule.checkNumberTy(ty, scalarPosition(s), what)
	}
}

func (rule *Checker) checkActionInputs(action *definition.ExecAction, uses *definition.String) {
	const key = "jobs.<job_id>.steps.with"
	if action == nil {
		return
	}
	actionSpec := uses.Text()
	var checkMapping func(map[string]*definition.ActionArgument, *definition.String, []*definition.ActionInputMapping)
	checkMapping = func(inputs map[string]*definition.ActionArgument, expression *definition.String, insertions []*definition.ActionInputMapping) {
		rule.checkWorkflowExpression(expression, "with", key, schema.StringMap)
		for _, id := range slices.Sorted(maps.Keys(inputs)) {
			input := inputs[id]
			if input == nil {
				continue
			}
			rule.checkString(input.Name, key)
			name := workflows.Scalar(input.Name)
			if input.Name == nil {
				name = value.YAMLScalar(id, definition.YamlTagStr, value.Origin{})
			}
			canonical, state := mappings.ResolveKey(name)
			isScriptAction := strings.HasPrefix(actionSpec, "actions/github-script@")
			if isScriptAction && (state == mappings.UnknownKey || state == mappings.KnownKey && strings.EqualFold(canonical, "script")) {
				rule.checkScriptString(input.Value, key)
			} else {
				rule.checkString(input.Value, key)
			}
		}
		for _, insertion := range insertions {
			if insertion != nil {
				checkMapping(insertion.Inputs, insertion.Expression, insertion.Insertions)
			}
		}
	}
	checkMapping(action.Inputs, action.InputsExpression, action.InputInsertions)
}

func (rule *Checker) checkEnv(env *definition.Env, workflowKey string) {
	if env == nil {
		return
	}

	var checkValues func(*definition.Env)
	checkValues = func(current *definition.Env) {
		if current == nil {
			return
		}
		if current.Expression != nil {
			rule.checkWorkflowExpression(current.Expression, "env", workflowKey, schema.StringMap)
		}
		for _, variable := range current.Vars {
			if variable != nil {
				rule.checkString(variable.Name, workflowKey)
				rule.checkString(variable.Value, workflowKey)
			}
		}
		for _, insertion := range current.Insertions {
			checkValues(insertion)
		}
	}
	checkValues(env)
}

func (rule *Checker) checkContainer(c *definition.Container, workflowKey, childWorkflowKeyPrefix string) {
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
			for _, field := range []struct {
				name  string
				value *definition.String
			}{{"username", c.Credentials.Username}, {"password", c.Credentials.Password}} {
				if field.value.IsExpressionAssigned() {
					rule.checkWorkflowExpression(field.value, "credentials."+field.name, k, schema.NonEmpty)
				} else {
					rule.checkString(field.value, k)
				}
			}
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

func (rule *Checker) checkConcurrency(c *definition.Concurrency, workflowKey string) {
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
	// The parser diagnoses literal YAML conflicts; check evaluated expressions here.
	if !c.Queue.ContainsExpression() && c.CancelInProgress.Expression == nil {
		return
	}
	var queue any = c.Queue.Value
	if c.Queue.ContainsExpression() {
		queue, _ = expressionLiteral(c.Queue.Value)
	}
	var cancel any = c.CancelInProgress.Value
	if c.CancelInProgress.Expression != nil {
		cancel, _ = expressionLiteral(c.CancelInProgress.Expression.Text())
	}
	if queue == "max" && cancel == true {
		rule.Error(scalarPosition(c.Queue), `"queue: max" cannot be combined with "cancel-in-progress: true" in "concurrency" section`)
	}
}

func (rule *Checker) checkDefaults(d *definition.Defaults, workflowKey string) {
	if d == nil || d.Run == nil {
		return
	}
	rule.checkWorkflowExpression(d.Run.Expression, "defaults.run", workflowKey, schema.DefaultsRun)
	rule.checkString(d.Run.Shell, workflowKey)
	rule.checkString(d.Run.WorkingDirectory, workflowKey)
}

func (rule *Checker) checkWorkflowCall(c *definition.WorkflowCall) {
	if c == nil || c.Uses == nil {
		return
	}

	rule.checkString(c.Uses, "")

	m, err := rule.workflowMetadata(c.Uses.Text())
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
		if !ok || mi.Type == definition.WorkflowCallEventInputTypeInvalid {
			continue
		}

		v := strings.TrimSpace(i.Value.Value)

		ty := exprtypes.StringType()
		switch len(ts) {
		case 0:
			switch v {
			case "null":
				ty = exprtypes.NullType()
			case "true", "false":
				ty = exprtypes.BoolType()
			default:
				if _, err := strconv.ParseFloat(v, 64); err == nil {
					ty = exprtypes.NumberType()
				}
			}
		case 1:
			if i.Value.IsExpressionAssigned() {
				ty = ts[0].ty
			}
		}

		if !mi.Type.ExprType().Assignable(ty) {
			rule.Errorf(
				scalarPosition(i.Value),
				"input %q is typed as %s by reusable workflow %q. %s value cannot be assigned",
				mi.Name,
				mi.Type.String(),
				c.Uses.Text(),
				ty.String(),
			)
		}
	}

	for _, s := range c.Secrets {
		rule.checkString(s.Value, "jobs.<job_id>.secrets.<secrets_id>")
	}
}

func (rule *Checker) checkSnapshot(s *definition.Snapshot) {
	if s == nil {
		return
	}
	rule.checkWorkflowExpression(s.Expression, "snapshot", "jobs.<job_id>.snapshot", schema.Snapshot)
	for _, field := range []struct {
		value *definition.String
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

func (rule *Checker) checkWebhookEventFilter(f *definition.WebhookEventFilter) {
	if f == nil {
		return
	}
	rule.checkStrings(f.Values, "")
}

func (rule *Checker) checkStrings(ss []*definition.String, workflowKey string) {
	for _, s := range ss {
		rule.checkString(s, workflowKey)
	}
}

func (rule *Checker) checkIfCondition(str *definition.String, workflowKey string) {
	if str == nil {
		return
	}

	// Conditions can omit ${{ }}, including expressions with operators.
	// Quote a condition beginning with ! to prevent YAML from treating it as a tag.
	// https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax#jobsjob_idstepsif
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

	restore := rule.scopeExpressionSource(str.Value, scalarPosition(str))
	defer restore()
	expr, err, offset := syntax.ParseConditionExpression(str.Value)
	if syntax.ConditionSource(str.Value) != str.Value && offset == 0 {
		rule.expressionSource = nil
	}
	rule.expressionOffset = offset
	base := sourcetext.ZeroIfNil(scalarPosition(str))
	base.Column += offset
	if offset != 0 && str.Quoted {
		base.Column++
	}
	if err != nil {
		rule.syntaxError(err, base)
		return
	}
	if expr == nil {
		rule.checkString(str, workflowKey)
		return
	}
	if result := rule.checkSemanticsOfExprNode(expr, base, false, workflowKey); result.Valid && !exprtypes.BoolType().Assignable(result.Type) {
		rule.Errorf(scalarPosition(str), "\"if\" condition should be type \"bool\" but got type %q", result.Type.String())
	}
}

func (rule *Checker) checkTemplateEvaluatedType(ts []typedExpr) {
	for _, t := range ts {
		ty := t.ty
		if t.refinement.hasTemplateInvalid {
			ty = t.refinement.templateInvalid
		}
		switch ty.Kind() {
		case exprtypes.KindObject, exprtypes.KindArray:
			rule.Errorf(&t.pos, "object and array values should not be evaluated in template with ${{ }} but evaluating the value of type %s", ty)
		case exprtypes.KindAny, exprtypes.KindNumber, exprtypes.KindBool, exprtypes.KindString, exprtypes.KindNull:
			// The runner converts null to the empty string, including missing
			// matrix members. Script-position checks still guard that insertion.
		}
	}
}

func (rule *Checker) checkString(str *definition.String, workflowKey string) []typedExpr {
	if str == nil {
		return nil
	}

	ts, ok := rule.checkExprsIn(str.Value, scalarPosition(str), str.Quoted, false, workflowKey)
	if !ok {
		return nil
	}
	// Static fields accept a single string literal expression after template folding.
	if workflowKey == "" && len(ts) > 0 && (len(ts) != 1 || !str.IsExpressionAssigned()) {
		rule.Error(scalarPosition(str), "expressions are not allowed in this static workflow field")
		return nil
	}

	rule.checkTemplateEvaluatedType(ts)
	return ts
}

func (rule *Checker) checkScriptString(str *definition.String, workflowKey string, shell ...string) {
	if str == nil {
		return
	}

	previous, previousLists, previousPosition := rule.scriptScalarArgs, rule.scriptWordLists, rule.scriptInsertion
	previousEnvironment := rule.scriptEnvironment
	rule.scriptScalarArgs = nil
	rule.scriptWordLists = nil
	rule.scriptEnvironment = nil
	rule.scriptInsertion = correctness.UnknownScriptInsertion
	if len(shell) > 0 {
		proofs := proveScriptArguments(str.Value, shell[0])
		rule.scriptScalarArgs, rule.scriptWordLists = proofs.scalars, proofs.wordLists
		rule.scriptEnvironment = proofs.environment
		if _, ok := scriptShellVariant(shell[0]); ok {
			rule.scriptInsertion = correctness.ShellWordInsertion
		}
	}
	defer func() {
		rule.scriptScalarArgs, rule.scriptWordLists, rule.scriptInsertion = previous, previousLists, previousPosition
		rule.scriptEnvironment = previousEnvironment
	}()
	ts, ok := rule.checkExprsIn(str.Value, scalarPosition(str), str.Quoted, true, workflowKey)
	if !ok {
		return
	}

	rule.checkTemplateEvaluatedType(ts)
}

func (rule *Checker) checkBool(b *definition.Bool, workflowKey string) {
	if b == nil || b.Expression == nil {
		return
	}

	ty, ok := rule.checkOneExpression(b.Expression, "bool value", workflowKey)
	if !ok {
		return
	}

	if k := ty.Kind(); k != exprtypes.KindBool && k != exprtypes.KindAny {
		rule.Errorf(scalarPosition(b.Expression), "type of expression must be bool but found type %s", ty.String())
	}
}

func (rule *Checker) checkMaxParallel(i *definition.Int, workflowKey string) {
	if i == nil || i.Expression == nil {
		return
	}
	ty, ok := rule.checkOneExpression(i.Expression, "integer value", workflowKey)
	if !ok || !rule.checkNumberTy(ty, scalarPosition(i.Expression), "integer value") {
		return
	}
	if value, known := expressionLiteral(i.Expression.Text()); known {
		for _, message := range schema.LiteralErrors(schema.Positive, value, "max-parallel") {
			rule.Error(scalarPosition(i.Expression), message)
		}
	}
}

func (rule *Checker) checkFloat(f *definition.Float, workflowKey string) {
	if f == nil {
		return
	}
	rule.checkNumberExpression(f.Expression, "float number value", workflowKey)
}

func (rule *Checker) checkRawYAMLValue(v definition.RawYAMLValue) exprtypes.Type {
	if object, ok := v.(*definition.RawYAMLObject); ok {
		properties := map[string]exprtypes.Type{}
		for key, property := range object.Props {
			properties[key] = rule.checkRawYAMLValue(property)
		}
		return exprtypes.NewStrictObjectType(properties).Type()
	}
	if array, ok := v.(*definition.RawYAMLArray); ok {
		var elements elementTypes
		for _, item := range array.Elems {
			if ty, ok := rule.checkMatrixSequenceItem(item); ok {
				elements.add(ty)
			}
		}
		return exprtypes.NewArrayType(elements.merged()).Type()
	}
	if scalar, ok := v.(*definition.RawYAMLString); ok {
		return rule.checkRawYAMLString(scalar)
	}
	panic("unreachable")
}

func (rule *Checker) checkRawYAMLString(y *definition.RawYAMLString) exprtypes.Type {
	ts, ok := rule.checkExprsIn(y.Value, y.Pos(), false, false, "jobs.<job_id>.strategy")

	if definition.IsExprAssigned(y.Value) {
		if !ok || len(ts) != 1 {
			return exprtypes.AnyType()
		}
		if value, known := expressionLiteral(y.Value); known {
			return exprtypes.TypeOfJSONValue(value)
		}
		return ts[0].ty
	}

	switch y.Tag {
	case definition.YamlTagBool:
		if codec.IsCoreSchemaBool(y.Value) {
			return exprtypes.BoolType()
		}
	case definition.YamlTagNull:
		if codec.IsCoreSchemaNull(y.Value) {
			return exprtypes.NullType()
		}
	case definition.YamlTagInt, definition.YamlTagFloat:
		if codec.IsCoreSchemaNumber(y.Value) {
			return exprtypes.NumberType()
		}
	}
	return exprtypes.StringType()
}

func (rule *Checker) checkWorkflowExpression(s *definition.String, what, key string, shape schema.Shape) {
	if s == nil {
		return
	}
	ty, ok := rule.checkOneExpression(s, what, key)
	if !ok {
		return
	}
	errors := shape.ValidateType(ty, what)
	// Inferred arrays can lose element types to AnyType; known JSON values allow
	// per-element and literal constraint checks.
	if len(errors) == 0 {
		if value, known := expressionLiteral(s.Value); known {
			errors = schema.LiteralErrors(shape, value, what)
		}
	}
	for _, message := range errors {
		rule.Error(scalarPosition(s), message)
	}
}
