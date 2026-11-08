package action

import (
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"

	identifier "actionlint.kjanat.dev/v2/internal/checking/rules/correctness/id"
	"actionlint.kjanat.dev/v2/internal/languages/yaml/syntax"

	actionmeta "actionlint.kjanat.dev/v2/internal/model/actions"

	value "actionlint.kjanat.dev/v2/internal/model/contexts/values"

	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"

	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
	"actionlint.kjanat.dev/v2/internal/results"

	findingmodel "actionlint.kjanat.dev/v2/internal/results/finding"
	"actionlint.kjanat.dev/v2/internal/results/messages"
)

type validator struct {
	standalone *actionmeta.Metadata
	errs       []*findingmodel.Finding
	advisories bool
	files      actionmeta.ReferencedFiles
}

// CheckWithAdvisories includes optional expression findings for host policy.
func CheckWithAdvisories(meta *actionmeta.Metadata, caller *workflowlang.Pos, files actionmeta.ReferencedFiles) []*findingmodel.Finding {
	rule := validator{advisories: true, files: files}
	rule.checkLocalActionMetadata(meta, caller)
	return rule.errs
}

// StandaloneWithAdvisories includes optional expression findings for host policy.
func StandaloneWithAdvisories(meta *actionmeta.Metadata, files actionmeta.ReferencedFiles) []*findingmodel.Finding {
	rule := validator{files: files, standalone: meta, advisories: true}
	rule.checkLocalActionMetadata(meta, &workflowlang.Pos{Line: 1, Col: 1})
	return rule.errs
}

func (rule *validator) expressionFindings(s string, bare bool, field string, inputs actionmeta.Inputs) []actionmeta.ExpressionProblem {
	read := actionmeta.ReadExpressions(s, bare, field, inputs)
	return append(read.Problems, expressionSemanticFindings(read.Expressions, field, rule.advisories)...)
}

func (rule *validator) Error(pos *workflowlang.Pos, code actionmeta.ProblemCode, message string) {
	ruleName, diagnosticCode := problemRule(code)
	finding := findingmodel.At(pos, ruleName, message)
	finding.Code = diagnosticCode
	rule.errs = append(rule.errs, finding)
}

func (rule *validator) Errorf(pos *workflowlang.Pos, code actionmeta.ProblemCode, format string, args ...any) {
	rule.Error(pos, code, fmt.Sprintf(format, args...))
}

// Check validates a manifest reached from a workflow call site. Findings which
// cannot point inside the manifest point to the caller's uses position.
func Check(meta *actionmeta.Metadata, caller *workflowlang.Pos, files actionmeta.ReferencedFiles) []*findingmodel.Finding {
	rule := validator{files: files}
	rule.checkLocalActionMetadata(meta, caller)
	return rule.errs
}

// Standalone validates a manifest's declarations at their own source positions.
func Standalone(meta *actionmeta.Metadata, files actionmeta.ReferencedFiles) []*findingmodel.Finding {
	rule := validator{files: files, standalone: meta}
	rule.checkLocalActionMetadata(meta, &workflowlang.Pos{Line: 1, Col: 1})
	return rule.errs
}

// Inputs validates supplied and required inputs against resolved action metadata.
func Inputs(meta *actionmeta.Metadata, exec workflowdecl.ExecActionView, describe func(*actionmeta.Metadata) string) []*findingmodel.Finding {
	rule := validator{}
	rule.checkAction(actionmeta.NewDefinition(meta), exec, describe(meta))
	return rule.errs
}

func (rule *validator) missingRunsProp(pos *workflowlang.Pos, prop, ty, action, path string) {
	pos = rule.metadataFieldPosition(pos, "runs", prop)
	rule.Errorf(pos, actionmeta.CodeMissingField, `%q is required in "runs" section because %q is a %s action. the action is defined at %q`, prop, action, ty, path)
}

func (rule *validator) checkInvalidRunsProps(pos *workflowlang.Pos, r *actionmeta.Runs, ty, action, path string, props []string) {
	for _, prop := range props {
		invalid := prop == "main" && r.Main != "" ||
			prop == "pre" && r.Pre != "" ||
			prop == "pre-if" && r.PreIf != "" ||
			prop == "post" && r.Post != "" ||
			prop == "post-if" && r.PostIf != "" ||
			prop == "steps" && len(r.Steps) > 0 ||
			prop == "image" && r.Image != "" ||
			prop == "pre-entrypoint" && r.PreEntrypoint != "" ||
			prop == "entrypoint" && r.Entrypoint != "" ||
			prop == "post-entrypoint" && r.PostEntrypoint != "" ||
			prop == "args" && r.Args != nil ||
			prop == "env" && r.Env != nil

		if invalid {
			pos := rule.metadataFieldPosition(pos, "runs", prop)
			rule.Errorf(pos, actionmeta.CodeUnexpectedField, `%q is not allowed in "runs" section because %q is a %s action. the action is defined at %q`, prop, action, ty, path)
		}
	}
}

func (rule *validator) checkRunsFileExists(file, dir, prop, name string, pos *workflowlang.Pos) {
	pos = rule.metadataFieldPosition(pos, "runs", prop)
	f := filepath.FromSlash(file)
	if f == "" {
		return
	}
	if rule.files.Lookup(file) == actionmeta.FileMissing {
		rule.Errorf(pos, actionmeta.CodeMissingFile, `file %q does not exist in %q. it is specified at %q key in "runs" section in %q action`, f, dir, prop, name)
	}
}

// https://docs.github.com/en/actions/creating-actions/metadata-syntax-for-github-actions#runs-for-docker-container-actions
// Agents: https://docs.github.com/api/article/body?pathname=/en/actions/reference/workflows-and-actions/metadata-syntax
func (rule *validator) checkLocalDockerActionRuns(r *actionmeta.Runs, dir, name string, pos *workflowlang.Pos) {
	if r.Image == "" {
		rule.missingRunsProp(pos, "image", "Docker", name, dir)
	} else if !actionmeta.ImageOnRegistry(r.Image) {
		rule.checkRunsFileExists(r.Image, dir, "image", name, pos)
		if filepath.Base(filepath.FromSlash(r.Image)) != "Dockerfile" {
			pos := rule.metadataFieldPosition(pos, "runs", "image")
			rule.Errorf(pos, actionmeta.CodeInvalidDockerfile, `the local file %q referenced from "image" key must be named "Dockerfile" in %q action. the action is defined at %q`, r.Image, name, dir)
		}
	}
	// Entrypoints resolve inside the container image; only a local Dockerfile can be checked here.
	rule.checkInvalidRunsProps(pos, r, "Docker", name, dir, []string{"main", "pre", "post", "steps"})
}

// https://docs.github.com/en/actions/creating-actions/metadata-syntax-for-github-actions#runs-for-composite-actions
// Agents: https://docs.github.com/api/article/body?pathname=/en/actions/reference/workflows-and-actions/metadata-syntax
func (rule *validator) checkLocalCompositeActionRuns(meta *actionmeta.Metadata, pos *workflowlang.Pos) {
	r := &meta.Runs
	if r.Steps == nil {
		rule.missingRunsProp(pos, "steps", "Composite", meta.Name, meta.Dir())
	}
	ids := map[string]bool{}
	for i, s := range r.Steps {
		if s.ID != nil && s.ID.Value != "" {
			id := s.ID.Value
			switch {
			case !identifier.Valid(id) || len(id) >= 100 || strings.HasPrefix(id, "__"):
				rule.compositeStepErrorfAt(meta, i, s.ID.Line, s.ID.Column, nil, actionmeta.CodeInvalidStepID, "has invalid ID %q; IDs must start with a letter or _, contain only alphanumeric characters, - or _, be shorter than 100 characters, and must not start with __", id)
			case ids[strings.ToLower(id)]:
				rule.compositeStepErrorfAt(meta, i, s.ID.Line, s.ID.Column, nil, actionmeta.CodeDuplicateStepID, "has duplicate ID %q; step IDs are case insensitive", id)
			}
			ids[strings.ToLower(id)] = true
		}
		rule.checkCompositeActionStep(meta, s, i)
	}
	rule.checkInvalidRunsProps(pos, r, "Composite", meta.Name, meta.Dir(), []string{"main", "pre", "pre-if", "post", "post-if", "image", "pre-entrypoint", "entrypoint", "post-entrypoint", "args", "env"})
}

// compositeStepErrorf reports an error at the step's own position in the action metadata file
// instead of at the "uses" site in the workflow being linted.
func (rule *validator) compositeStepErrorf(meta *actionmeta.Metadata, s *actionmeta.Step, idx int, code actionmeta.ProblemCode, format string, args ...any) {
	rule.compositeStepErrorfAt(meta, idx, s.Line, s.Column, nil, code, format, args...)
}

// compositeStepErrorfAt reports a position inside the step, with an optional exclusive end.
func (rule *validator) compositeStepErrorfAt(meta *actionmeta.Metadata, idx, line, col int, end *workflowlang.Pos, code actionmeta.ProblemCode, format string, args ...any) {
	m := fmt.Sprintf(format, args...)
	ruleName, diagnosticCode := problemRule(code)
	err := findingmodel.At(&workflowlang.Pos{Line: line, Col: col}, ruleName, fmt.Sprintf(`"runs.steps[%d]" in metadata of %q action %s`, idx, meta.Name, m))
	err.Path = meta.Path()
	err.Code = diagnosticCode
	err.Source = meta.Source
	err.EndPosition = end
	if idx >= 0 && idx < len(meta.Runs.Steps) {
		step := meta.Runs.Steps[idx]
		origin := &findingmodel.IgnoreOrigin{}
		if step.ID != nil {
			origin.StepID = step.ID.Value
		}
		if step.StepName != nil {
			origin.StepName = step.StepName.Value
		}
		if step.Uses != nil {
			origin.Uses = *step.Uses
		}
		for _, value := range []*string{&origin.StepID, &origin.StepName, &origin.Uses} {
			if literal := exprsyntax.LiteralExpressionValue(*value); literal != nil {
				*value = *literal
			}
		}
		err.IgnoreOrigin = origin
	}
	rule.errs = append(rule.errs, err)
}

// metadataErrorfAt reports inside the action metadata, with an optional exclusive end.
func (rule *validator) metadataErrorfAt(meta *actionmeta.Metadata, line, col int, end *workflowlang.Pos, code actionmeta.ProblemCode, format string, args ...any) {
	ruleName, diagnosticCode := problemRule(code)
	err := findingmodel.Errorf(&workflowlang.Pos{Line: line, Col: col}, ruleName, format, args...)
	err.Path = meta.Path()
	err.Code = diagnosticCode
	err.Source = meta.Source
	err.EndPosition = end
	err.IgnoreOrigin = &findingmodel.IgnoreOrigin{Metadata: true}
	rule.errs = append(rule.errs, err)
}

func (rule *validator) checkActionInputDefaults(meta *actionmeta.Metadata) {
	const field = "inputs.*.default"
	for _, kv := range meta.InputDefaults {
		for _, v := range rule.expressionFindings(kv.Value.Value, false, field, meta.Inputs) {
			pos := actionmeta.ExpressionPosition(meta, &kv.Value, v.Offset)
			message := v.Message
			if v.Context != "" {
				message = fmt.Sprintf(
					`uses context %q which is not available in input defaults. available contexts are %s`,
					v.Context, messages.Quotes(actionmeta.AllowedContexts(field)),
				)
			}
			rule.metadataErrorfAt(
				meta, pos.Line, pos.Col, actionmeta.ExpressionEndPosition(meta, &kv.Value, v), v.Code,
				`default value of input %q in metadata of %q action %s`, kv.Name, meta.Name, message,
			)
		}
	}
}

func (rule *validator) checkCompositeActionStepKeys(meta *actionmeta.Metadata, s *actionmeta.Step, idx int, allowed []string) {
	for _, k := range s.Keys {
		if !slices.Contains(allowed, strings.ToLower(k)) {
			rule.compositeStepErrorf(meta, s, idx, actionmeta.CodeUnexpectedField, "has unexpected key %q. expected one of %s", k, messages.SortedQuotes(allowed))
		}
	}
}

func (rule *validator) checkCompositeActionStep(meta *actionmeta.Metadata, s *actionmeta.Step, idx int) {
	if !s.IsMapping {
		rule.compositeStepErrorf(meta, s, idx, actionmeta.CodeInvalidStep, `must be a mapping with "run" or "uses" key`)
		return
	}

	var hasRun, hasShell, hasUses bool
	for _, k := range s.Keys {
		switch strings.ToLower(k) {
		case "run":
			hasRun = true
		case "shell":
			hasShell = true
		case "uses":
			hasUses = true
		}
	}

	switch {
	case hasRun && hasUses:
		rule.compositeStepErrorf(meta, s, idx, actionmeta.CodeConflictingStepKind, `cannot have both "run" and "uses" keys`)
	case hasRun:
		if s.Run == nil {
			rule.compositeStepErrorf(meta, s, idx, actionmeta.CodeInvalidFieldType, `must have a string value at "run" key`)
		}
		if !hasShell {
			rule.compositeStepErrorf(meta, s, idx, actionmeta.CodeMissingShell, `requires "shell" key since it has "run" key`)
		} else if s.Shell == nil {
			rule.compositeStepErrorf(meta, s, idx, actionmeta.CodeInvalidFieldType, `must have a string value at "shell" key`)
		}
		rule.checkCompositeActionStepKeys(meta, s, idx, actionmeta.StepKeys("run-step"))
	case hasUses:
		if s.Uses == nil {
			rule.compositeStepErrorf(meta, s, idx, actionmeta.CodeInvalidFieldType, `must have a string value at "uses" key`)
		} else if *s.Uses == "" {
			rule.compositeStepErrorf(meta, s, idx, actionmeta.CodeInvalidReference, `has empty "uses" value`)
		} else if strings.HasPrefix(*s.Uses, "$/") && strings.ContainsRune(*s.Uses, '@') {
			rule.compositeStepErrorf(meta, s, idx, actionmeta.CodeInvalidReference, `self-repository references do not support an '@ref' suffix at "uses" key`)
		} else if u, _, _ := strings.Cut(*s.Uses, "@"); strings.HasSuffix(u, ".yml") || strings.HasSuffix(u, ".yaml") {
			rule.compositeStepErrorf(meta, s, idx, actionmeta.CodeReusableWorkflow, `cannot call reusable workflow %q at "uses" key`, *s.Uses)
		}
		rule.checkCompositeActionStepKeys(meta, s, idx, actionmeta.StepKeys("uses-step"))
	default:
		rule.compositeStepErrorf(meta, s, idx, actionmeta.CodeMissingStepKind, `requires either "run" or "uses" key`)
		keys := slices.Concat(actionmeta.StepKeys("run-step"), actionmeta.StepKeys("uses-step"))
		slices.Sort(keys)
		rule.checkCompositeActionStepKeys(meta, s, idx, slices.Compact(keys))
	}

	rule.checkCompositeActionStepExprs(meta, s, idx)
}

func (rule *validator) checkCompositeActionStepExprs(meta *actionmeta.Metadata, s *actionmeta.Step, idx int) {
	report := func(key, field string, v *actionmeta.ExprString, bare bool) {
		if v == nil {
			return
		}
		for _, violation := range rule.expressionFindings(v.Value, bare, "runs.steps.*."+field, meta.Inputs) {
			pos := actionmeta.ExpressionPosition(meta, v, violation.Offset)
			end := actionmeta.ExpressionEndPosition(meta, v, violation)
			if ctx := violation.Context; ctx != "" {
				rule.compositeStepErrorfAt(
					meta, idx, pos.Line, pos.Col, end, violation.Code,
					`uses context %q at %q key which is not available in a composite action. %s`,
					ctx, key, compositeStepContextHint(ctx),
				)
			} else {
				rule.compositeStepErrorfAt(meta, idx, pos.Line, pos.Col, end, violation.Code, `%s at %q key`, violation.Message, key)
			}
		}
	}

	report("if", "if", s.If, true)
	report("run", "run", s.Run, false)
	report("working-directory", "working-directory", s.WorkingDirectory, false)
	report("name", "name", s.StepName, false)
	report("shell", "shell", s.Shell, false)
	report("continue-on-error", "continue-on-error", s.ContinueOnError, false)
	report("with", "with", s.WithExpression, false)
	report("env", "env", s.EnvExpression, false)
	for _, kv := range s.With {
		report(`with.`+kv.Name, "with.*", &kv.Value, false)
	}
	for _, kv := range s.Env {
		report(`env.`+kv.Name, "env.*", &kv.Value, false)
	}
}

func compositeStepContextHint(ctx string) string {
	switch ctx {
	case "secrets":
		return "pass secrets to the action as inputs instead"
	case "vars":
		return "the runner does not provide the vars context to actions; pass the values as inputs instead"
	case "needs":
		return "the needs context is only available in workflow jobs"
	default:
		return "see https://docs.github.com/en/actions/reference/workflows-and-actions/contexts for details"
	}
}

// https://docs.github.com/en/actions/creating-actions/metadata-syntax-for-github-actions#runs-for-javascript-actions
// Agents: https://docs.github.com/api/article/body?pathname=/en/actions/reference/workflows-and-actions/metadata-syntax
func (rule *validator) checkLocalJavaScriptActionRuns(r *actionmeta.Runs, dir, name string, pos *workflowlang.Pos) {
	if r.Main == "" {
		rule.missingRunsProp(pos, "main", "JavaScript", name, dir)
	} else {
		rule.checkRunsFileExists(r.Main, dir, "main", name, pos)
	}

	rule.checkRunsFileExists(r.Pre, dir, "pre", name, pos)
	if r.Pre == "" && r.PreIf != "" {
		pos := rule.metadataFieldPosition(pos, "runs", "pre-if")
		rule.Errorf(pos, actionmeta.CodeMissingField, `"pre" is required when "pre-if" is specified in "runs" section in %q action. the action is defined at %q`, name, dir)
	}

	rule.checkRunsFileExists(r.Post, dir, "post", name, pos)
	if r.Post == "" && r.PostIf != "" {
		pos := rule.metadataFieldPosition(pos, "runs", "post-if")
		rule.Errorf(pos, actionmeta.CodeMissingField, `"post" is required when "post-if" is specified in "runs" section in %q action. the action is defined at %q`, name, dir)
	}

	rule.checkInvalidRunsProps(pos, r, "JavaScript", name, dir, []string{"steps", "image", "pre-entrypoint", "entrypoint", "post-entrypoint", "args", "env"})
}

func (rule *validator) checkLocalActionInputs(meta *actionmeta.Metadata, pos *workflowlang.Pos) {
	for _, i := range meta.Inputs {
		if i.Deprecated && i.DeprecationMessage == "" {
			rule.Errorf(
				rule.metadataFieldPosition(pos, "inputs", i.Name, "deprecationMessage"),
				actionmeta.CodeEmptyDeprecationMessage,
				"input %q is deprecated but \"deprecationMessage\" is empty in metadata of %q action at %q",
				i.Name,
				meta.Name,
				meta.Path(),
			)
		}
	}
}

// https://docs.github.com/en/actions/creating-actions/metadata-syntax-for-github-actions#runs
// Agents: https://docs.github.com/api/article/body?pathname=/en/actions/reference/workflows-and-actions/metadata-syntax
func (rule *validator) checkLocalActionRuns(meta *actionmeta.Metadata, pos *workflowlang.Pos) {
	pos = rule.metadataFieldPosition(pos, "runs", "using")
	r := &meta.Runs
	using := strings.ToLower(r.Using)
	switch using {
	case "":
		if r.Plugin == "" {
			rule.Errorf(pos, actionmeta.CodeMissingField, `"runs.using" is missing in local action %q defined at %q`, meta.Name, meta.Dir())
		}
	case "docker":
		rule.checkLocalDockerActionRuns(r, meta.Dir(), meta.Name, pos)
	case "composite":
		rule.checkLocalCompositeActionRuns(meta, pos)
	default:
		if _, ok := actionmeta.Runtimes[using]; !ok {
			rule.Errorf(pos, actionmeta.CodeRuntimeUnsupported, `invalid runner name %q at runs.using in %q action defined at %q. valid runners are %s. see https://docs.github.com/en/actions/creating-actions/metadata-syntax-for-github-actions#runs`, r.Using, meta.Name, meta.Dir(), actionmeta.ValidRuntimes())
		} else if problem := actionmeta.RuntimeProblem(using); problem != "" {
			rule.Errorf(pos, runtimeProblemCode(using), "%s. update runs.using in local action %q to a current runtime", problem, meta.Name)
		}

		// Validate JavaScript fields even when the runtime declaration has a finding.
		if strings.HasPrefix(using, "node") {
			rule.checkLocalJavaScriptActionRuns(r, meta.Dir(), meta.Name, pos)
		}
	}
}

// https://docs.github.com/en/actions/creating-actions/metadata-syntax-for-github-actions
// Agents: https://docs.github.com/api/article/body?pathname=/en/actions/reference/workflows-and-actions/metadata-syntax
func (rule *validator) checkLocalActionMetadata(meta *actionmeta.Metadata, caller *workflowlang.Pos) {
	rule.checkActionMetadataSchema(meta)
	if meta.Name == "" {
		rule.Errorf(rule.metadataFieldPosition(caller, "name"), actionmeta.CodeMissingField, "name is required in action metadata %q", meta.Path())
	}
	if meta.Description == "" {
		rule.Errorf(rule.metadataFieldPosition(caller, "description"), actionmeta.CodeMissingField, "description is required in metadata of %q action at %q", meta.Name, meta.Path())
	}
	if meta.Branding.Icon != "" {
		if _, ok := brandingIcons[strings.ToLower(meta.Branding.Icon)]; !ok {
			rule.Errorf(
				rule.metadataFieldPosition(caller, "branding", "icon"),
				actionmeta.CodeBrandingIcon,
				"incorrect icon name %q at branding.icon in metadata of %q action at %q. see the official document to know the exhaustive list of supported icons: https://docs.github.com/en/actions/creating-actions/metadata-syntax-for-github-actions#brandingicon",
				meta.Branding.Icon,
				meta.Name,
				meta.Path(),
			)
		}
	}
	if meta.Branding.Color != "" {
		if _, ok := brandingColors[strings.ToLower(meta.Branding.Color)]; !ok {
			rule.Errorf(
				rule.metadataFieldPosition(caller, "branding", "color"),
				actionmeta.CodeBrandingColor,
				"incorrect color %q at branding.icon in metadata of %q action at %q. see the official document to know the exhaustive list of supported colors: https://docs.github.com/en/actions/creating-actions/metadata-syntax-for-github-actions#brandingcolor",
				meta.Branding.Color,
				meta.Name,
				meta.Path(),
			)
		}
	}
	rule.checkLocalActionInputs(meta, caller)
	rule.checkActionInputDefaults(meta)
	rule.checkLocalActionRuns(meta, caller)
}

var reNewlineWithIndent = regexp.MustCompile(`\s*\r?\n\s*`)

func (rule *validator) checkAction(meta actionmeta.Definition, exec workflowdecl.ExecActionView, description string) {
	inputs := exec.ResolvedInputs()
	known := inputs.Complete()
	// Check specified inputs are defined in action's inputs spec
	for key := range inputs.KeyedEntries() {
		name, scalar := key.ScalarString()
		if !scalar {
			continue
		}
		id := strings.ToLower(name)
		position := key.Origin().Span.Start
		pos := &workflowlang.Pos{Line: position.Line, Col: position.Column}
		m, ok := meta.Input(id)
		if !ok {
			if meta.AllowsAdditionalInputs() {
				continue
			}
			ns := make([]string, 0, len(meta.InputNames()))
			for _, inputID := range meta.InputNames() {
				i, _ := meta.Input(inputID)
				ns = append(ns, i.Name)
			}
			rule.Errorf(
				pos,
				actionmeta.CodeUnknownInput,
				"input %q is not defined in action %s. available inputs are %s",
				name,
				description,
				messages.SortedQuotes(ns),
			)
		} else if m.Deprecated && !m.Required {
			// Note: Using required inputs cannot be avoided. So we don't report it as error (though this should not
			// happen normally).
			msg := fmt.Sprintf(
				"avoid using deprecated input %q in action %s",
				name,
				description,
			)
			d := reNewlineWithIndent.ReplaceAllString(strings.TrimRight(m.DeprecationMessage, ". "), " ")
			if d != "" {
				msg += ": " + d
			}
			rule.Error(pos, actionmeta.CodeDeprecatedInput, msg)
		}
	}

	// Check mandatory inputs are specified
	for _, id := range meta.InputNames() {
		i, _ := meta.Input(id)
		if i.Required && known {
			if inputs.Lookup(id).State() == value.Missing {
				ns := make([]string, 0, len(meta.InputNames()))
				for _, inputID := range meta.InputNames() {
					i, _ := meta.Input(inputID)
					if i.Required {
						ns = append(ns, i.Name)
					}
				}
				rule.Errorf(
					positionPointer(exec.Uses().Pos()),
					actionmeta.CodeRequiredInput,
					"missing input %q which is required by action %s. all required inputs are %s",
					i.Name,
					description,
					messages.SortedQuotes(ns),
				)
			}
		}
	}
}

func (rule *validator) checkActionMetadataSchema(meta *actionmeta.Metadata) {
	validation := actionmeta.Validate(meta)
	problems := validation.Problems
	for _, field := range validation.ExpressionFields {
		for _, finding := range expressionSemanticFindings(field.Expressions, field.SchemaPath, rule.advisories) {
			position := actionmeta.ExpressionPosition(meta, &field.Value, finding.Offset)
			end := actionmeta.ExpressionEndPosition(meta, &field.Value, finding)
			problem := actionmeta.MetadataProblem{Path: meta.Path(), Code: finding.Code, Message: fmt.Sprintf("%s at %q in action metadata", finding.Message, field.FieldPath), Start: sourcepos.Position{Line: position.Line, Column: position.Col}}
			if end != nil {
				problem.End = sourcepos.Position{Line: end.Line, Column: end.Col}
			}
			problems = append(problems, problem)
		}
	}
	for _, problem := range problems {
		ruleName, diagnosticCode := problemRule(problem.Code)
		finding := findingmodel.FromDiagnostic(results.Diagnostic{
			Rule: ruleName, Code: diagnosticCode, Path: problem.Path,
			Message: problem.Message, Start: problem.Start, End: problem.End,
		})
		finding.Source = meta.Source
		finding.IgnoreOrigin = &findingmodel.IgnoreOrigin{Metadata: true}
		rule.errs = append(rule.errs, finding)
	}
}

// Missing fields point at their nearest declared parent. Existing fields point
// at their key. Workflow-call diagnostics point to the caller's uses: value.
func (rule *validator) metadataFieldPosition(fallback *workflowlang.Pos, fields ...string) *workflowlang.Pos {
	if rule.standalone == nil {
		return fallback
	}
	node := actionmeta.Node(rule.standalone.Root)
	pos := &workflowlang.Pos{Line: 1, Col: 1}
	for _, field := range fields {
		if node == nil || node.Kind != syntax.MappingNode {
			break
		}
		var next *syntax.Node
		for i := 0; i+1 < len(node.Content); i += 2 {
			key := node.Content[i]
			if strings.EqualFold(actionmeta.Key(key), field) {
				pos = &workflowlang.Pos{Line: key.Line, Col: key.Column}
				next = actionmeta.Node(node.Content[i+1])
				break
			}
		}
		node = next
	}
	return pos
}

// problemRule selects the configurable check and its reported subcode.
func problemRule(code actionmeta.ProblemCode) (ruleName, diagnosticCode string) {
	switch code {
	case actionmeta.CodeCoercion, actionmeta.CodeStringCondition, actionmeta.CodeJoinLiteral, actionmeta.CodeCollectionComparison:
		return string(code), string(code)
	case actionmeta.CodeInvalidJSON, actionmeta.CodeFormatPlaceholder:
		return "expression-call", string(code)
	case actionmeta.CodeMissingField, actionmeta.CodeUnexpectedField, actionmeta.CodeInvalidFieldType,
		actionmeta.CodeDuplicateField, actionmeta.CodeNestingDepth, actionmeta.CodeExpressionForbidden,
		actionmeta.CodeMissingFile, actionmeta.CodeInvalidDockerfile, actionmeta.CodeInvalidStepID,
		actionmeta.CodeDuplicateStepID, actionmeta.CodeInvalidStep, actionmeta.CodeConflictingStepKind,
		actionmeta.CodeMissingStepKind, actionmeta.CodeMissingShell, actionmeta.CodeInvalidReference,
		actionmeta.CodeReusableWorkflow, actionmeta.CodeExpressionSyntax, actionmeta.CodeExpressionContext,
		actionmeta.CodeExpressionReference, actionmeta.CodeExpressionCall, actionmeta.CodeExpressionType,
		actionmeta.CodeJSONMemberCollision, actionmeta.CodeRuntimeUnsupported, actionmeta.CodeRuntimeRemoved,
		actionmeta.CodeRuntimeDeprecated, actionmeta.CodeBrandingIcon, actionmeta.CodeBrandingColor,
		actionmeta.CodeEmptyDeprecationMessage, actionmeta.CodeUnknownInput, actionmeta.CodeDeprecatedInput,
		actionmeta.CodeRequiredInput, actionmeta.CodeYAMLAnchor, actionmeta.CodeYAMLAlias, actionmeta.CodeYAMLTag,
		actionmeta.CodeMetadataParse, actionmeta.CodeMetadataRead, actionmeta.CodeMetadataEmpty:
		return "action", string(code)
	}
	return "action", string(code)
}
