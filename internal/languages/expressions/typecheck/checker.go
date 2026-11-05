package typecheck

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	expreval "actionlint.kjanat.dev/v2/internal/languages/expressions/evaluate"

	exprvalues "actionlint.kjanat.dev/v2/internal/languages/expressions/values"

	exprfunctions "actionlint.kjanat.dev/v2/internal/languages/expressions/functions"

	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"

	exprtypes "actionlint.kjanat.dev/v2/internal/languages/expressions/types"

	runnerjson "actionlint.kjanat.dev/v2/internal/languages/json"

	contextspec "actionlint.kjanat.dev/v2/internal/model/contexts"
	"actionlint.kjanat.dev/v2/internal/results/messages"
)

func ordinal(i int) string {
	suffix := "th"
	switch i % 10 {
	case 1:
		if i%100 != 11 {
			suffix = "st"
		}
	case 2:
		if i%100 != 12 {
			suffix = "nd"
		}
	case 3:
		if i%100 != 13 {
			suffix = "rd"
		}
	}
	return fmt.Sprintf("%d%s", i, suffix)
}

// parseFormatFuncSpecifiers returns references from a syntactically valid template.
// The argument count is checked separately by the call's format plan.
func parseFormatFuncSpecifiers(format string, capacity int) map[int]struct{} {
	references := make(map[int]struct{}, capacity)
	plan := exprfunctions.PlanFormat(format, 256)
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

// Functions

// builtinSecretProps are the secrets which GitHub Actions always provides to a workflow.
// ACTIONS_STEP_DEBUG and ACTIONS_RUNNER_DEBUG seem supplied from caller of the workflow (#130).
var builtinSecretProps = map[string]exprtypes.Type{
	"github_token":         exprtypes.StringType(),
	"actions_step_debug":   exprtypes.StringType(),
	"actions_runner_debug": exprtypes.StringType(),
}

// BuiltinGlobalVariableTypes defines types of all global variables. All context variables are
// documented at https://docs.github.com/en/actions/learn-github-actions/contexts
var BuiltinGlobalVariableTypes = map[string]exprtypes.Type{
	// https://docs.github.com/en/actions/reference/workflows-and-actions/contexts#github-context
	// Agents: https://docs.github.com/api/article/body?pathname=/en/actions/reference/workflows-and-actions/contexts
	"github": exprtypes.NewNamedObjectType(map[string]exprtypes.Type{
		"action":                    exprtypes.StringType(),
		"action_path":               exprtypes.StringType(), // Note: Composite actions only
		"action_ref":                exprtypes.StringType(),
		"action_repository":         exprtypes.StringType(),
		"action_status":             exprtypes.StringType(), // Note: Composite actions only
		"actor":                     exprtypes.StringType(),
		"actor_id":                  exprtypes.StringType(),
		"api_url":                   exprtypes.StringType(),
		"artifact_cache_size_limit": exprtypes.NumberType(), // Note: Missing from the context reference
		"artifacts":                 exprtypes.StringType(),
		"artifacts_list":            exprtypes.StringType(),
		"base_ref":                  exprtypes.StringType(),
		"env":                       exprtypes.StringType(),
		"event":                     exprtypes.NewEmptyObjectType().Type(), // Note: Stricter type check for this payload would be possible
		"event_name":                exprtypes.StringType(),
		"event_path":                exprtypes.StringType(),
		"graphql_url":               exprtypes.StringType(),
		"head_ref":                  exprtypes.StringType(),
		"job":                       exprtypes.StringType(),
		"output":                    exprtypes.StringType(), // Note: Runner-provided file command path missing from the context reference
		"path":                      exprtypes.StringType(),
		"ref":                       exprtypes.StringType(),
		"ref_name":                  exprtypes.StringType(),
		"ref_protected":             exprtypes.BoolType(),
		"ref_type":                  exprtypes.StringType(),
		"repository":                exprtypes.StringType(),
		"repository_id":             exprtypes.StringType(),
		"repository_owner":          exprtypes.StringType(),
		"repository_owner_id":       exprtypes.StringType(),
		"repository_visibility":     exprtypes.StringType(), // Note: Missing from the context reference; documented as an OIDC claim
		"repositoryUrl":             exprtypes.StringType(),
		"retention_days":            exprtypes.StringType(),
		"run_attempt":               exprtypes.StringType(),
		"run_id":                    exprtypes.StringType(),
		"run_number":                exprtypes.StringType(),
		"secret_source":             exprtypes.StringType(),
		"server_url":                exprtypes.StringType(),
		"sha":                       exprtypes.StringType(),
		"state":                     exprtypes.StringType(), // Note: Runner-provided file command path missing from the context reference
		"step_summary":              exprtypes.StringType(), // Note: Runner-provided file command path missing from the context reference
		"token":                     exprtypes.StringType(),
		"triggering_actor":          exprtypes.StringType(),
		"workflow":                  exprtypes.StringType(),
		"workflow_ref":              exprtypes.StringType(),
		"workflow_sha":              exprtypes.StringType(),
		"workspace":                 exprtypes.StringType(),
	}).Type(),
	// https://docs.github.com/en/actions/learn-github-actions/contexts#env-context
	"env": exprtypes.NewMapObjectType(exprtypes.StringType()).Type(), // env.<env_name>
	// https://docs.github.com/en/actions/learn-github-actions/contexts#job-context
	"job": exprtypes.NewStrictObjectType(map[string]exprtypes.Type{
		"check_run_id": exprtypes.NumberType(),
		"container": exprtypes.NewStrictObjectType(map[string]exprtypes.Type{
			"id":      exprtypes.StringType(),
			"network": exprtypes.StringType(),
		}).Type(),
		"services": exprtypes.NewMapObjectType(
			exprtypes.NewStrictObjectType(map[string]exprtypes.Type{
				"id":      exprtypes.StringType(), // job.services.<service id>.id
				"network": exprtypes.StringType(),
				"ports":   exprtypes.NewMapObjectType(exprtypes.StringType()).Type(),
			}).Type(),
		).Type(),
		"status":              exprtypes.StringType(),
		"workflow_file_path":  exprtypes.StringType(), // https://docs.github.com/en/actions/reference/workflows-and-actions/contexts#job-context
		"workflow_ref":        exprtypes.StringType(),
		"workflow_repository": exprtypes.StringType(),
		"workflow_sha":        exprtypes.StringType(),
	}).Type(),
	// https://docs.github.com/en/actions/learn-github-actions/contexts#steps-context
	"steps": exprtypes.NewEmptyStrictObjectType().Type(), // This value will be updated contextually
	// https://docs.github.com/en/actions/learn-github-actions/contexts#runner-context
	"runner": exprtypes.NewStrictObjectType(map[string]exprtypes.Type{
		"name":        exprtypes.StringType(),
		"os":          exprtypes.StringType(),
		"arch":        exprtypes.StringType(),
		"temp":        exprtypes.StringType(),
		"tool_cache":  exprtypes.StringType(),
		"debug":       exprtypes.StringType(),
		"environment": exprtypes.StringType(), // https://github.com/github/docs/issues/32443
	}).Type(),
	// https://docs.github.com/en/actions/learn-github-actions/contexts#secrets-context
	"secrets": exprtypes.NewStrictObjectType(builtinSecretProps).WithMapped(exprtypes.StringType()).Type(),
	// https://docs.github.com/en/actions/learn-github-actions/contexts#strategy-context
	"strategy": exprtypes.NewObjectType(map[string]exprtypes.Type{
		"fail-fast":    exprtypes.BoolType(),
		"job-index":    exprtypes.NumberType(),
		"job-total":    exprtypes.NumberType(),
		"max-parallel": exprtypes.NumberType(),
	}).Type(),
	// https://docs.github.com/en/actions/learn-github-actions/contexts#matrix-context
	"matrix": exprtypes.NewEmptyStrictObjectType().Type(), // This value will be updated contextually
	// https://docs.github.com/en/actions/learn-github-actions/contexts#needs-context
	"needs": exprtypes.NewEmptyStrictObjectType().Type(), // This value will be updated contextually
	// https://docs.github.com/en/actions/learn-github-actions/contexts#inputs-context
	// https://docs.github.com/en/actions/learn-github-actions/reusing-workflows
	"inputs": exprtypes.NewEmptyStrictObjectType().Type(),
	// https://docs.github.com/en/actions/learn-github-actions/contexts#vars-context
	"vars": exprtypes.NewMapObjectType(exprtypes.StringType()).Type(), // vars.<var_name>
}

// Semantics Checker

// Checker is a semantics Checker for expression syntax. It checks types of values
// in given expression syntax tree. It additionally checks other semantics like arguments of
// format() built-in function. To know the details of the syntax, see
//
// - https://docs.github.com/en/actions/learn-github-actions/contexts
// - https://docs.github.com/en/actions/learn-github-actions/expressions
type Checker struct {
	skipEvaluation        bool
	conditionalEvaluation bool
	funcs                 map[string][]*exprfunctions.FuncSignature
	vars                  map[string]exprtypes.Type
	errs                  []*Error
	varsCopied            bool
	nodeTypes             map[exprsyntax.Node]exprtypes.Type
	availableContexts     []string
	availableSpecialFuncs []string
	jobCondition          bool
	configVars            []string
	configSecrets         []string
	condition             bool
	checks                Checks
	callFacts             map[*exprsyntax.FuncCallNode]exprfunctions.FunctionCallFacts
	evaluationInvalid     bool
	validationFailed      bool
	staticInvalid         bool
}

// NewChecker creates a type checker with the configured names and semantic checks.
// A nil configuration uses built-in names and disables optional checks.
func NewChecker(cfg *Options) *Checker {
	c := &Checker{
		funcs:      exprfunctions.BuiltinFuncSignatures,
		vars:       BuiltinGlobalVariableTypes,
		varsCopied: false,
		nodeTypes:  make(map[exprsyntax.Node]exprtypes.Type),
	}
	if cfg != nil {
		c.checks = cfg.Checks
		c.configVars = cfg.Variables
		c.configSecrets = cfg.Secrets
	}
	return c
}

func errorAtExpr(kind findingKind, e exprsyntax.Node, msg string) *Error {
	span := exprsyntax.ExpressionSpan(e)
	return &Error{
		Message: msg,
		Rule:    kind.rule, Code: kind.code,
		Offset:    span.Start,
		EndOffset: span.End,
		Line:      span.Line,
		Column:    span.Column,
	}
}

func errorfAtExpr(kind findingKind, e exprsyntax.Node, format string, args ...any) *Error {
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

func (sema *Checker) errorf(kind findingKind, e exprsyntax.Node, format string, args ...any) {
	sema.validationError(errorfAtExpr(kind, e, format, args...))
}

func errorfAtCallee(kind findingKind, n *exprsyntax.FuncCallNode, format string, args ...any) *Error {
	return errorAtToken(kind, n.Token(), fmt.Sprintf(format, args...))
}

func (sema *Checker) calleeErrorf(kind findingKind, n *exprsyntax.FuncCallNode, format string, args ...any) {
	sema.validationError(errorfAtCallee(kind, n, format, args...))
}

func (sema *Checker) ensureVarsCopied() {
	if sema.varsCopied {
		return
	}

	// Make shallow copy of current variables map not to pollute global variable
	copied := make(map[string]exprtypes.Type, len(sema.vars))
	maps.Copy(copied, sema.vars)
	sema.vars = copied
	sema.varsCopied = true
}

// SetContextTypes replaces the expression's context names and types with a caller-supplied scope.
// Names are case-insensitive. The Checker copies the map.
func (sema *Checker) SetContextTypes(contexts map[string]exprtypes.Type) {
	sema.vars = make(map[string]exprtypes.Type, len(contexts))
	for name, ty := range contexts {
		sema.vars[strings.ToLower(name)] = ty
	}
	sema.varsCopied = true
}

// UpdateMatrix updates matrix object to given object type. Since matrix values change according to
// 'matrix' section of job configuration, the type needs to be updated.
func (sema *Checker) UpdateMatrix(ty exprtypes.ObjectType) {
	sema.ensureVarsCopied()
	sema.vars["matrix"] = ty.Type()
}

// UpdateSteps updates 'steps' context object to given object type.
func (sema *Checker) UpdateSteps(ty exprtypes.ObjectType) {
	sema.ensureVarsCopied()
	sema.vars["steps"] = ty.Type()
}

// UpdateNeeds updates 'needs' context object to given object type.
func (sema *Checker) UpdateNeeds(ty exprtypes.ObjectType) {
	sema.ensureVarsCopied()
	sema.vars["needs"] = ty.Type()
}

// UpdateSecrets updates 'secrets' context object to given object type. The strictness of the
// given type is preserved, so an open type keeps allowing unknown secret names.
func (sema *Checker) UpdateSecrets(ty exprtypes.ObjectType) {
	sema.ensureVarsCopied()

	// Merges automatically supplied secrets with manually defined secrets.
	var builder exprtypes.ObjectBuilder
	for name, value := range builtinSecretProps {
		builder.Set(name, value)
	}
	for name, value := range ty.Properties() {
		builder.SetProperty(ty.PropertyName(name), value)
	}
	if mapped, ok := ty.Mapped(); ok {
		builder.SetMapped(mapped)
	}
	sema.vars["secrets"] = builder.Build().Type()
}

// UpdateInputs updates 'inputs' context object to given object type.
func (sema *Checker) UpdateInputs(ty exprtypes.ObjectType) {
	sema.ensureVarsCopied()
	o, ok := sema.vars["inputs"].Object()
	if !ok || o.Len() == 0 && o.IsStrict() {
		sema.vars["inputs"] = ty.Type()
		return
	}
	// When both `workflow_call` and `workflow_dispatch` are the triggers of the workflow, `inputs` context can be used
	// by both events. To cover both cases, merge `inputs` contexts into one object type. (#263)
	sema.vars["inputs"] = o.Type().Merge(ty.Type())
}

// UpdateDispatchInputs updates 'github.event.inputs' and 'inputs' objects to given object type.
// https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows
func (sema *Checker) UpdateDispatchInputs(ty exprtypes.ObjectType) {
	sema.UpdateInputs(ty)

	// Update `github.event.inputs`.
	// Unlike `inputs.*`, type of `github.event.inputs.*` is always String unlike `inputs.*`. We need
	// to create a new type from `ty` (e.g. {foo: boolean, bar: number} -> {foo: String, bar: String})

	var builder exprtypes.ObjectBuilder
	for name := range ty.Properties() {
		builder.SetProperty(ty.PropertyName(name), exprtypes.StringType())
	}
	inputs := builder.Build().Type()

	gh, ok := sema.vars["github"].Object()
	if !ok {
		return
	}
	eventType, ok := gh.Property("event")
	if !ok {
		return
	}
	ev, ok := eventType.Object()
	if !ok {
		return
	}
	// Payload variants for workflow_dispatch receive the same inputs, so
	// narrowing github.event by event name keeps them.
	variants := ev.EventVariants()
	for i, variant := range variants {
		if variant.Event == "workflow_dispatch" {
			variants[i].Payload = variant.Payload.With("inputs", inputs)
		}
	}
	updated := ev.With("inputs", inputs).WithEventVariants(variants)
	sema.vars["github"] = gh.With("event", updated.Type()).Type()
}

// UpdateJobs updates 'jobs' context object to given object type.
func (sema *Checker) UpdateJobs(ty exprtypes.ObjectType) {
	sema.ensureVarsCopied()
	sema.vars["jobs"] = ty.Type()
}

// SetContextAvailability sets available context names while semantics checks. Some contexts limit
// where they can be used.
// https://docs.github.com/en/actions/learn-github-actions/contexts#context-availability
//
// Elements of 'avail' parameter must be in lower case to Check context names in case-insensitive.
//
// If this method is not called before checks, Checker considers any contexts are
// available by default.
// Available contexts for workflow keys can be obtained from actionlint.ContextAvailability.
func (sema *Checker) SetContextAvailability(avail []string) {
	sema.availableContexts = avail
}

func (sema *Checker) checkAvailableContext(n *exprsyntax.VariableNode) {
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

// SetSpecialFunctionAvailability sets names of available special functions while semantics checks.
// Some functions limit where they can be used.
// https://docs.github.com/en/actions/learn-github-actions/contexts#context-availability
//
// Elements of 'avail' parameter must be in lower case to Check function names in case-insensitive.
//
// If this method is not called before checks, Checker considers no special function is
// allowed by default. Allowed functions can be obtained from actionlint.SpecialFunctionNames global
// constant.
//
// Available function names for workflow keys can be obtained from actionlint.ContextAvailability.
func (sema *Checker) SetSpecialFunctionAvailability(avail []string) {
	sema.availableSpecialFuncs = avail
}

// SetWorkflowKeyAvailability selects workflow contexts, special functions, and
// position-specific signatures. Job conditions allow job arguments to success
// and failure; step and snapshot conditions require zero arguments.
func (sema *Checker) SetWorkflowKeyAvailability(key string) {
	sema.condition = strings.HasSuffix(key, ".if")
	contexts, functions := contextspec.WorkflowKeyAvailability(key)
	sema.SetContextAvailability(contexts)
	sema.SetSpecialFunctionAvailability(functions)
	sema.jobCondition = key == "jobs.<job_id>.if"
}

func (sema *Checker) checkSpecialFunctionAvailability(n *exprsyntax.FuncCallNode) {
	f := strings.ToLower(n.Callee)

	allowed, ok := contextspec.SpecialFunctionNames[f]
	if !ok {
		return // This function is not special
	}

	if slices.Contains(sema.availableSpecialFuncs, f) {
		return
	}

	sema.calleeErrorf(unavailableFunction,
		n,
		"calling function %q is not allowed here. %q is only available in %s. see https://docs.github.com/en/actions/learn-github-actions/contexts#context-availability for more details",
		n.Callee,
		n.Callee,
		messages.Quotes(allowed),
	)
}

func (sema *Checker) checkVariable(n *exprsyntax.VariableNode) exprtypes.Type {
	v, ok := sema.vars[n.Name]
	if !ok {
		if sigs, known := sema.funcs[n.Name]; known && len(sigs) > 0 {
			name := n.Token().Value
			for _, sig := range sigs {
				if len(sig.Params) == 0 {
					sema.errorf(missingCall, n, "function %q must be called with parentheses; did you mean %q?", name, name+"()")
					return exprtypes.AnyType()
				}
			}
			sema.errorf(missingCall, n, "function %q must be called with parentheses and arguments", name)
			return exprtypes.AnyType()
		}
		if inputs, ok := sema.vars["inputs"].Object(); ok && inputs.IsStrict() && slices.Contains(sema.availableContexts, "inputs") {
			if _, declared := inputs.Property(n.Name); declared {
				sema.errorf(unknownVariable, n, "undefined variable %q; did you mean %q?", n.Token().Value, "inputs."+n.Name)
				return exprtypes.AnyType()
			}
		}
		if suggestion := sema.contextPropertySuggestion(n.Name); suggestion != "" {
			sema.errorf(unknownVariable, n, "undefined variable %q; did you mean %q?", n.Token().Value, suggestion)
			return exprtypes.AnyType()
		}
		available := make([]string, 0, len(sema.availableContexts))
		for _, name := range sema.availableContexts {
			if _, exists := sema.vars[name]; exists {
				available = append(available, name)
			}
		}
		if suggestion := CloseName(n.Name, available); suggestion != "" {
			sema.errorf(unknownVariable, n, "undefined variable %q; did you mean %q?", n.Token().Value, suggestion)
			return exprtypes.AnyType()
		}
		ss := make([]string, 0, len(sema.vars))
		for n := range sema.vars {
			ss = append(ss, n)
		}
		sema.errorf(unknownVariable, n, "undefined variable %q. available variables are %s", n.Token().Value, messages.SortedQuotes(ss))
		return exprtypes.AnyType()
	}

	sema.checkAvailableContext(n)
	return v
}

func (sema *Checker) checkObjectDeref(n *exprsyntax.ObjectDerefNode) exprtypes.Type {
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
			if v, ok := n.Receiver.(*exprsyntax.VariableNode); ok {
				switch v.Name {
				case "vars":
					sema.checkConfigVariables(n, n.Property)
				case "secrets":
					sema.checkConfigSecrets(n, n.Property)
				}
			}
			return mapped
		}
		if receiver, ok := n.Receiver.(*exprsyntax.VariableNode); ok {
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
		// Map element type of delererenced array
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

func (sema *Checker) checkConfigVariables(n exprsyntax.Node, prop string) {
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

func (sema *Checker) checkConfigSecrets(n exprsyntax.Node, prop string) {
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

func (sema *Checker) checkArrayDeref(n *exprsyntax.ArrayDerefNode) exprtypes.Type {
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
		if path := strings.ToLower(exprsyntax.DisplayReference(n.Receiver)); path == "github.event" || strings.HasPrefix(path, "github.event.") || strings.HasPrefix(path, "github.event[") {
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

func (sema *Checker) checkIndexAccess(n *exprsyntax.IndexAccessNode) exprtypes.Type {
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
			if lit, ok := n.Index.(*exprsyntax.StringNode); ok {
				if prop, ok := object.Property(strings.ToLower(lit.Value)); ok {
					return prop
				}
				if mapped, ok := object.Mapped(); ok {
					if v, ok := n.Operand.(*exprsyntax.VariableNode); ok {
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
	signature  *exprfunctions.FuncSignature
	arityValid bool
	arguments  []exprfunctions.ParameterMatch
	err        *Error
}

func checkFuncSignature(n *exprsyntax.FuncCallNode, sig *exprfunctions.FuncSignature, args []exprfunctions.CallArgument) signatureMatch {
	match := signatureMatch{signature: sig}
	lp, la := len(sig.Params), len(args)
	if sig.MaxParams > 0 && la > sig.MaxParams {
		match.err = errorfAtCallee(argumentCount, n, "function %q takes at most %d arguments but %d arguments are given", n.Callee, sig.MaxParams, la)
		return match
	}
	if sig.VariableLengthParams && (lp > la) || !sig.VariableLengthParams && lp != la {
		atLeast := ""
		if sig.VariableLengthParams {
			atLeast = "at least "
		}
		match.err = errorfAtCallee(argumentCount, n,
			"number of arguments is wrong. function %q takes %s%d parameters but %d arguments are given",
			sig.String(), atLeast, lp, la)
		return match
	}
	if sig.EvaluationPolicy == exprfunctions.EvaluateCase && la%2 == 0 {
		match.err = errorfAtCallee(argumentCount, n, "case() requires an odd number of arguments (pred/value pairs + default) but got %d", la)
		return match
	}
	match.arityValid = true
	for i, arg := range args {
		p := sig.ParameterAt(i, la)
		m := exprfunctions.MatchParameter(p, arg, sig.CompositeStringFallback)
		match.arguments = append(match.arguments, m)
		if !m.Accepted && match.err == nil {
			match.err = errorfAtExpr(argumentType, n.Args[i],
				"%s argument of function call is not assignable. %q cannot be assigned to %q. called function type is %q",
				ordinal(i+1), arg.Source.String(), p.String(), sig.String())
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

func (sema *Checker) checkBuiltinFuncCall(n *exprsyntax.FuncCallNode, match signatureMatch) exprtypes.Type {
	sig := match.signature
	if sema.skipEvaluation && (strings.EqualFold(n.Callee, "fromJSON") || strings.EqualFold(n.Callee, "format")) {
		return sig.Ret
	}
	sema.checkSpecialFunctionAvailability(n)
	if sema.condition && sema.checks.CaseInsensitiveConditions && len(n.Args) == 2 {
		switch strings.ToLower(n.Callee) {
		case "contains", "startswith", "endswith":
			sema.checkIdentityComparison(n, n.Args[0], n.Args[1])
		}
	}

	// Special checks for specific built-in functions
	switch strings.ToLower(n.Callee) {
	case "format":
		arg := match.arguments[0]
		format, ok := arg.Value.(string)
		if arg.State != exprfunctions.ArgumentKnown || !ok {
			return sig.Ret
		}
		l := len(n.Args) - 1 // -1 means removing first format string argument
		plan, _ := exprfunctions.ArgumentFormatPlan(&match.arguments[0].Argument, l)
		for i := range l {
			if !plan.References(i) {
				finding := errorfAtCallee(formatPlaceholder, n, "format string %q does not contain placeholder {%d}. remove argument which is unused in the format string", format, i)
				finding.Advisory = true
				sema.errs = append(sema.errs, finding)
			}
		}
		missing := false
		for part := range plan.Parts() {
			if part.Index >= l {
				missing = true
				sema.evaluationError(errorfAtCallee(formatPlaceholder, n, "format string %q contains placeholder {%d} but only %d arguments are given to format", format, part.Index, l))
			}
		}
		if !plan.Valid() && !missing {
			sema.evaluationError(errorfAtCallee(formatPlaceholder, n, "format string %q has an invalid placeholder", format))
		}
		if plan.Valid() {
			for part := range plan.Parts() {
				if part.Index >= 0 && match.arguments[part.Index+1].Argument.State == exprfunctions.ArgumentInvalid {
					break
				}
				if part.Specifier != "" {
					sema.evaluationError(errorfAtCallee(formatPlaceholder, n, "format string %q uses unsupported format specifier %q", format, part.Specifier))
					break
				}
			}
		}
	case "fromjson":
		arg := match.arguments[0]
		input, ok := arg.Value.(string)
		if arg.State != exprfunctions.ArgumentKnown || !ok {
			return sig.Ret
		}
		value, collisions, err := runnerjson.Read(input)
		if err != nil && !errors.Is(err, runnerjson.ErrUnsupportedValue) {
			if syntax, ok := errors.AsType[*runnerjson.ReadError](err); ok {
				sema.evaluationError(errorfAtExpr(invalidJSON, n.Args[0], "broken JSON string is passed to fromJSON() at offset %d: %s", syntax.Offset, syntax))
			} else {
				sema.evaluationError(errorfAtExpr(invalidJSON, n.Args[0], "broken JSON string is passed to fromJSON(): %s", err))
			}
			return sig.Ret
		}
		for _, collision := range collisions {
			sema.errorf(jsonCollision, n.Args[0], "fromJSON() object members %q and %q collide under case-insensitive lookup at JSON offset %d; the selected value depends on JSON parsing mode", collision.First, collision.Second, collision.Offset)
		}
		if len(collisions) > 0 || err != nil {
			return sig.Ret
		}
		return exprtypes.TypeOfJSONValue(value)
	}

	return sig.Ret
}

func (sema *Checker) checkDemandedExpression(node exprsyntax.Node, demand exprfunctions.ArgumentDemand) (exprtypes.Type, bool) {
	previousInvalid := sema.evaluationInvalid
	previousConditional := sema.conditionalEvaluation
	previousSkipped := sema.skipEvaluation
	sema.evaluationInvalid = false
	sema.conditionalEvaluation = previousConditional || demand == exprfunctions.DemandConditional
	sema.skipEvaluation = previousSkipped || demand == exprfunctions.DemandSkipped
	ty := sema.check(node)
	invalid := sema.evaluationInvalid
	sema.evaluationInvalid = previousInvalid || (invalid && demand == exprfunctions.DemandRequired)
	sema.conditionalEvaluation = previousConditional
	sema.skipEvaluation = previousSkipped
	return ty, invalid
}

func (sema *Checker) checkExpressionBranch(node exprsyntax.Node, demand exprfunctions.ArgumentDemand) exprtypes.Type {
	ty, _ := sema.checkDemandedExpression(node, demand)
	return ty
}

func (sema *Checker) checkCallArgument(node exprsyntax.Node, demand exprfunctions.ArgumentDemand) exprfunctions.CallArgument {
	ty, invalid := sema.checkDemandedExpression(node, demand)
	arg := exprfunctions.CallArgument{Source: ty, Demand: demand}
	if invalid && demand != exprfunctions.DemandSkipped {
		arg.State = exprfunctions.ArgumentInvalid
	} else if demand != exprfunctions.DemandSkipped {
		if value, known := expreval.ConditionConstantValue(node); known {
			arg.Value, arg.State = value, exprfunctions.ArgumentKnown
		}
	}
	return arg
}

func (sema *Checker) checkCaseCall(n *exprsyntax.FuncCallNode, sig *exprfunctions.FuncSignature) exprtypes.Type {
	sema.checkSpecialFunctionAvailability(n)
	var alternatives []exprtypes.Type
	args := make([]exprfunctions.CallArgument, 0, len(n.Args))
	ty, _ := sema.checkCaseBranches(n.Args, &alternatives, &args, len(n.Args))
	sema.recordCallArguments(n, checkFuncSignature(n, sig, args))
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
func (sema *Checker) checkCaseBranches(args []exprsyntax.Node, alternatives *[]exprtypes.Type, checked *[]exprfunctions.CallArgument, count int) (exprtypes.Type, bool) {
	check := func(node exprsyntax.Node) exprfunctions.CallArgument {
		demand := exprfunctions.DemandForArgument(exprfunctions.EvaluateCase, len(*checked), count, *checked)
		arg := sema.checkCallArgument(node, demand)
		*checked = append(*checked, arg)
		return arg
	}
	if len(args) == 1 {
		arg := check(args[0])
		reachable := arg.Demand != exprfunctions.DemandSkipped
		if reachable {
			*alternatives = append(*alternatives, arg.Source)
		}
		return arg.Source, reachable
	}
	arg := check(args[0])
	ty := arg.Source
	if match := exprfunctions.MatchParameter(exprtypes.BoolType(), arg, false); !match.Accepted && arg.Demand != exprfunctions.DemandSkipped && !sema.skipEvaluation {
		previousInvalid := sema.evaluationInvalid
		previous := sema.conditionalEvaluation
		sema.conditionalEvaluation = previous || arg.Demand == exprfunctions.DemandConditional
		actual := ty
		if arg.State == exprfunctions.ArgumentKnown {
			actual = exprtypes.TypeOfJSONValue(arg.Value)
		}
		sema.evaluationError(errorfAtExpr(operandType, args[0], "case() predicate must be boolean but found %s", actual.String()))
		sema.conditionalEvaluation = previous
		if arg.Demand != exprfunctions.DemandRequired {
			sema.evaluationInvalid = previousInvalid
		}
	}
	var valueArgument exprfunctions.CallArgument
	valueType := sema.withEventGuard(args[0], true, func() exprtypes.Type {
		valueArgument = check(args[1])
		return valueArgument.Source
	})
	valueReachable := valueArgument.Demand != exprfunctions.DemandSkipped
	if valueReachable {
		*alternatives = append(*alternatives, valueType)
	}
	// Later predicates and the default are reachable only when this predicate
	// is false. Keep those guards nested so all earlier exclusions apply.
	var restType exprtypes.Type
	var restReachable bool
	sema.withEventGuard(args[0], false, func() exprtypes.Type {
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

func (sema *Checker) checkFuncCall(n *exprsyntax.FuncCallNode) exprtypes.Type {
	// Check function name in case insensitive. For example, toJson and toJSON are the same function.
	callee := strings.ToLower(n.Callee)
	sigs, ok := sema.funcs[callee]
	if !ok {
		candidates := make([]string, 0, len(sema.funcs))
		for name, signatures := range sema.funcs {
			if _, special := contextspec.SpecialFunctionNames[name]; special && !slices.Contains(sema.availableSpecialFuncs, name) {
				continue
			}
			if len(signatures) > 0 {
				candidates = append(candidates, signatures[0].Name)
			}
		}
		if suggestion := CloseName(callee, candidates); suggestion != "" {
			sema.calleeErrorf(unknownFunction, n, "undefined function %q; did you mean %q?", n.Callee, suggestion)
			return exprtypes.AnyType()
		}
		ss := make([]string, 0, len(sema.funcs))
		for n := range sema.funcs {
			ss = append(ss, n)
		}
		sema.calleeErrorf(unknownFunction, n, "undefined function %q. available functions are %s", n.Callee, messages.SortedQuotes(ss))
		return exprtypes.AnyType()
	}

	if sema.jobCondition && (callee == "success" || callee == "failure") && len(n.Args) > 0 {
		sigs = []*exprfunctions.FuncSignature{{
			Name:                 callee,
			Ret:                  exprtypes.BoolType(),
			Params:               []exprtypes.Type{exprtypes.StringType()},
			VariableLengthParams: true,
		}}
	}

	declared := make([]exprtypes.Type, 0, len(sigs))
	for _, sig := range sigs {
		if !slices.ContainsFunc(declared, func(ty exprtypes.Type) bool { return ty.Equal(sig.Ret) }) {
			declared = append(declared, sig.Ret)
		}
	}
	sema.callFacts[n] = exprfunctions.FunctionCallFacts{DeclaredReturns: declared}

	policy := sigs[0].EvaluationPolicy
	if policy == exprfunctions.EvaluateCase && len(n.Args) >= 3 && len(n.Args)%2 == 1 && (sigs[0].MaxParams == 0 || len(n.Args) <= sigs[0].MaxParams) {
		return sema.checkCaseCall(n, sigs[0])
	}
	args := make([]exprfunctions.CallArgument, len(n.Args))
	if len(n.Args) > 0 {
		args[0] = sema.checkCallArgument(n.Args[0], exprfunctions.DemandRequired)
		blocked := args[0].State == exprfunctions.ArgumentInvalid
		for _, index := range exprfunctions.ArgumentExecutionOrder(policy, len(n.Args), args) {
			demand := exprfunctions.DemandForArgument(policy, index, len(n.Args), args)
			if blocked {
				demand = exprfunctions.DemandSkipped
			}
			args[index] = sema.checkCallArgument(n.Args[index], demand)
			blocked = blocked || (demand == exprfunctions.DemandRequired && args[index].State == exprfunctions.ArgumentInvalid)
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
		} else {
			for _, candidate := range candidates {
				sema.validationError(candidate.err)
			}
			if len(candidates) > 0 {
				sema.recordCallArguments(n, candidates[0])
			}
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
			if arg.Conversion != exprfunctions.ConversionNone {
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
	if sema.checks.SuspiciousCoercion {
		for i, arg := range match.arguments {
			if !sema.skipEvaluation && arg.Argument.Demand != exprfunctions.DemandSkipped && arg.Conversion == exprfunctions.ConversionString && (arg.Argument.Source.Kind() == exprtypes.KindBool || arg.Argument.Source.Kind() == exprtypes.KindNull) {
				operation := "converts"
				if sema.conditionalEvaluation || arg.Argument.Demand == exprfunctions.DemandConditional {
					operation = "may convert"
				}
				sema.ruleError(n.Args[i], "coercion", fmt.Sprintf("%s argument of %s() %s %s to a string; check whether a string value was intended", ordinal(i+1), n.Callee, operation, arg.Argument.Source))
			}
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

func (sema *Checker) recordCallArguments(n *exprsyntax.FuncCallNode, match signatureMatch) {
	facts := sema.callFacts[n]
	for _, arg := range match.arguments {
		value := exprvalues.EvaluationResult{State: exprvalues.EvaluationState(arg.State), Value: arg.Value}
		if arg.Argument.Demand == exprfunctions.DemandSkipped {
			value = exprvalues.EvaluationResult{State: exprvalues.EvaluationUnknown}
		}
		facts.Arguments = append(facts.Arguments, exprfunctions.FunctionArgumentFacts{
			Source:      arg.Argument.Source,
			SourceValue: exprvalues.EvaluationResult{State: exprvalues.EvaluationState(arg.Argument.State), Value: arg.Argument.Value},
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

func (sema *Checker) checkNotOp(n *exprsyntax.NotOpNode) exprtypes.Type {
	ty := sema.check(n.Operand)
	if !exprtypes.BoolType().Assignable(ty) {
		sema.errorf(operandType, n, "type of operand of ! operator %q is not assignable to type \"bool\"", ty.String())
	}
	return exprtypes.BoolType()
}

// suspiciousComparison identifies supported comparisons whose conversion can hide
// a mistaken operand. Unknown types provide no evidence for this warning.
func suspiciousComparison(op exprsyntax.CompareOpNodeKind, left, right exprtypes.Type) bool {
	l, r := left.Kind(), right.Kind()
	if l == exprtypes.KindAny || r == exprtypes.KindAny {
		return false
	}
	composite := func(k exprtypes.Kind) bool { return k == exprtypes.KindArray || k == exprtypes.KindObject }
	if op.IsEqualityOp() {
		return l != r && (composite(l) || composite(r))
	}
	return composite(l) || composite(r) || l == exprtypes.KindNull || r == exprtypes.KindNull || l == exprtypes.KindBool || r == exprtypes.KindBool
}

func (sema *Checker) checkCompareOp(n *exprsyntax.CompareOpNode) exprtypes.Type {
	l := sema.check(n.Left)
	r := sema.check(n.Right)
	sema.checkConditionComparison(n)

	if sema.checks.SuspiciousCoercion && suspiciousComparison(n.Kind, l, r) {
		sema.ruleError(n, "coercion", fmt.Sprintf("comparison of %s and %s with %s uses Actions coercion or NaN; check whether these operand types were intended", l, r, n.Kind))
	}

	return exprtypes.BoolType()
}

// checkWithNarrowing checks type of given expression with type narrowing. Type narrowing narrows
// down the type of the expression by assuming its value. For example, `l && r` is typed as
// `typeof(l) | typeof(r)` usually. However when the expression is assumed to be true, its type can
// be narrowed down to `typeof(r)`.
// This analysis is useful to make type checking more accurate. For example, `some_var && 60 || 20`
// can be typed as `number` instead of `typeof(some_var) | number`. (#384)
func (sema *Checker) checkWithNarrowing(n exprsyntax.Node, isTruthy bool) exprtypes.Type {
	switch n := n.(type) {
	case *exprsyntax.LogicalOpNode:
		switch n.Kind {
		case exprsyntax.LogicalOpNodeKindAnd:
			// When `l && r` is true, narrow its type to `typeof(r)`
			if isTruthy {
				left := sema.check(n.Left)
				right := sema.withEventGuard(n.Left, true, func() exprtypes.Type { return sema.checkExpressionBranch(n.Right, logicalRightDemand(n)) })
				sema.nodeTypes[n] = left.Merge(right)
				return right
			}
		case exprsyntax.LogicalOpNodeKindOr:
			// When `l || r` is false, narrow its type to `typeof(r)`
			if !isTruthy {
				left := sema.check(n.Left)
				right := sema.withEventGuard(n.Left, false, func() exprtypes.Type { return sema.checkExpressionBranch(n.Right, logicalRightDemand(n)) })
				sema.nodeTypes[n] = left.Merge(right)
				return right
			}
		case exprsyntax.LogicalOpNodeKindInvalid:
			// Invalid operators are not narrowed.
		}
		return sema.checkLogicalOp(n)
	case *exprsyntax.NotOpNode:
		sema.nodeTypes[n] = exprtypes.BoolType()
		return sema.checkWithNarrowing(n.Operand, !isTruthy)
	default:
		return sema.check(n)
	}
}

func (sema *Checker) checkLogicalOp(n *exprsyntax.LogicalOpNode) (ty exprtypes.Type) {
	defer func() { sema.nodeTypes[n] = ty }()

	switch n.Kind {
	case exprsyntax.LogicalOpNodeKindAnd:
		// When `l` is false in `l && r`, its type is `typeof(l)`. Otherwise `typeof(r)`.
		// Narrow the type of LHS expression by assuming its value is falsy.
		left := sema.checkWithNarrowing(n.Left, false)
		right := sema.withEventGuard(n.Left, true, func() exprtypes.Type { return sema.checkExpressionBranch(n.Right, logicalRightDemand(n)) })
		return left.Merge(right)
	case exprsyntax.LogicalOpNodeKindOr:
		// When `l` is true in `l || r`, its type is `typeof(l)`. Otherwise `typeof(r).
		// Narrow the type of LHS expression by assuming its value is truthy.
		left := sema.checkWithNarrowing(n.Left, true)
		right := sema.withEventGuard(n.Left, false, func() exprtypes.Type { return sema.checkExpressionBranch(n.Right, logicalRightDemand(n)) })
		return left.Merge(right)
	case exprsyntax.LogicalOpNodeKindInvalid:
		// Invalid operators still check both operands.
		fallthrough
	default:
		sema.check(n.Left)
		sema.check(n.Right)
		return exprtypes.AnyType()
	}
}

func (sema *Checker) check(expr exprsyntax.Node) (ty exprtypes.Type) {
	defer func() { sema.nodeTypes[expr] = ty }()

	switch e := expr.(type) {
	case *exprsyntax.VariableNode:
		return sema.checkVariable(e)
	case *exprsyntax.NullNode:
		return exprtypes.NullType()
	case *exprsyntax.BoolNode:
		return exprtypes.BoolType()
	case *exprsyntax.StringNode:
		return exprtypes.StringType()
	case *exprsyntax.IntNode, *exprsyntax.FloatNode:
		return exprtypes.NumberType()
	case *exprsyntax.ObjectDerefNode:
		return sema.checkObjectDeref(e)
	case *exprsyntax.ArrayDerefNode:
		return sema.checkArrayDeref(e)
	case *exprsyntax.IndexAccessNode:
		return sema.checkIndexAccess(e)
	case *exprsyntax.FuncCallNode:
		return sema.checkFuncCall(e)
	case *exprsyntax.NotOpNode:
		return sema.checkNotOp(e)
	case *exprsyntax.CompareOpNode:
		return sema.checkCompareOp(e)
	case *exprsyntax.LogicalOpNode:
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
	Valid      bool
	Findings   []*Error
	Evaluation exprvalues.EvaluationResult
	// Call is nil when the root expression has no known function contract.
	Call         *exprfunctions.FunctionCallFacts
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

// CheckResult checks an expression and returns its independent semantic facts.
func (sema *Checker) CheckResult(expr exprsyntax.Node) ExpressionCheckResult {
	sema.errs = []*Error{}
	sema.callFacts = make(map[*exprsyntax.FuncCallNode]exprfunctions.FunctionCallFacts)
	sema.nodeTypes = make(map[exprsyntax.Node]exprtypes.Type)
	sema.evaluationInvalid = false
	sema.validationFailed = false
	sema.staticInvalid = false
	ty := sema.check(expr)
	errs := detachErrors(sema.errs)

	evaluation := exprvalues.EvaluationResult{State: exprvalues.EvaluationUnknown}
	phase := FailureNone
	if sema.staticInvalid {
		evaluation.State = exprvalues.EvaluationNotAttempted
		phase = FailureStaticValidation
	} else if sema.evaluationInvalid {
		evaluation.State = exprvalues.EvaluationInvalid
		phase = FailureEvaluation
	} else if value, known := expreval.ConditionConstantValue(expr); known {
		evaluation = exprvalues.EvaluationResult{State: exprvalues.EvaluationKnown, Value: value}
	}
	result := ExpressionCheckResult{Type: ty, Types: InferredTypes{byNode: sema.nodeTypes}, Valid: !sema.validationFailed, Findings: errs, Evaluation: evaluation, FailurePhase: phase}
	if call, ok := expr.(*exprsyntax.FuncCallNode); ok {
		if facts, found := sema.callFacts[call]; found {
			if evaluation.State == exprvalues.EvaluationKnown {
				facts.ReturnDomain = exprfunctions.ReturnDomainExcludesValue
				for _, declared := range facts.DeclaredReturns {
					if returnContainsValue(declared, evaluation.Value) {
						facts.ReturnDomain = exprfunctions.ReturnDomainContainsValue
						break
					}
				}
			}
			if facts.ReturnDomain == exprfunctions.ReturnDomainContainsValue {
				actual := exprtypes.TypeOfJSONValue(evaluation.Value)
				if !actual.Equal(result.Type) {
					facts.RefinedAlternatives = []exprtypes.Type{actual}
				}
			}
			result.Call = &facts
		}
	}
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

// Check returns the inferred type and findings for compatibility.
func (sema *Checker) Check(expr exprsyntax.Node) (exprtypes.Type, []*Error) {
	result := sema.CheckResult(expr)
	return result.Type, result.Findings
}

// IsConstant returns the given expression is a constant. For example the following expressions
// are constants.
//   - 1 == 2
//   - (!true && 'foo') || 'bar'
//   - startsWith('foobar', 'foo')
//   - format('{} + {} = {}', 1, 2, 3)
func (sema *Checker) IsConstant(expr exprsyntax.Node) bool {
	switch e := expr.(type) {
	case *exprsyntax.NullNode, *exprsyntax.BoolNode, *exprsyntax.IntNode, *exprsyntax.FloatNode, *exprsyntax.StringNode:
		return true
	case *exprsyntax.VariableNode, *exprsyntax.ObjectDerefNode, *exprsyntax.ArrayDerefNode, *exprsyntax.IndexAccessNode:
		return false
	case *exprsyntax.NotOpNode:
		return sema.IsConstant(e.Operand)
	case *exprsyntax.CompareOpNode:
		return sema.IsConstant(e.Left) && sema.IsConstant(e.Right)
	case *exprsyntax.LogicalOpNode:
		return sema.IsConstant(e.Left) && sema.IsConstant(e.Right)
	case *exprsyntax.FuncCallNode:
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
