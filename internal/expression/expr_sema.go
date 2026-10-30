package expression

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	contextspec "actionlint.kjanat.dev/v2/internal/expression/context"
	stringutil "actionlint.kjanat.dev/v2/internal/stringutil"
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

// parseFormatFuncSpecifiers parses the format String passed to `format()` calls.
// https://docs.github.com/en/actions/writing-workflows/choosing-what-your-workflow-does/evaluate-expressions-in-workflows-and-actions#format
func parseFormatFuncSpecifiers(f string, n int) map[int]struct{} {
	ret := make(map[int]struct{}, n)

	const none int = -1
	start, end := none, none

	for i, r := range f {
		// Before specifier
		if start == none {
			if r == '{' {
				// Opening brace
				start = i + 1
			}
			continue
		}

		// Inside specifier
		if end == none {
			switch {
			case r == '{' && i == start:
				// Escaped '{'
				start = none
			case r == '}':
				if i == start {
					// Empty specifier '{}'
					start = none
				} else {
					// Closing brace.
					end = i
				}
			case r < '0' || r > '9':
				if r == '{' {
					start = i + 1
				} else {
					start = none
				}
			}
			continue
		}

		// After specifier
		if r == '}' {
			// Parsing specifier needs to be delayed while '}' continues.
			continue
		}
		if (i-end)%2 == 1 {
			// Odd number of closing braces means the specifier closed. For example '{0}}}' contains
			// a specifier but '{0}}}}' doesn't.
			v, _ := strconv.Atoi(f[start:end])
			ret[v] = struct{}{}
		}
		if r == '{' {
			start = i + 1
		} else {
			start = none
		}
		end = none
	}

	// When the input ends while '}' continues at end of specifier
	if start > none && end > none && (len(f)-end)%2 == 1 {
		v, _ := strconv.Atoi(f[start:end])
		ret[v] = struct{}{}
	}

	return ret
}

// Functions

// FuncSignature is a signature of function, which holds return and arguments types.
type FuncSignature struct {
	// Name is a name of the function.
	Name string
	// Ret is a return type of the function.
	Ret Type
	// Params is a list of parameter types of the function. The final element of this list might
	// be repeated as variable length arguments.
	Params []Type
	// VariableLengthParams is a flag to handle variable length parameters. When this flag is set to
	// true, it means that the last type of params might be specified multiple times (including zero
	// times). Setting true implies length of Params is more than 0.
	VariableLengthParams bool
	// IsConstFunc is true when the function returns a constant when all parameters are constants.
	IsConstFunc bool
}

func (sig *FuncSignature) String() string {
	ts := make([]string, 0, len(sig.Params))
	for _, p := range sig.Params {
		ts = append(ts, p.String())
	}
	elip := ""
	if sig.VariableLengthParams {
		elip = "..."
	}
	return fmt.Sprintf("%s(%s%s) -> %s", sig.Name, strings.Join(ts, ", "), elip, sig.Ret.String())
}

// BuiltinFuncSignatures is a set of all builtin function signatures. All function names are in
// lower case because function names are compared in case insensitive.
// https://docs.github.com/en/actions/learn-github-actions/expressions#functions
var BuiltinFuncSignatures = map[string][]*FuncSignature{
	"contains": {
		{
			Name: "contains",
			Ret:  BoolType(),
			Params: []Type{
				StringType(),
				StringType(),
			},
			IsConstFunc: true,
		},
		{
			Name: "contains",
			Ret:  BoolType(),
			Params: []Type{
				NewArrayType(AnyType()).Type(),
				AnyType(),
			},
			IsConstFunc: true,
		},
	},
	"startswith": {{
		Name: "startsWith",
		Ret:  BoolType(),
		Params: []Type{
			StringType(),
			StringType(),
		},
		IsConstFunc: true,
	}},
	"endswith": {{
		Name: "endsWith",
		Ret:  BoolType(),
		Params: []Type{
			StringType(),
			StringType(),
		},
		IsConstFunc: true,
	}},
	"format": {{
		Name:        "format",
		Ret:         StringType(),
		Params:      []Type{StringType()},
		IsConstFunc: true,
	}, {
		Name: "format",
		Ret:  StringType(),
		Params: []Type{
			StringType(),
			AnyType(), // variable length
		},
		VariableLengthParams: true,
		IsConstFunc:          true,
	}},
	"join": {
		{
			Name:        "join",
			Ret:         StringType(),
			Params:      []Type{StringType()},
			IsConstFunc: true,
		},
		{
			Name:        "join",
			Ret:         StringType(),
			Params:      []Type{StringType(), StringType()},
			IsConstFunc: true,
		},
		{
			Name: "join",
			Ret:  StringType(),
			Params: []Type{
				NewArrayType(StringType()).Type(),
				StringType(),
			},
			IsConstFunc: true,
		},
		// When the second parameter is omitted, values are concatenated with ','.
		{
			Name: "join",
			Ret:  StringType(),
			Params: []Type{
				NewArrayType(StringType()).Type(),
			},
			IsConstFunc: true,
		},
	},
	"tojson": {{
		Name: "toJSON",
		Ret:  StringType(),
		Params: []Type{
			AnyType(),
		},
		IsConstFunc: true,
	}},
	"fromjson": {{
		Name: "fromJSON",
		Ret:  AnyType(),
		Params: []Type{
			StringType(),
		},
		IsConstFunc: true,
	}},
	"hashfiles": {{
		Name: "hashFiles",
		Ret:  StringType(),
		Params: []Type{
			StringType(),
		},
		VariableLengthParams: true,
	}},
	"success": {{
		Name:   "success",
		Ret:    BoolType(),
		Params: []Type{},
	}},
	"always": {{
		Name:   "always",
		Ret:    BoolType(),
		Params: []Type{},
	}},
	"cancelled": {{
		Name:   "cancelled",
		Ret:    BoolType(),
		Params: []Type{},
	}},
	"failure": {{
		Name:   "failure",
		Ret:    BoolType(),
		Params: []Type{},
	}},
	"case": {{
		Name: "case",
		Ret:  AnyType(),
		Params: []Type{
			BoolType(),
			AnyType(),
			AnyType(),
		},
		VariableLengthParams: true,
		IsConstFunc:          true,
	}},
}

// Global variables

// builtinSecretProps are the secrets which GitHub Actions always provides to a workflow.
// ACTIONS_STEP_DEBUG and ACTIONS_RUNNER_DEBUG seem supplied from caller of the workflow (#130).
var builtinSecretProps = map[string]Type{
	"github_token":         StringType(),
	"actions_step_debug":   StringType(),
	"actions_runner_debug": StringType(),
}

// BuiltinGlobalVariableTypes defines types of all global variables. All context variables are
// documented at https://docs.github.com/en/actions/learn-github-actions/contexts
var BuiltinGlobalVariableTypes = map[string]Type{
	// https://docs.github.com/en/actions/reference/workflows-and-actions/contexts#github-context
	// Agents: https://docs.github.com/api/article/body?pathname=/en/actions/reference/workflows-and-actions/contexts
	"github": newNamedObjectType(map[string]Type{
		"action":                    StringType(),
		"action_path":               StringType(), // Note: Composite actions only
		"action_ref":                StringType(),
		"action_repository":         StringType(),
		"action_status":             StringType(), // Note: Composite actions only
		"actor":                     StringType(),
		"actor_id":                  StringType(),
		"api_url":                   StringType(),
		"artifact_cache_size_limit": NumberType(), // Note: Missing from the context reference
		"artifacts":                 StringType(),
		"artifacts_list":            StringType(),
		"base_ref":                  StringType(),
		"env":                       StringType(),
		"event":                     NewEmptyObjectType().Type(), // Note: Stricter type check for this payload would be possible
		"event_name":                StringType(),
		"event_path":                StringType(),
		"graphql_url":               StringType(),
		"head_ref":                  StringType(),
		"job":                       StringType(),
		"output":                    StringType(), // Note: Runner-provided file command path missing from the context reference
		"path":                      StringType(),
		"ref":                       StringType(),
		"ref_name":                  StringType(),
		"ref_protected":             BoolType(),
		"ref_type":                  StringType(),
		"repository":                StringType(),
		"repository_id":             StringType(),
		"repository_owner":          StringType(),
		"repository_owner_id":       StringType(),
		"repository_visibility":     StringType(), // Note: Missing from the context reference; documented as an OIDC claim
		"repositoryUrl":             StringType(),
		"retention_days":            StringType(),
		"run_attempt":               StringType(),
		"run_id":                    StringType(),
		"run_number":                StringType(),
		"secret_source":             StringType(),
		"server_url":                StringType(),
		"sha":                       StringType(),
		"state":                     StringType(), // Note: Runner-provided file command path missing from the context reference
		"step_summary":              StringType(), // Note: Runner-provided file command path missing from the context reference
		"token":                     StringType(),
		"triggering_actor":          StringType(),
		"workflow":                  StringType(),
		"workflow_ref":              StringType(),
		"workflow_sha":              StringType(),
		"workspace":                 StringType(),
	}).Type(),
	// https://docs.github.com/en/actions/learn-github-actions/contexts#env-context
	"env": NewMapObjectType(StringType()).Type(), // env.<env_name>
	// https://docs.github.com/en/actions/learn-github-actions/contexts#job-context
	"job": NewStrictObjectType(map[string]Type{
		"check_run_id": NumberType(),
		"container": NewStrictObjectType(map[string]Type{
			"id":      StringType(),
			"network": StringType(),
		}).Type(),
		"services": NewMapObjectType(
			NewStrictObjectType(map[string]Type{
				"id":      StringType(), // job.services.<service id>.id
				"network": StringType(),
				"ports":   NewMapObjectType(StringType()).Type(),
			}).Type(),
		).Type(),
		"status":              StringType(),
		"workflow_file_path":  StringType(), // https://docs.github.com/en/actions/reference/workflows-and-actions/contexts#job-context
		"workflow_ref":        StringType(),
		"workflow_repository": StringType(),
		"workflow_sha":        StringType(),
	}).Type(),
	// https://docs.github.com/en/actions/learn-github-actions/contexts#steps-context
	"steps": NewEmptyStrictObjectType().Type(), // This value will be updated contextually
	// https://docs.github.com/en/actions/learn-github-actions/contexts#runner-context
	"runner": NewStrictObjectType(map[string]Type{
		"name":        StringType(),
		"os":          StringType(),
		"arch":        StringType(),
		"temp":        StringType(),
		"tool_cache":  StringType(),
		"debug":       StringType(),
		"environment": StringType(), // https://github.com/github/docs/issues/32443
	}).Type(),
	// https://docs.github.com/en/actions/learn-github-actions/contexts#secrets-context
	"secrets": NewStrictObjectType(builtinSecretProps).WithMapped(StringType()).Type(),
	// https://docs.github.com/en/actions/learn-github-actions/contexts#strategy-context
	"strategy": NewObjectType(map[string]Type{
		"fail-fast":    BoolType(),
		"job-index":    NumberType(),
		"job-total":    NumberType(),
		"max-parallel": NumberType(),
	}).Type(),
	// https://docs.github.com/en/actions/learn-github-actions/contexts#matrix-context
	"matrix": NewEmptyStrictObjectType().Type(), // This value will be updated contextually
	// https://docs.github.com/en/actions/learn-github-actions/contexts#needs-context
	"needs": NewEmptyStrictObjectType().Type(), // This value will be updated contextually
	// https://docs.github.com/en/actions/learn-github-actions/contexts#inputs-context
	// https://docs.github.com/en/actions/learn-github-actions/reusing-workflows
	"inputs": NewEmptyStrictObjectType().Type(),
	// https://docs.github.com/en/actions/learn-github-actions/contexts#vars-context
	"vars": NewMapObjectType(StringType()).Type(), // vars.<var_name>
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
	funcs                 map[string][]*FuncSignature
	vars                  map[string]Type
	errs                  []*Error
	varsCopied            bool
	untrusted             *UntrustedInputChecker
	availableContexts     []string
	availableSpecialFuncs []string
	jobCondition          bool
	configVars            []string
	configSecrets         []string
	condition             bool
	checks                Checks
}

// NewChecker creates new Checker instance. When checkUntrustedInput is
// set to true, the Checker will make use of possibly untrusted inputs error. The cfg parameter is
// selected names and optional checks for this analysis. It may be nil.
func NewChecker(checkUntrustedInput bool, cfg *Options) *Checker {
	c := &Checker{
		funcs:      BuiltinFuncSignatures,
		vars:       BuiltinGlobalVariableTypes,
		varsCopied: false,
	}
	if cfg != nil {
		c.checks = cfg.Checks
		c.configVars = cfg.Variables
		c.configSecrets = cfg.Secrets
	}
	if checkUntrustedInput {
		c.untrusted = NewUntrustedInputChecker(BuiltinUntrustedInputs)
	}
	return c
}

func errorAtExpr(kind findingKind, e Node, msg string) *Error {
	span := ExpressionSpan(e)
	return &Error{
		Message: msg,
		Rule:    kind.rule, Code: kind.code,
		Offset:    span.Start,
		EndOffset: span.End,
		Line:      span.Line,
		Column:    span.Column,
	}
}

func errorfAtExpr(kind findingKind, e Node, format string, args ...any) *Error {
	return errorAtExpr(kind, e, fmt.Sprintf(format, args...))
}

func (sema *Checker) errorf(kind findingKind, e Node, format string, args ...any) {
	sema.errs = append(sema.errs, errorfAtExpr(kind, e, format, args...))
}

func errorfAtCallee(kind findingKind, n *FuncCallNode, format string, args ...any) *Error {
	return errorAtToken(kind, n.Token(), fmt.Sprintf(format, args...))
}

func (sema *Checker) calleeErrorf(kind findingKind, n *FuncCallNode, format string, args ...any) {
	sema.errs = append(sema.errs, errorfAtCallee(kind, n, format, args...))
}

func (sema *Checker) ensureVarsCopied() {
	if sema.varsCopied {
		return
	}

	// Make shallow copy of current variables map not to pollute global variable
	copied := make(map[string]Type, len(sema.vars))
	maps.Copy(copied, sema.vars)
	sema.vars = copied
	sema.varsCopied = true
}

// SetContextTypes replaces the expression's context names and types with a caller-supplied scope.
// Names are case-insensitive. The Checker copies the map.
func (sema *Checker) SetContextTypes(contexts map[string]Type) {
	sema.vars = make(map[string]Type, len(contexts))
	for name, ty := range contexts {
		sema.vars[strings.ToLower(name)] = ty
	}
	sema.varsCopied = true
}

// UpdateMatrix updates matrix object to given object type. Since matrix values change according to
// 'matrix' section of job configuration, the type needs to be updated.
func (sema *Checker) UpdateMatrix(ty ObjectType) {
	sema.ensureVarsCopied()
	sema.vars["matrix"] = ty.Type()
}

// UpdateSteps updates 'steps' context object to given object type.
func (sema *Checker) UpdateSteps(ty ObjectType) {
	sema.ensureVarsCopied()
	sema.vars["steps"] = ty.Type()
}

// UpdateNeeds updates 'needs' context object to given object type.
func (sema *Checker) UpdateNeeds(ty ObjectType) {
	sema.ensureVarsCopied()
	sema.vars["needs"] = ty.Type()
}

// UpdateSecrets updates 'secrets' context object to given object type. The strictness of the
// given type is preserved, so an open type keeps allowing unknown secret names.
func (sema *Checker) UpdateSecrets(ty ObjectType) {
	sema.ensureVarsCopied()

	// Merges automatically supplied secrets with manually defined secrets.
	declared := ty.rep()
	props := maps.Clone(builtinSecretProps)
	maps.Copy(props, declared.props)
	sema.vars["secrets"] = ObjectType{&objectData{props: props, mapped: declared.mapped, names: maps.Clone(declared.names)}}.Type()
}

// UpdateInputs updates 'inputs' context object to given object type.
func (sema *Checker) UpdateInputs(ty ObjectType) {
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
func (sema *Checker) UpdateDispatchInputs(ty ObjectType) {
	sema.UpdateInputs(ty)

	// Update `github.event.inputs`.
	// Unlike `inputs.*`, type of `github.event.inputs.*` is always String unlike `inputs.*`. We need
	// to create a new type from `ty` (e.g. {foo: boolean, bar: number} -> {foo: String, bar: String})

	declared := ty.rep()
	p := make(map[string]Type, len(declared.props))
	for n := range declared.props {
		p[n] = StringType()
	}
	inputs := ObjectType{&objectData{props: p, names: maps.Clone(declared.names)}}.Type()

	gh, ok := sema.vars["github"].Object()
	if !ok {
		return
	}
	ev, ok := gh.rep().props["event"].Object()
	if !ok {
		return
	}
	// Payload variants for workflow_dispatch receive the same inputs, so
	// narrowing github.event by event name keeps them.
	variants := slices.Clone(ev.rep().variants)
	for i, variant := range variants {
		if variant.Event == "workflow_dispatch" {
			variants[i].Payload = variant.Payload.With("inputs", inputs)
		}
	}
	event := ev.rep()
	props := maps.Clone(event.props)
	if props == nil {
		props = map[string]Type{}
	}
	props["inputs"] = inputs
	updated := ObjectType{&objectData{displayName: event.displayName, props: props, names: event.names, variants: variants, mapped: event.mapped}}
	sema.vars["github"] = gh.With("event", updated.Type()).Type()
}

// UpdateJobs updates 'jobs' context object to given object type.
func (sema *Checker) UpdateJobs(ty ObjectType) {
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

func (sema *Checker) checkAvailableContext(n *VariableNode) {
	ctx := strings.ToLower(n.Name)
	if slices.Contains(sema.availableContexts, ctx) {
		return
	}

	var notes string
	switch len(sema.availableContexts) {
	case 0:
		notes = "no context is available here"
	case 1:
		notes = "available context is " + stringutil.Quotes(sema.availableContexts)
	default:
		notes = "available contexts are " + stringutil.Quotes(sema.availableContexts)
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

func (sema *Checker) checkSpecialFunctionAvailability(n *FuncCallNode) {
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
		stringutil.Quotes(allowed),
	)
}

func (sema *Checker) visitUntrustedCheckerOnEnterNode(n Node) {
	if sema.untrusted != nil {
		sema.untrusted.OnVisitNodeEnter(n)
	}
}

func (sema *Checker) visitUntrustedCheckerOnLeaveNode(n Node) {
	if sema.untrusted != nil {
		sema.untrusted.OnVisitNodeLeave(n)
	}
}

func (sema *Checker) checkVariable(n *VariableNode) Type {
	v, ok := sema.vars[n.Name]
	if !ok {
		if sigs, known := sema.funcs[n.Name]; known && len(sigs) > 0 {
			name := n.Token().Value
			for _, sig := range sigs {
				if len(sig.Params) == 0 {
					sema.errorf(missingCall, n, "function %q must be called with parentheses; did you mean %q?", name, name+"()")
					return AnyType()
				}
			}
			sema.errorf(missingCall, n, "function %q must be called with parentheses and arguments", name)
			return AnyType()
		}
		if inputs, ok := sema.vars["inputs"].Object(); ok && inputs.IsStrict() && slices.Contains(sema.availableContexts, "inputs") {
			if _, declared := inputs.Property(n.Name); declared {
				sema.errorf(unknownVariable, n, "undefined variable %q; did you mean %q?", n.Token().Value, "inputs."+n.Name)
				return AnyType()
			}
		}
		if suggestion := sema.contextPropertySuggestion(n.Name); suggestion != "" {
			sema.errorf(unknownVariable, n, "undefined variable %q; did you mean %q?", n.Token().Value, suggestion)
			return AnyType()
		}
		available := make([]string, 0, len(sema.availableContexts))
		for _, name := range sema.availableContexts {
			if _, exists := sema.vars[name]; exists {
				available = append(available, name)
			}
		}
		if suggestion := CloseName(n.Name, available); suggestion != "" {
			sema.errorf(unknownVariable, n, "undefined variable %q; did you mean %q?", n.Token().Value, suggestion)
			return AnyType()
		}
		ss := make([]string, 0, len(sema.vars))
		for n := range sema.vars {
			ss = append(ss, n)
		}
		sema.errorf(unknownVariable, n, "undefined variable %q. available variables are %s", n.Token().Value, stringutil.SortedQuotes(ss))
		return AnyType()
	}

	sema.checkAvailableContext(n)
	return v
}

func (sema *Checker) checkObjectDeref(n *ObjectDerefNode) Type {
	ty := sema.check(n.Receiver)
	switch ty.Kind() {
	case KindAny:
		return AnyType()
	case KindObject:
		object, _ := ty.Object()
		if t, ok := object.Property(n.Property); ok {
			return t
		}
		if mapped, ok := object.Mapped(); ok {
			if v, ok := n.Receiver.(*VariableNode); ok {
				switch v.Name {
				case "vars":
					sema.checkConfigVariables(n, n.Property)
				case "secrets":
					sema.checkConfigSecrets(n, n.Property)
				}
			}
			return mapped
		}
		if receiver, ok := n.Receiver.(*VariableNode); ok {
			// User-defined properties can resemble context names without referring
			// to them, such as inputs.github_token or matrix.runner_os.
			switch receiver.Name {
			case "github", "job", "runner", "strategy":
				if suggestion := sema.contextPropertySuggestion(n.Property); suggestion != "" {
					sema.errorf(unknownProperty, n, "property %q is not defined in this context; did you mean %q?", n.PropertyName(), suggestion)
					return AnyType()
				}
			}
		}
		sema.missingProperty(n, n.Receiver, n.PropertyName(), object)
		return AnyType()
	case KindArray:
		array, _ := ty.Array()
		if !array.Deref() {
			sema.errorf(operandType, n, "receiver of object dereference %q must be type of object but got %q", n.Property, ty.String())
			return AnyType()
		}
		elem := array.Elem()
		if elem.Kind() == KindAny {
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
			return AnyType()
		}
		// Map element type of delererenced array
		mappedElem := AnyType()
		if t, ok := et.Property(n.Property); ok {
			mappedElem = t
		} else if mapped, ok := et.Mapped(); ok {
			mappedElem = mapped
		} else {
			sema.missingProperty(n, n.Receiver, n.PropertyName(), et)
		}
		return newFilteredArrayType(mappedElem).Type()
	case KindNull, KindNumber, KindBool, KindString:
	}
	sema.errorf(operandType, n, "receiver of object dereference %q must be type of object but got %q", n.Property, ty.String())
	return AnyType()
}

// Suggest only explicit properties in contexts available at this expression.
// Matching the context prefix preserves distinctions such as job.workflow_sha
// (the called workflow) versus github.workflow_sha (the caller).
func (sema *Checker) contextPropertySuggestion(name string) string {
	return ContextPropertySuggestion(name, sema.availableContexts, sema.vars)
}

func ContextPropertySuggestion(name string, contexts []string, variables map[string]Type) string {
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

func (sema *Checker) checkConfigVariables(n Node, prop string) {
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
		stringutil.SortedQuotes(sema.configVars),
	)
}

func (sema *Checker) checkConfigSecrets(n Node, prop string) {
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
		stringutil.SortedQuotes(sema.configSecrets),
	)
}

func (sema *Checker) checkArrayDeref(n *ArrayDerefNode) Type {
	ty := sema.check(n.Receiver)
	switch ty.Kind() {
	case KindAny:
		return newFilteredArrayType(AnyType()).Type()
	case KindArray:
		array, _ := ty.Array()
		return newFilteredArrayType(array.Elem()).Type()
	case KindObject:
		// Object filtering is available for objects, not only arrays (#66)
		object, _ := ty.Object()

		if mapped, ok := object.Mapped(); ok {
			// For map object or loose object at receiver of .*
			switch mapped.Kind() {
			case KindAny, KindObject:
				return newFilteredArrayType(mapped).Type()
			case KindNull, KindNumber, KindBool, KindString, KindArray:
			}
			sema.errorf(operandType, n, "elements of object at receiver of object filtering `.*` must be type of object but got %q. the type of receiver was %q", mapped.String(), ty.String())
			return AnyType()
		}

		// Payload object filters select scalar members as well as object members.
		if path := strings.ToLower(eventReference(n.Receiver)); path == "github.event" || strings.HasPrefix(path, "github.event.") || strings.HasPrefix(path, "github.event[") {
			return newFilteredArrayType(AnyType()).Type()
		}
		// For strict object at receiver of .*
		found := false
		for _, t := range object.Properties() {
			if t.Kind() == KindObject {
				found = true
				break
			}
		}
		if !found {
			sema.errorf(operandType, n, "object type %q cannot be filtered by object filtering `.*` since it has no object element", ty.String())
			return AnyType()
		}

		return newFilteredArrayType(AnyType()).Type()
	case KindNull, KindNumber, KindBool, KindString:
	}
	sema.errorf(operandType, n, "receiver of object filtering `.*` must be type of array or object but got %q", ty.String())
	return AnyType()
}

func (sema *Checker) checkIndexAccess(n *IndexAccessNode) Type {
	// Note: Index must be visited before Index to make UntrustedInputChecker work correctly even if
	// the expression has some nest like foo[aaa.bbb].bar. Nest happens in top-down order and
	// properties/indices access Check is done in bottom-up order. So, as far as we visit nested
	// index nodes before visiting operand, the index is recursively checked first.
	idx := sema.check(n.Index)

	ty := sema.check(n.Operand)
	switch ty.Kind() {
	case KindAny:
		return AnyType()
	case KindArray:
		array, _ := ty.Array()
		if k := idx.Kind(); k == KindAny || k == KindNumber {
			return array.Elem()
		}
		sema.errorf(operandType, n.Index, "index access of array must be type of number but got %q", idx.String())
		return AnyType()
	case KindObject:
		object, _ := ty.Object()
		switch idx.Kind() {
		case KindAny:
			return AnyType()
		case KindString:
			// Index access with String literal like foo['bar']
			if lit, ok := n.Index.(*StringNode); ok {
				if prop, ok := object.Property(strings.ToLower(lit.Value)); ok {
					return prop
				}
				if mapped, ok := object.Mapped(); ok {
					if v, ok := n.Operand.(*VariableNode); ok {
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
			return AnyType() // Fallback
		case KindNull, KindNumber, KindBool, KindObject, KindArray:
		}
		sema.errorf(operandType, n.Index, "property access of object must be type of string but got %q", idx.String())
		return AnyType()
	case KindNull, KindNumber, KindBool, KindString:
	}
	sema.errorf(operandType, n, "index access operand must be type of object or array but got %q", ty.String())
	return AnyType()
}

func checkFuncSignature(n *FuncCallNode, sig *FuncSignature, args []Type) *Error {
	lp, la := len(sig.Params), len(args)
	if sig.VariableLengthParams && (lp > la) || !sig.VariableLengthParams && lp != la {
		atLeast := ""
		if sig.VariableLengthParams {
			atLeast = "at least "
		}
		return errorfAtCallee(argumentCount,
			n,
			"number of arguments is wrong. function %q takes %s%d parameters but %d arguments are given",
			sig.String(),
			atLeast,
			lp,
			la,
		)
	}

	for i := range len(sig.Params) {
		p, a := sig.Params[i], args[i]
		if !p.Assignable(a) {
			return errorfAtExpr(argumentType,
				n.Args[i],
				"%s argument of function call is not assignable. %q cannot be assigned to %q. called function type is %q",
				ordinal(i+1),
				a.String(),
				p.String(),
				sig.String(),
			)
		}
	}

	// A variadic signature requires its listed parameters; optional tails are
	// represented by a separate overload, as with format(String).
	if sig.VariableLengthParams {
		rest := args[lp:]
		p := sig.Params[lp-1]
		for i, a := range rest {
			if !p.Assignable(a) {
				return errorfAtExpr(argumentType,
					n.Args[lp+i],
					"%s argument of function call is not assignable. %q cannot be assigned to %q. called function type is %q",
					ordinal(lp+i+1),
					a.String(),
					p.String(),
					sig.String(),
				)
			}
		}
	}

	return nil
}

func (sema *Checker) checkBuiltinFuncCall(n *FuncCallNode, sig *FuncSignature) Type {
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
		lit, ok := n.Args[0].(*StringNode)
		if !ok {
			return sig.Ret
		}
		l := len(n.Args) - 1 // -1 means removing first format string argument

		holders := parseFormatFuncSpecifiers(lit.Value, l)

		for i := range l {
			if _, ok := holders[i]; !ok {
				sema.calleeErrorf(formatPlaceholder, n, "format string %q does not contain placeholder {%d}. remove argument which is unused in the format string", lit.Value, i)
				continue
			}
			delete(holders, i) // forget it to check unused placeholders
		}

		for i := range holders {
			sema.calleeErrorf(formatPlaceholder, n, "format string %q contains placeholder {%d} but only %d arguments are given to format", lit.Value, i, l)
		}
	case "fromjson":
		lit, ok := n.Args[0].(*StringNode)
		if !ok {
			return sig.Ret
		}
		collisions := JSONMemberCollisions(lit.Value)
		for _, collision := range collisions {
			sema.errorf(jsonCollision, lit, "fromJSON() object members %q and %q collide under case-insensitive lookup at JSON offset %d; the selected value depends on JSON parsing mode", collision.First, collision.Second, collision.Offset)
		}
		if len(collisions) > 0 {
			return sig.Ret
		}
		var v any
		err := json.Unmarshal([]byte(lit.Value), &v)
		if err == nil {
			return TypeOfJSONValue(v)
		}
		if s, ok := errors.AsType[*json.SyntaxError](err); ok {
			sema.errorf(invalidJSON, lit, "broken JSON string is passed to fromJSON() at offset %d: %s", s.Offset, s)
		}
	case "case":
		if len(n.Args)%2 == 0 {
			sema.calleeErrorf(argumentCount, n, "case() requires an odd number of arguments (pred/value pairs + default) but got %d", len(n.Args))
		}
	}

	return sig.Ret
}

func (sema *Checker) checkExpressionBranch(node Node, skipped bool) Type {
	previous := sema.skipEvaluation
	sema.skipEvaluation = previous || skipped
	ty := sema.check(node)
	sema.skipEvaluation = previous
	return ty
}

func (sema *Checker) checkCaseCall(n *FuncCallNode) Type {
	sema.checkSpecialFunctionAvailability(n)
	ty, _ := sema.checkCaseBranches(n.Args, true)
	return ty
}

// checkCaseBranches checks one predicate/value pair and recurses into the rest.
// The result type merges every reachable value branch with the final default.
// Predicates never contribute. Branches made unreachable by constant predicates
// are still checked but skip evaluation, so their types are left out of the
// merge. The second result reports whether any branch in args was reachable.
func (sema *Checker) checkCaseBranches(args []Node, remaining bool) (Type, bool) {
	if len(args) == 1 {
		return sema.checkExpressionBranch(args[0], !remaining), remaining
	}
	ty := sema.checkExpressionBranch(args[0], !remaining)
	if k := ty.Kind(); remaining && !sema.skipEvaluation && k != KindBool && k != KindAny {
		sema.errorf(operandType, args[0], "case() predicate must be boolean but found %s", ty.String())
	}
	value, known := ConditionConstantValue(args[0])
	predicate, boolean := value.(bool)
	valueReachable := remaining && (!known || !boolean || predicate)
	valueType := sema.withEventGuard(args[0], true, func() Type {
		return sema.checkExpressionBranch(args[1], !valueReachable)
	})
	if known && boolean && predicate {
		remaining = false
	}
	// Later predicates and the default are reachable only when this predicate
	// is false. Keep those guards nested so all earlier exclusions apply.
	var restType Type
	var restReachable bool
	sema.withEventGuard(args[0], false, func() Type {
		restType, restReachable = sema.checkCaseBranches(args[2:], remaining)
		return restType
	})
	switch {
	case valueReachable && restReachable:
		return valueType.Merge(restType), true
	case valueReachable:
		return valueType, true
	default:
		return restType, restReachable
	}
}

func (sema *Checker) checkFuncCall(n *FuncCallNode) Type {
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
			return AnyType()
		}
		ss := make([]string, 0, len(sema.funcs))
		for n := range sema.funcs {
			ss = append(ss, n)
		}
		sema.calleeErrorf(unknownFunction, n, "undefined function %q. available functions are %s", n.Callee, stringutil.SortedQuotes(ss))
		return AnyType()
	}

	// The runner's expression parser limits these variadic functions to 255 arguments.
	// https://github.com/actions/runner/blob/759385a3510197a58b5c08dc1f373b74b9f4643b/src/Sdk/DTExpressions2/Expressions2/ExpressionConstants.cs
	if (callee == "hashfiles" || callee == "format" || callee == "case") && len(n.Args) > 255 {
		sema.calleeErrorf(argumentCount, n, "function %q takes at most 255 arguments but %d arguments are given", n.Callee, len(n.Args))
	}
	if sema.jobCondition && (callee == "success" || callee == "failure") && len(n.Args) > 0 {
		sigs = []*FuncSignature{{
			Name:                 callee,
			Ret:                  BoolType(),
			Params:               []Type{StringType()},
			VariableLengthParams: true,
		}}
	}

	if callee == "case" && len(n.Args) >= 3 && len(n.Args)%2 == 1 {
		return sema.checkCaseCall(n)
	}
	tys := make([]Type, 0, len(n.Args))
	for _, a := range n.Args {
		tys = append(tys, sema.check(a))
	}

	// Check all overloads
	errs := []*Error{}
	for _, sig := range sigs {
		err := checkFuncSignature(n, sig, tys)
		if err == nil {
			// When one of overload pass type Check, overload was resolved correctly
			return sema.checkBuiltinFuncCall(n, sig)
		}
		errs = append(errs, err)
	}

	// All candidates failed
	sema.errs = append(sema.errs, errs...)

	return AnyType()
}

func (sema *Checker) checkNotOp(n *NotOpNode) Type {
	ty := sema.check(n.Operand)
	if !(BoolType()).Assignable(ty) {
		sema.errorf(operandType, n, "type of operand of ! operator %q is not assignable to type \"bool\"", ty.String())
	}
	return BoolType()
}

func validateCompareOpOperands(op CompareOpNodeKind, l, r Type) bool {
	// Comparison behavior: https://docs.github.com/en/actions/learn-github-actions/expressions#operators
	switch op {
	case CompareOpNodeKindEq, CompareOpNodeKindNotEq:
		switch l.Kind() {
		case KindAny, KindNull:
			return true
		case KindNumber, KindBool, KindString:
			// Objects and arrays are coerced to NaN hence the comparison result is always false
			return r.Kind() != KindObject && r.Kind() != KindArray
		case KindObject:
			k := r.Kind()
			return k == KindObject || k == KindNull || k == KindAny
		case KindArray:
			if la, ok := l.Array(); ok {
				if ra, ok := r.Array(); ok {
					return validateCompareOpOperands(op, la.Elem(), ra.Elem())
				}
			}
			k := r.Kind()
			return k == KindNull || k == KindAny
		}
		panic("unreachable")
	case CompareOpNodeKindLess, CompareOpNodeKindLessEq, CompareOpNodeKindGreater, CompareOpNodeKindGreaterEq:
		// null, bool, array, and object cannot be compared with these operators
		switch l.Kind() {
		case KindAny, KindNumber, KindString:
			k := r.Kind()
			return k != KindNull && k != KindBool && k != KindObject && k != KindArray
		case KindNull, KindBool, KindObject, KindArray:
			return false
		}
		panic("unreachable")
	case CompareOpNodeKindInvalid:
		// Invalid operators are not rejected here.
		fallthrough
	default:
		return true
	}
}

func (sema *Checker) checkCompareOp(n *CompareOpNode) Type {
	l := sema.check(n.Left)
	r := sema.check(n.Right)
	sema.checkConditionComparison(n, l, r)

	if !validateCompareOpOperands(n.Kind, l, r) {
		sema.errorf(operandType, n, "%q value cannot be compared to %q value with %q operator", l.String(), r.String(), n.Kind.String())
	}

	return BoolType()
}

// checkWithNarrowing checks type of given expression with type narrowing. Type narrowing narrows
// down the type of the expression by assuming its value. For example, `l && r` is typed as
// `typeof(l) | typeof(r)` usually. However when the expression is assumed to be true, its type can
// be narrowed down to `typeof(r)`.
// This analysis is useful to make type checking more accurate. For example, `some_var && 60 || 20`
// can be typed as `number` instead of `typeof(some_var) | number`. (#384)
func (sema *Checker) checkWithNarrowing(n Node, isTruthy bool) Type {
	switch n := n.(type) {
	case *LogicalOpNode:
		switch n.Kind {
		case LogicalOpNodeKindAnd:
			// When `l && r` is true, narrow its type to `typeof(r)`
			if isTruthy {
				sema.check(n.Left)
				return sema.withEventGuard(n.Left, true, func() Type { return sema.check(n.Right) })
			}
		case LogicalOpNodeKindOr:
			// When `l || r` is false, narrow its type to `typeof(r)`
			if !isTruthy {
				sema.check(n.Left)
				return sema.withEventGuard(n.Left, false, func() Type { return sema.check(n.Right) })
			}
		case LogicalOpNodeKindInvalid:
			// Invalid operators are not narrowed.
		}
		return sema.checkLogicalOp(n)
	case *NotOpNode:
		return sema.checkWithNarrowing(n.Operand, !isTruthy)
	default:
		return sema.check(n)
	}
}

func (sema *Checker) checkLogicalOp(n *LogicalOpNode) Type {
	switch n.Kind {
	case LogicalOpNodeKindAnd:
		// When `l` is false in `l && r`, its type is `typeof(l)`. Otherwise `typeof(r)`.
		// Narrow the type of LHS expression by assuming its value is falsy.
		left := sema.checkWithNarrowing(n.Left, false)
		value, known := ConditionConstantValue(n.Left)
		right := sema.withEventGuard(n.Left, true, func() Type { return sema.checkExpressionBranch(n.Right, known && !ExpressionTruthy(value)) })
		return left.Merge(right)
	case LogicalOpNodeKindOr:
		// When `l` is true in `l || r`, its type is `typeof(l)`. Otherwise `typeof(r).
		// Narrow the type of LHS expression by assuming its value is truthy.
		left := sema.checkWithNarrowing(n.Left, true)
		value, known := ConditionConstantValue(n.Left)
		right := sema.withEventGuard(n.Left, false, func() Type { return sema.checkExpressionBranch(n.Right, known && ExpressionTruthy(value)) })
		return left.Merge(right)
	case LogicalOpNodeKindInvalid:
		// Invalid operators still check both operands.
		fallthrough
	default:
		sema.check(n.Left)
		sema.check(n.Right)
		return AnyType()
	}
}

func (sema *Checker) check(expr Node) Type {
	sema.visitUntrustedCheckerOnEnterNode(expr)
	defer sema.visitUntrustedCheckerOnLeaveNode(expr) // Call this method in bottom-up order

	switch e := expr.(type) {
	case *VariableNode:
		return sema.checkVariable(e)
	case *NullNode:
		return NullType()
	case *BoolNode:
		return BoolType()
	case *StringNode:
		return StringType()
	case *IntNode, *FloatNode:
		return NumberType()
	case *ObjectDerefNode:
		return sema.checkObjectDeref(e)
	case *ArrayDerefNode:
		return sema.checkArrayDeref(e)
	case *IndexAccessNode:
		return sema.checkIndexAccess(e)
	case *FuncCallNode:
		return sema.checkFuncCall(e)
	case *NotOpNode:
		return sema.checkNotOp(e)
	case *CompareOpNode:
		return sema.checkCompareOp(e)
	case *LogicalOpNode:
		return sema.checkLogicalOp(e)
	default:
		panic("unreachable")
	}
}

// Check checks semantics of given expression syntax tree. It returns the type of the expression as
// the first return value when the Check was successfully done. And it returns all errors found
// while checking the expression as the second return value.
func (sema *Checker) Check(expr Node) (Type, []*Error) {
	sema.errs = []*Error{}
	if sema.untrusted != nil {
		sema.untrusted.Init()
	}
	ty := sema.check(expr)
	if sema.condition && sema.checks.StringConditions && conditionStringReference(expr, ty) {
		sema.ruleError(expr, "string-conditions", "bare string condition is truthy for every non-empty value, including 'false' and '0'; use an explicit string comparison or fromJSON() for a boolean contract")
	}
	errs := detachErrors(sema.errs)
	if sema.untrusted != nil {
		sema.untrusted.OnVisitEnd()
		errs = append(errs, sema.untrusted.Errs()...)
		for _, finding := range CheckSecretInterpolations(expr) {
			// Invalid selectors do not prove that a secret value is emitted.
			// Errors in a separate expression branch must not hide valid ones.
			invalid := slices.ContainsFunc(sema.errs, func(err *Error) bool {
				return err.Offset < finding.EndOffset && finding.Offset < err.EndOffset
			})
			if !invalid {
				errs = append(errs, finding)
			}
		}
	}
	return ty, errs
}

// IsConstant returns the given expression is a constant. For example the following expressions
// are constants.
//   - 1 == 2
//   - (!true && 'foo') || 'bar'
//   - startsWith('foobar', 'foo')
//   - format('{} + {} = {}', 1, 2, 3)
func (sema *Checker) IsConstant(expr Node) bool {
	switch e := expr.(type) {
	case *NullNode, *BoolNode, *IntNode, *FloatNode, *StringNode:
		return true
	case *VariableNode, *ObjectDerefNode, *ArrayDerefNode, *IndexAccessNode:
		return false
	case *NotOpNode:
		return sema.IsConstant(e.Operand)
	case *CompareOpNode:
		return sema.IsConstant(e.Left) && sema.IsConstant(e.Right)
	case *LogicalOpNode:
		return sema.IsConstant(e.Left) && sema.IsConstant(e.Right)
	case *FuncCallNode:
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
