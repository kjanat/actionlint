package correctness

import (
	"context"
	"errors"
	"fmt"

	"actionlint.kjanat.dev/v2/internal/checking/define"
	actioncheck "actionlint.kjanat.dev/v2/internal/checking/rules/correctness/action"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	"actionlint.kjanat.dev/v2/internal/checking/workflowvisit"
	"actionlint.kjanat.dev/v2/internal/input/references/actions"
	actionremote "actionlint.kjanat.dev/v2/internal/input/references/actions/remote"
	workspaceactions "actionlint.kjanat.dev/v2/internal/input/references/actions/workspace"
	"actionlint.kjanat.dev/v2/internal/model/actionmeta"
	"actionlint.kjanat.dev/v2/internal/model/calls/references"
	"actionlint.kjanat.dev/v2/internal/model/parsing"
	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"
	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
	findingmodel "actionlint.kjanat.dev/v2/internal/results/finding"
)

// ActionRule is the single definition of the action rule.
var ActionRule = &define.Rule{
	Name:            "action",
	Description:     "Validate action references, inputs, outputs, and local metadata",
	Category:        "correctness",
	Subcategory:     "actions",
	Recommended:     true,
	Help:            "Check the action reference, runtime, declared inputs and action metadata at the indicated source.",
	SupportsActions: true,
	Walker: func(c define.Inputs, base workflowvisit.Base) workflowvisit.Rule {
		return NewRuleAction(c.Context, base, c.Actions.Prepared(), c.RemoteActions).WithLifecycle(c.Lifecycle)
	},
}

// ruleAction is a rule to check running action in steps of jobs.
// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#jobsjob_idstepsuses
// Agents: https://docs.github.com/api/article/body?pathname=/en/actions/reference/workflows-and-actions/workflow-syntax
type ruleAction struct {
	workflowvisit.Base
	reporting.Logger
	cache      workspaceactions.MetadataDocumentLookup
	validation *actionMetadataValidation
	remote     *actionremote.ResolvedActions
	context    context.Context
	lifecycle  actionmeta.LifecycleContext
}

// WithLifecycle supplies the execution facts shared by workflow and action checks.
func (rule *ruleAction) WithLifecycle(context actionmeta.LifecycleContext) *ruleAction {
	if rule.lifecycle != context {
		rule.validation = newActionMetadataValidation()
	}
	rule.lifecycle = context
	return rule
}

// NewRuleAction checks references using local metadata and acquired remote definitions.
func NewRuleAction(ctx context.Context, base workflowvisit.Base, cache workspaceactions.MetadataDocumentLookup, remote *actionremote.ResolvedActions) *ruleAction {
	return &ruleAction{
		Base:  base,
		Tag:   base.Name(),
		cache: cache, validation: newActionMetadataValidation(), remote: remote, context: ctx,
	}
}

// VisitStep is callback when visiting Step node.
func (rule *ruleAction) VisitStep(n workflowdecl.Step) error {
	e, ok := n.Action()
	if !ok || e.Uses == nil {
		return nil
	}

	if e.Uses.ContainsExpression() {
		// Cannot parse specification made with interpolation. Give up
		return nil
	}

	spec := e.Uses.Text()
	if _, hosted := references.ParseRepositoryURL(spec); hosted {
		// vendor-lock-in owns URL-form references independently of metadata checks.
		return nil
	}

	switch references.FormOf(spec) {
	case references.FormWorkspace:
		// Relative to repository root
		return rule.checkLocalAction(spec, spec, e)
	case references.FormSelfRepository:
		// Relative to the repository and commit that contain the running workflow.
		// Normalize it to the existing local path form so metadata and output validation still work.
		local, ok := references.SelfRepositoryLocal(spec)
		if !ok {
			rule.Report(actioncheck.InvalidReference(reporting.SourcePosition(e.Uses), spec, "self-repository references do not support an '@ref' suffix"))
			return nil
		}
		return rule.checkLocalAction(local, spec, e)
	case references.FormContainer:
		rule.Report(actioncheck.Docker(e)...)
		return nil
	case references.FormRepository, references.FormBuiltin:
	}

	if findings := actioncheck.RepositoryReference(spec, reporting.SourcePosition(e.Uses)); len(findings) != 0 {
		rule.Report(findings...)
		return nil
	}
	ctx := rule.context
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	inspection := rule.remote.Inspect(spec)
	if inspection.Availability().State() == actions.MetadataInvalid {
		for _, problem := range inspection.Availability().Problems() {
			finding := findingmodel.Errorf(reporting.SourcePosition(e.Uses), "action", "remote action %q has invalid metadata at %s:%d:%d: %s", spec, problem.Path, problem.Start.Line, problem.Start.Column, problem.Message)
			finding.Code = problem.Code
			if finding.Code == "" {
				finding.Code = string(actionmeta.CodeMetadataParse)
			}
			rule.Report(finding)
		}
	}
	rule.Report(actioncheck.Repository(e, inspection.Definition(), rule.lifecycle)...)
	return nil
}

// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#example-using-action-in-the-same-repository-as-the-workflow
// Agents: https://docs.github.com/api/article/body?pathname=/en/actions/reference/workflows-and-actions/workflow-syntax
func (rule *ruleAction) checkLocalAction(localSpec, displaySpec string, action *workflowlang.ExecAction) error {
	if references.FormOf(displaySpec) == references.FormSelfRepository {
		localSpec = displaySpec
	}
	meta, _, source, err := rule.cache.FindMetadataDocument(localSpec)
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if err != nil {
		if _, unresolved := errors.AsType[workspaceactions.UnresolvedError](err); unresolved {
			rule.Report(findingmodel.At(reporting.SourcePosition(action.Uses), "missing-local-action", err.Error()))
		} else {
			finding := findingmodel.At(reporting.SourcePosition(action.Uses), rule.Name(), err.Error())
			finding.Code = string(actionmeta.CodeMetadataRead)
			if parseError, ok := errors.AsType[*workspaceactions.ParseError](err); ok {
				finding.Code = string(actionmeta.CodeMetadataParse)
				finding.MetadataParseSource = source.Capture().Revision()
				if !finding.MetadataParseSource.Valid() {
					return fmt.Errorf("metadata parse source %q has no captured revision: %w", parseError.Path(), err)
				}
			}
			rule.Report(finding)
		}
		return nil
	}
	if meta == nil {
		return nil
	}

	if err := rule.replayMetadataFindings(meta, source, action, displaySpec); err != nil {
		return err
	}

	rule.Report(actioncheck.Inputs(meta, action, func(m *actionmeta.Metadata) string {
		return fmt.Sprintf("%q defined at %q", m.Name, displaySpec)
	})...)
	return nil
}

func (rule *ruleAction) ForMetadata(actions *workspaceactions.Resolver) workflowvisit.MetadataParticipant {
	child := NewRuleAction(rule.context, workflowvisit.NewBase(rule.Name(), rule.Description()), actions.Prepared(), rule.remote).WithLifecycle(rule.lifecycle)
	child.validation = rule.validation
	return child
}

func (rule *ruleAction) CheckStandaloneMetadata(meta *actionmeta.Metadata, snapshot parsing.SourceSnapshot, files actionmeta.ReferencedFiles) error {
	if snapshot.Kind() != parsing.KindAction {
		return errors.New("standalone metadata has no admitted source snapshot")
	}
	if original, parsed := meta.OriginalSource(); !parsed || original != string(snapshot.Content()) {
		return errors.New("standalone metadata does not belong to its source snapshot")
	}
	rule.Report(actioncheck.StandaloneWithAdvisories(meta, files, rule.lifecycle)...)
	rule.AnnotateSince(0, func(finding *findingmodel.Finding) {
		finding.MetadataIdentity = &findingmodel.MetadataIdentity{Key: metadataFindingKey(meta, snapshot.Reference().Document, finding), Declaration: true}
	})
	return nil
}

var _ workflowvisit.MetadataParticipant = (*ruleAction)(nil)
