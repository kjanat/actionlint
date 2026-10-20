package validation

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	actionmeta "actionlint.kjanat.dev/internal/action"
	exprlang "actionlint.kjanat.dev/internal/expression"
	findingmodel "actionlint.kjanat.dev/internal/finding"
	"actionlint.kjanat.dev/internal/semantics/identifier"
	"actionlint.kjanat.dev/internal/stringutil"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
	"actionlint.kjanat.dev/internal/yaml"
)

type validator struct {
	standalone *actionmeta.Metadata
	errs       []*findingmodel.Finding
}

func (rule *validator) Error(pos *workflowlang.Pos, message string) {
	rule.errs = append(rule.errs, findingmodel.At(pos, "action", message))
}

func (rule *validator) Errorf(pos *workflowlang.Pos, format string, args ...any) {
	rule.errs = append(rule.errs, findingmodel.Errorf(pos, "action", format, args...))
}

// Check validates a manifest reached from a workflow call site. Findings which
// cannot point inside the manifest retain the caller's uses position.
func Check(meta *actionmeta.Metadata, caller *workflowlang.Pos) []*findingmodel.Finding {
	rule := validator{}
	rule.checkLocalActionMetadata(meta, &workflowlang.ExecAction{Uses: &workflowlang.String{Pos: caller}})
	return rule.errs
}

// Standalone validates a manifest's declarations at their own source positions.
func Standalone(meta *actionmeta.Metadata) []*findingmodel.Finding {
	rule := validator{standalone: meta}
	rule.checkLocalActionMetadata(meta, &workflowlang.ExecAction{Uses: &workflowlang.String{Pos: &workflowlang.Pos{Line: 1, Col: 1}}})
	return rule.errs
}

// Inputs validates supplied and required inputs against resolved action metadata.
func Inputs(meta *actionmeta.Metadata, exec *workflowlang.ExecAction, describe func(*actionmeta.Metadata) string) []*findingmodel.Finding {
	rule := validator{}
	rule.checkAction(meta, exec, describe)
	return rule.errs
}

// https://docs.github.com/en/actions/creating-actions/metadata-syntax-for-github-actions#runsimage
// Agents: https://docs.github.com/api/article/body?pathname=/en/actions/reference/workflows-and-actions/metadata-syntax
//
// Required! The Docker image to use as the container to run the action.
// The value can be the Docker base image name, a local `Dockerfile` in your repository,
// or a public image in Docker Hub or another registry.
//
// To reference a `Dockerfile` local to your repository, the file must be named `Dockerfile`
// and you must use a path relative to your action metadata file.
// The `docker` application will execute this file.
func isImageOnDockerRegistry(image string) bool {
	return strings.HasPrefix(image, "docker://") ||
		strings.HasPrefix(image, "gcr.io/") ||
		strings.HasPrefix(image, "pkg.dev/") ||
		strings.HasPrefix(image, "ghcr.io/") ||
		strings.HasPrefix(image, "docker.io/")
}

func (rule *validator) missingRunsProp(pos *workflowlang.Pos, prop, ty, action, path string) {
	pos = rule.metadataFieldPosition(pos, "runs", prop)
	rule.Errorf(pos, `%q is required in "runs" section because %q is a %s action. the action is defined at %q`, prop, action, ty, path)
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
			rule.Errorf(pos, `%q is not allowed in "runs" section because %q is a %s action. the action is defined at %q`, prop, action, ty, path)
		}
	}
}

func (rule *validator) checkRunsFileExists(file, dir, prop, name string, pos *workflowlang.Pos) {
	pos = rule.metadataFieldPosition(pos, "runs", prop)
	f := filepath.FromSlash(file)
	if f == "" {
		return
	}
	p := filepath.Join(dir, f)
	if _, err := os.Stat(p); errors.Is(err, os.ErrNotExist) {
		rule.Errorf(pos, `file %q does not exist in %q. it is specified at %q key in "runs" section in %q action`, f, dir, prop, name)
	}
}

// https://docs.github.com/en/actions/creating-actions/metadata-syntax-for-github-actions#runs-for-docker-container-actions
// Agents: https://docs.github.com/api/article/body?pathname=/en/actions/reference/workflows-and-actions/metadata-syntax
func (rule *validator) checkLocalDockerActionRuns(r *actionmeta.Runs, dir, name string, pos *workflowlang.Pos) {
	if r.Image == "" {
		rule.missingRunsProp(pos, "image", "Docker", name, dir)
	} else if !isImageOnDockerRegistry(r.Image) {
		rule.checkRunsFileExists(r.Image, dir, "image", name, pos)
		if filepath.Base(filepath.FromSlash(r.Image)) != "Dockerfile" {
			pos := rule.metadataFieldPosition(pos, "runs", "image")
			rule.Errorf(pos, `the local file %q referenced from "image" key must be named "Dockerfile" in %q action. the action is defined at %q`, r.Image, name, dir)
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
				rule.compositeStepErrorfAt(meta, i, s.ID.Line, s.ID.Column, nil, "has invalid ID %q; IDs must start with a letter or _, contain only alphanumeric characters, - or _, be shorter than 100 characters, and must not start with __", id)
			case ids[strings.ToLower(id)]:
				rule.compositeStepErrorfAt(meta, i, s.ID.Line, s.ID.Column, nil, "has duplicate ID %q; step IDs are case insensitive", id)
			}
			ids[strings.ToLower(id)] = true
		}
		rule.checkCompositeActionStep(meta, s, i)
	}
	rule.checkInvalidRunsProps(pos, r, "Composite", meta.Name, meta.Dir(), []string{"main", "pre", "pre-if", "post", "post-if", "image", "pre-entrypoint", "entrypoint", "post-entrypoint", "args", "env"})
}

// compositeStepErrorf reports an error at the step's own position in the action metadata file
// instead of at the "uses" site in the workflow being linted.
func (rule *validator) compositeStepErrorf(meta *actionmeta.Metadata, s *actionmeta.Step, idx int, format string, args ...any) {
	rule.compositeStepErrorfAt(meta, idx, s.Line, s.Column, nil, format, args...)
}

// compositeStepErrorfAt reports a position inside the step, with an optional exclusive end.
func (rule *validator) compositeStepErrorfAt(meta *actionmeta.Metadata, idx, line, col int, end *workflowlang.Pos, format string, args ...any) {
	m := fmt.Sprintf(format, args...)
	err := findingmodel.At(&workflowlang.Pos{Line: line, Col: col}, "action", fmt.Sprintf(`"runs.steps[%d]" in metadata of %q action %s`, idx, meta.Name, m))
	err.Path = meta.Path()
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
			if literal := exprlang.LiteralExpressionValue(*value); literal != nil {
				*value = *literal
			}
		}
		err.IgnoreOrigin = origin
	}
	rule.errs = append(rule.errs, err)
}

// metadataErrorfAt reports inside the action metadata, with an optional exclusive end.
func (rule *validator) metadataErrorfAt(meta *actionmeta.Metadata, line, col int, end *workflowlang.Pos, format string, args ...any) {
	err := findingmodel.Errorf(&workflowlang.Pos{Line: line, Col: col}, "action", format, args...)
	err.Path = meta.Path()
	err.Source = meta.Source
	err.EndPosition = end
	err.IgnoreOrigin = &findingmodel.IgnoreOrigin{Metadata: true}
	rule.errs = append(rule.errs, err)
}

func (rule *validator) checkActionInputDefaults(meta *actionmeta.Metadata) {
	const field = "inputs.*.default"
	for _, kv := range meta.InputDefaults {
		for _, v := range actionmeta.ExpressionViolations(kv.Value.Value, false, field, meta.Inputs) {
			pos := actionmeta.ExpressionPosition(meta, &kv.Value, v.Offset)
			message := v.Message
			if v.Context != "" {
				message = fmt.Sprintf(
					`uses context %q which is not available in input defaults. available contexts are %s`,
					v.Context, stringutil.Quotes(actionmeta.AllowedContexts(field)),
				)
			}
			rule.metadataErrorfAt(
				meta, pos.Line, pos.Col, actionmeta.ExpressionEndPosition(meta, &kv.Value, v),
				`default value of input %q in metadata of %q action %s`, kv.Name, meta.Name, message,
			)
		}
	}
}

func (rule *validator) checkCompositeActionStepKeys(meta *actionmeta.Metadata, s *actionmeta.Step, idx int, allowed []string) {
	for _, k := range s.Keys {
		if !slices.Contains(allowed, strings.ToLower(k)) {
			rule.compositeStepErrorf(meta, s, idx, "has unexpected key %q. expected one of %s", k, stringutil.SortedQuotes(allowed))
		}
	}
}

func (rule *validator) checkCompositeActionStep(meta *actionmeta.Metadata, s *actionmeta.Step, idx int) {
	if !s.IsMapping {
		rule.compositeStepErrorf(meta, s, idx, `must be a mapping with "run" or "uses" key`)
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
		rule.compositeStepErrorf(meta, s, idx, `cannot have both "run" and "uses" keys`)
	case hasRun:
		if s.Run == nil {
			rule.compositeStepErrorf(meta, s, idx, `must have a string value at "run" key`)
		}
		if !hasShell {
			rule.compositeStepErrorf(meta, s, idx, `requires "shell" key since it has "run" key`)
		} else if s.Shell == nil {
			rule.compositeStepErrorf(meta, s, idx, `must have a string value at "shell" key`)
		}
		rule.checkCompositeActionStepKeys(meta, s, idx, actionmeta.StepKeys("run-step"))
	case hasUses:
		if s.Uses == nil {
			rule.compositeStepErrorf(meta, s, idx, `must have a string value at "uses" key`)
		} else if *s.Uses == "" {
			rule.compositeStepErrorf(meta, s, idx, `has empty "uses" value`)
		} else if strings.HasPrefix(*s.Uses, "$/") && strings.ContainsRune(*s.Uses, '@') {
			rule.compositeStepErrorf(meta, s, idx, `self-repository references do not support an '@ref' suffix at "uses" key`)
		} else if u, _, _ := strings.Cut(*s.Uses, "@"); strings.HasSuffix(u, ".yml") || strings.HasSuffix(u, ".yaml") {
			rule.compositeStepErrorf(meta, s, idx, `cannot call reusable workflow %q at "uses" key`, *s.Uses)
		}
		rule.checkCompositeActionStepKeys(meta, s, idx, actionmeta.StepKeys("uses-step"))
	default:
		rule.compositeStepErrorf(meta, s, idx, `requires either "run" or "uses" key`)
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
		for _, violation := range actionmeta.ExpressionViolations(v.Value, bare, "runs.steps.*."+field, meta.Inputs) {
			pos := actionmeta.ExpressionPosition(meta, v, violation.Offset)
			end := actionmeta.ExpressionEndPosition(meta, v, violation)
			if ctx := violation.Context; ctx != "" {
				rule.compositeStepErrorfAt(
					meta, idx, pos.Line, pos.Col, end,
					`uses context %q at %q key which is not available in a composite action. %s`,
					ctx, key, compositeStepContextHint(ctx),
				)
			} else {
				rule.compositeStepErrorfAt(meta, idx, pos.Line, pos.Col, end, `%s at %q key`, violation.Message, key)
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
		rule.Errorf(pos, `"pre" is required when "pre-if" is specified in "runs" section in %q action. the action is defined at %q`, name, dir)
	}

	rule.checkRunsFileExists(r.Post, dir, "post", name, pos)
	if r.Post == "" && r.PostIf != "" {
		pos := rule.metadataFieldPosition(pos, "runs", "post-if")
		rule.Errorf(pos, `"post" is required when "post-if" is specified in "runs" section in %q action. the action is defined at %q`, name, dir)
	}

	rule.checkInvalidRunsProps(pos, r, "JavaScript", name, dir, []string{"steps", "image", "pre-entrypoint", "entrypoint", "post-entrypoint", "args", "env"})
}

func (rule *validator) checkLocalActionInputs(meta *actionmeta.Metadata, pos *workflowlang.Pos) {
	for _, i := range meta.Inputs {
		if i.Deprecated && i.DeprecationMessage == "" {
			rule.Errorf(
				rule.metadataFieldPosition(pos, "inputs", i.Name, "deprecationMessage"),
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
			rule.Errorf(pos, `"runs.using" is missing in local action %q defined at %q`, meta.Name, meta.Dir())
		}
	case "docker":
		rule.checkLocalDockerActionRuns(r, meta.Dir(), meta.Name, pos)
	case "composite":
		rule.checkLocalCompositeActionRuns(meta, pos)
	default:
		if _, ok := actionmeta.Runtimes[using]; !ok {
			rule.Errorf(pos, `invalid runner name %q at runs.using in %q action defined at %q. valid runners are %s. see https://docs.github.com/en/actions/creating-actions/metadata-syntax-for-github-actions#runs`, r.Using, meta.Name, meta.Dir(), actionmeta.ValidRuntimes())
		} else if problem := actionmeta.RuntimeProblem(using); problem != "" {
			rule.Errorf(pos, "%s. update runs.using in local action %q to a current runtime", problem, meta.Name)
		}

		// Validate JavaScript fields even when the runtime declaration has a diagnostic.
		if strings.HasPrefix(using, "node") {
			rule.checkLocalJavaScriptActionRuns(r, meta.Dir(), meta.Name, pos)
		}
	}
}

// https://docs.github.com/en/actions/creating-actions/metadata-syntax-for-github-actions
// Agents: https://docs.github.com/api/article/body?pathname=/en/actions/reference/workflows-and-actions/metadata-syntax
func (rule *validator) checkLocalActionMetadata(meta *actionmeta.Metadata, action *workflowlang.ExecAction) {
	rule.checkActionMetadataSchema(meta)
	if meta.Name == "" {
		rule.Errorf(rule.metadataFieldPosition(action.Uses.Pos, "name"), "name is required in action metadata %q", meta.Path())
	}
	if meta.Description == "" {
		rule.Errorf(rule.metadataFieldPosition(action.Uses.Pos, "description"), "description is required in metadata of %q action at %q", meta.Name, meta.Path())
	}
	if meta.Branding.Icon != "" {
		if _, ok := brandingIcons[strings.ToLower(meta.Branding.Icon)]; !ok {
			rule.Errorf(
				rule.metadataFieldPosition(action.Uses.Pos, "branding", "icon"),
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
				rule.metadataFieldPosition(action.Uses.Pos, "branding", "color"),
				"incorrect color %q at branding.icon in metadata of %q action at %q. see the official document to know the exhaustive list of supported colors: https://docs.github.com/en/actions/creating-actions/metadata-syntax-for-github-actions#brandingcolor",
				meta.Branding.Color,
				meta.Name,
				meta.Path(),
			)
		}
	}
	rule.checkLocalActionInputs(meta, action.Uses.Pos)
	rule.checkActionInputDefaults(meta)
	rule.checkLocalActionRuns(meta, action.Uses.Pos)
}

var reNewlineWithIndent = regexp.MustCompile(`\s*\r?\n\s*`)

func (rule *validator) checkAction(meta *actionmeta.Metadata, exec *workflowlang.ExecAction, describe func(*actionmeta.Metadata) string) {
	inputs, known := workflowlang.ActionInputNames(exec)
	// Check specified inputs are defined in action's inputs spec
	for id, i := range inputs {
		m, ok := meta.Inputs[id]
		if !ok {
			ns := make([]string, 0, len(meta.Inputs))
			for _, i := range meta.Inputs {
				ns = append(ns, i.Name)
			}
			rule.Errorf(
				i.Name.Pos,
				"input %q is not defined in action %s. available inputs are %s",
				i.Name.Value,
				describe(meta),
				stringutil.SortedQuotes(ns),
			)
		} else if m.Deprecated && !m.Required {
			// Note: Using required inputs cannot be avoided. So we don't report it as error (though this should not
			// happen normally).
			msg := fmt.Sprintf(
				"avoid using deprecated input %q in action %s",
				i.Name.Value,
				describe(meta),
			)
			d := reNewlineWithIndent.ReplaceAllString(strings.TrimRight(m.DeprecationMessage, ". "), " ")
			if d != "" {
				msg += ": " + d
			}
			rule.Error(i.Name.Pos, msg)
		}
	}

	// Check mandatory inputs are specified
	for id, i := range meta.Inputs {
		if i.Required && known {
			if _, ok := inputs[id]; !ok {
				ns := make([]string, 0, len(meta.Inputs))
				for _, i := range meta.Inputs {
					if i.Required {
						ns = append(ns, i.Name)
					}
				}
				rule.Errorf(
					exec.Uses.Pos,
					"missing input %q which is required by action %s. all required inputs are %s",
					i.Name,
					describe(meta),
					stringutil.SortedQuotes(ns),
				)
			}
		}
	}
}

func (rule *validator) checkActionMetadataSchema(meta *actionmeta.Metadata) {
	for _, d := range actionmeta.Validate(meta) {
		finding := findingmodel.FromDiagnostic(d)
		finding.Source = meta.Source
		finding.IgnoreOrigin = &findingmodel.IgnoreOrigin{Metadata: true}
		rule.errs = append(rule.errs, finding)
	}
}

// Missing fields point at their nearest declared parent. Existing fields point
// at their key. Workflow-call diagnostics retain their original uses: position.
func (rule *validator) metadataFieldPosition(fallback *workflowlang.Pos, fields ...string) *workflowlang.Pos {
	if rule.standalone == nil {
		return fallback
	}
	node := actionmeta.Node(rule.standalone.Root)
	pos := &workflowlang.Pos{Line: 1, Col: 1}
	for _, field := range fields {
		if node == nil || node.Kind != yaml.MappingNode {
			break
		}
		var next *yaml.Node
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
