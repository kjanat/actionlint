package correctness

import (
	"context"
	"errors"
	"fmt"

	"actionlint.kjanat.dev/v2/internal/checking/define"
	"actionlint.kjanat.dev/v2/internal/checking/rules/correctness/actioncheck"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	"actionlint.kjanat.dev/v2/internal/checking/workflowvisit"
	"actionlint.kjanat.dev/v2/internal/input/references/actions"
	"actionlint.kjanat.dev/v2/internal/input/references/actions/localactions"
	"actionlint.kjanat.dev/v2/internal/input/references/actions/remote"
	"actionlint.kjanat.dev/v2/internal/model/actionmeta"
	"actionlint.kjanat.dev/v2/internal/model/calls/callreferences"
	"actionlint.kjanat.dev/v2/internal/model/parsing"
	"actionlint.kjanat.dev/v2/internal/model/workflows"
	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
	"actionlint.kjanat.dev/v2/internal/results/finding"
)

// InvalidActionReferenceRule also supplies the shared action metadata traversal.
var InvalidActionReferenceRule = &define.Rule{
	Name:            "invalid-action-reference",
	Description:     "Validate action reference syntax",
	Category:        "correctness",
	Subcategory:     "actions",
	Recommended:     true,
	Help:            "Use owner/repo[/path]@ref, ./path, $/path, or a valid docker:// reference.",
	SupportsActions: true,
	Walker: func(c define.RuleInputs, base workflowvisit.Base) workflowvisit.Rule {
		return NewRuleAction(c.Context, base, c.Actions.Prepared(), c.RemoteActions).WithLifecycle(c.Lifecycle)
	},
}

type ruleAction struct {
	workflowvisit.Base
	reporting.Logger
	cache      localactions.ActionMetadataDocumentLookup
	validation *actionMetadataValidation
	remote     *remote.ResolvedActions
	context    context.Context
	lifecycle  actionmeta.LifecycleContext
}

func (rule *ruleAction) WithLifecycle(context actionmeta.LifecycleContext) *ruleAction {
	if rule.lifecycle != context {
		rule.validation = newActionMetadataValidation()
	}
	rule.lifecycle = context
	return rule
}

func NewRuleAction(ctx context.Context, base workflowvisit.Base, cache localactions.ActionMetadataDocumentLookup, remote *remote.ResolvedActions) *ruleAction {
	return &ruleAction{
		Base:  base,
		Tag:   base.Name(),
		cache: cache, validation: newActionMetadataValidation(), remote: remote, context: ctx,
	}
}

func (rule *ruleAction) VisitStep(n workflows.BoundStep) error {
	e, ok := n.Action()
	if !ok || e.Uses == nil {
		return nil
	}

	if e.Uses.ContainsExpression() {
		return nil
	}

	spec := e.Uses.Text()
	if _, hosted := callreferences.ParseRepositoryURL(spec); hosted {
		// vendor-lock-in owns URL-form references independently of metadata checks.
		return nil
	}

	switch callreferences.FormOf(spec) {
	case callreferences.FormWorkspace:
		return rule.checkLocalAction(spec, spec, e)
	case callreferences.FormSelfRepository:
		// Relative to the repository and commit that contain the running workflow.
		local, ok := callreferences.SelfRepositoryLocal(spec)
		if !ok {
			rule.Report(actioncheck.InvalidReference(reporting.SourcePosition(e.Uses), spec, "self-repository references do not support an '@ref' suffix"))
			return nil
		}
		return rule.checkLocalAction(local, spec, e)
	case callreferences.FormContainer:
		rule.Report(actioncheck.Docker(e)...)
		return nil
	case callreferences.FormRepository, callreferences.FormBuiltin:
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
			code := problem.Code
			if code == "" {
				code = actionmeta.CodeMetadataParse
			}
			ruleName, diagnosticCode := actioncheck.ProblemRule(code)
			item := finding.Errorf(reporting.SourcePosition(e.Uses), ruleName, "remote action %q has invalid metadata at %s:%d:%d: %s", spec, problem.Path, problem.Start.Line, problem.Start.Column, problem.Message)
			item.Code = diagnosticCode
			rule.Report(item)
		}
	}
	rule.Report(actioncheck.Repository(e, inspection.Definition(), rule.lifecycle)...)
	return nil
}

func (rule *ruleAction) checkLocalAction(localSpec, displaySpec string, action *definition.ExecAction) error {
	if callreferences.FormOf(displaySpec) == callreferences.FormSelfRepository {
		localSpec = displaySpec
	}
	meta, _, source, err := rule.cache.FindMetadataDocument(localSpec)
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if err != nil {
		if _, unresolved := errors.AsType[localactions.UnresolvedError](err); unresolved {
			rule.Report(finding.At(reporting.SourcePosition(action.Uses), "missing-local-action", err.Error()))
		} else {
			item := finding.At(reporting.SourcePosition(action.Uses), "action-metadata-read", err.Error())
			item.Code = string(actionmeta.CodeMetadataRead)
			if parseError, ok := errors.AsType[*localactions.ParseError](err); ok {
				item.Rule = "syntax-check"
				item.Code = string(actionmeta.CodeMetadataParse)
				item.MetadataParseSource = source.Capture().Revision()
				if !item.MetadataParseSource.Valid() {
					return fmt.Errorf("metadata parse source %q has no captured revision: %w", parseError.Path(), err)
				}
			}
			rule.Report(item)
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

func (rule *ruleAction) ForMetadata(actions *localactions.Resolver) workflowvisit.MetadataParticipant {
	child := NewRuleAction(rule.context, workflowvisit.NewBase(rule.Name(), rule.Description()), actions.Prepared(), rule.remote).WithLifecycle(rule.lifecycle)
	child.validation = rule.validation
	return child
}

func (rule *ruleAction) CheckStandaloneMetadata(meta *actionmeta.Metadata, snapshot parsing.SourceSnapshot, files actionmeta.ReferencedFiles) error {
	if snapshot.Kind() != parsing.KindAction {
		return errors.New("standalone metadata has no admitted source snapshot")
	}
	if original, parsed := meta.Text(); !parsed || original != string(snapshot.Content()) {
		return errors.New("standalone metadata does not belong to its source snapshot")
	}
	rule.Report(actioncheck.StandaloneWithAdvisories(meta, files, rule.lifecycle)...)
	rule.AnnotateSince(0, func(item *finding.Finding) {
		item.MetadataIdentity = &finding.MetadataIdentity{Key: metadataFindingKey(meta, snapshot.Reference().Document, item), Declaration: true}
	})
	return nil
}

var _ workflowvisit.MetadataParticipant = (*ruleAction)(nil)
