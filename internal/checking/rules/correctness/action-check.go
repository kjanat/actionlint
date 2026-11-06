package correctness

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"

	checkcalls "actionlint.kjanat.dev/v2/internal/checking/executor/calls"
	"actionlint.kjanat.dev/v2/internal/input/references/actions"

	actioncheck "actionlint.kjanat.dev/v2/internal/checking/rules/correctness/action"

	workflowvisit "actionlint.kjanat.dev/v2/internal/checking/traversal"

	actionmeta "actionlint.kjanat.dev/v2/internal/model/actions"

	workspaceactions "actionlint.kjanat.dev/v2/internal/input/references/actions/workspace"

	actionremote "actionlint.kjanat.dev/v2/internal/input/references/actions/remote"
	"actionlint.kjanat.dev/v2/internal/model/calls/references"
	"actionlint.kjanat.dev/v2/internal/model/parsing"

	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"

	findingmodel "actionlint.kjanat.dev/v2/internal/results/finding"
)

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
}

// NewRuleAction checks references using local metadata and acquired remote definitions.
func NewRuleAction(ctx context.Context, cache workspaceactions.MetadataDocumentLookup, remote *actionremote.ResolvedActions) *ruleAction {
	descriptor := ActionDeclaration()
	return &ruleAction{
		Base:  workflowvisit.NewBase(descriptor.Name, descriptor.Description),
		Tag:   descriptor.Name,
		cache: cache, validation: newActionMetadataValidation(), remote: remote, context: ctx,
	}
}

// VisitStep is callback when visiting Step node.
func (rule *ruleAction) VisitStep(n workflowdecl.StepView) error {
	e, ok := n.Action()
	if !ok || !e.Uses().Present() {
		return nil
	}

	if e.Uses().ContainsExpression() {
		// Cannot parse specification made with interpolation. Give up
		return nil
	}

	spec := e.Uses().Value()
	if _, hosted := references.ParseRepositoryURL(spec); hosted {
		// vendor-lock-in owns URL-form references independently of metadata checks.
		return nil
	}

	if strings.HasPrefix(spec, "./") {
		// Relative to repository root
		return rule.checkLocalAction(spec, spec, e)
	}

	if strings.HasPrefix(spec, "$/") {
		// Relative to the repository and commit that contain the running workflow.
		// Normalize it to the existing local path form so metadata and output validation still work.
		local, ok := references.SelfRepositoryLocal(spec)
		if !ok {
			rule.Report(actioncheck.InvalidReference(reporting.SourcePosition(e.Uses()), spec, "self-repository references do not support an '@ref' suffix"))
			return nil
		}
		return rule.checkLocalAction(local, spec, e)
	}

	if strings.HasPrefix(spec, "docker://") {
		rule.Report(actioncheck.Docker(e)...)
		return nil
	}

	if findings := actioncheck.RepositoryReference(spec, reporting.SourcePosition(e.Uses())); len(findings) != 0 {
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
			finding := findingmodel.Errorf(reporting.SourcePosition(e.Uses()), "action", "remote action %q has invalid metadata at %s:%d:%d: %s", spec, problem.Path, problem.Start.Line, problem.Start.Column, problem.Message)
			finding.Code = problem.Code
			if finding.Code == "" {
				finding.Code = string(actionmeta.CodeMetadataParse)
			}
			rule.Report(finding)
		}
	}
	rule.Report(actioncheck.Repository(e, inspection.Definition())...)
	return nil
}

// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#example-using-action-in-the-same-repository-as-the-workflow
// Agents: https://docs.github.com/api/article/body?pathname=/en/actions/reference/workflows-and-actions/workflow-syntax
func (rule *ruleAction) checkLocalAction(localSpec, displaySpec string, action workflowdecl.ExecActionView) error {
	if strings.HasPrefix(displaySpec, "$/") {
		localSpec = displaySpec
	}
	meta, _, source, err := rule.cache.FindMetadataDocument(localSpec)
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if err != nil {
		if _, unresolved := errors.AsType[workspaceactions.UnresolvedError](err); unresolved {
			rule.Report(findingmodel.At(reporting.SourcePosition(action.Uses()), "missing-local-action", err.Error()))
		} else {
			finding := findingmodel.At(reporting.SourcePosition(action.Uses()), rule.Name(), err.Error())
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

func (rule *ruleAction) ForMetadata(actions *workspaceactions.Resolver) workflowvisit.Rule {
	child := NewRuleAction(rule.context, actions, rule.remote)
	child.validation = rule.validation
	return child
}

func (rule *ruleAction) CheckStandaloneMetadata(meta *actionmeta.Metadata, snapshot parsing.SourceSnapshot) error {
	if snapshot.Kind() != parsing.KindAction {
		return errors.New("standalone metadata has no admitted source snapshot")
	}
	if original, parsed := meta.OriginalSource(); !parsed || original != string(snapshot.Content()) {
		return errors.New("standalone metadata does not belong to its source snapshot")
	}
	rule.Report(actioncheck.StandaloneWithAdvisories(meta)...)
	rule.AnnotateSince(0, func(finding *findingmodel.Finding) {
		finding.MetadataIdentity = &findingmodel.MetadataIdentity{Key: metadataFindingKey(meta, snapshot.Reference().Document, finding), Declaration: true}
	})
	return nil
}

var _ checkcalls.MetadataParticipant = (*ruleAction)(nil)
