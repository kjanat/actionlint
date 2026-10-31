package builtin

import (
	"context"
	"errors"
	"fmt"
	"strings"

	actionmeta "actionlint.kjanat.dev/v2/internal/action"
	actionlocal "actionlint.kjanat.dev/v2/internal/action/local"
	actioncheck "actionlint.kjanat.dev/v2/internal/action/validation"
	"actionlint.kjanat.dev/v2/internal/actioncatalog"
	"actionlint.kjanat.dev/v2/internal/documentparse"
	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
	workflowdecl "actionlint.kjanat.dev/v2/internal/workflow/declaration"
	"actionlint.kjanat.dev/v2/internal/workflow/references"
	workflowvisit "actionlint.kjanat.dev/v2/internal/workflow/visit"
)

// ruleAction is a rule to check running action in steps of jobs.
// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#jobsjob_idstepsuses
// Agents: https://docs.github.com/api/article/body?pathname=/en/actions/reference/workflows-and-actions/workflow-syntax
type ruleAction struct {
	workflowvisit.Base
	ruleLogger
	cache      actionlocal.MetadataDocumentLookup
	validation *actionMetadataValidation
}

// NewRuleAction checks the actions that steps use, reading local action
// metadata through cache.
func NewRuleAction(cache actionlocal.MetadataDocumentLookup) MetadataParticipant {
	return newRuleAction(cache)
}

func newRuleAction(cache actionlocal.MetadataDocumentLookup) *ruleAction {
	validation := newActionMetadataValidation()

	return &ruleAction{
		Base: builtinRuleBase("action"), tag: "action",
		cache:      cache,
		validation: validation,
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

	if strings.HasPrefix(spec, "./") {
		// Relative to repository root
		return rule.checkLocalAction(spec, spec, e)
	}

	if strings.HasPrefix(spec, "$/") {
		// Relative to the repository and commit that contain the running workflow.
		// Normalize it to the existing local path form so metadata and output validation still work.
		local, ok := references.SelfRepositoryLocal(spec)
		if !ok {
			rule.Report(actioncheck.InvalidReference(declarationPosition(e.Uses()), spec, "self-repository references do not support an '@ref' suffix"))
			return nil
		}
		return rule.checkLocalAction(local, spec, e)
	}

	if strings.HasPrefix(spec, "docker://") {
		rule.Report(actioncheck.Docker(e)...)
		return nil
	}

	rule.Report(actioncheck.Repository(e, actioncatalog.Lookup, rule.Debug)...)
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
		if _, unresolved := errors.AsType[actionlocal.UnresolvedError](err); unresolved {
			rule.Report(findingmodel.At(declarationPosition(action.Uses()), "missing-local-action", err.Error()))
		} else {
			finding := findingmodel.At(declarationPosition(action.Uses()), rule.Name(), err.Error())
			if parseError, ok := errors.AsType[*actionlocal.ParseError](err); ok {
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

func (rule *ruleAction) ForMetadata(actions *actionlocal.Resolver) workflowvisit.Rule {
	child := newRuleAction(actions)
	child.validation = rule.validation
	return child
}

func (rule *ruleAction) CheckStandaloneMetadata(meta *actionmeta.Metadata, snapshot documentparse.SourceSnapshot) error {
	if snapshot.Kind() != documentparse.KindAction {
		return errors.New("standalone metadata has no admitted source snapshot")
	}
	if original, parsed := meta.OriginalSource(); !parsed || original != string(snapshot.Content()) {
		return errors.New("standalone metadata does not belong to its source snapshot")
	}
	rule.Report(actioncheck.Standalone(meta)...)
	rule.AnnotateSince(0, func(finding *findingmodel.Finding) {
		finding.MetadataIdentity = &findingmodel.MetadataIdentity{Key: metadataFindingKey(meta, snapshot.Reference().Document, finding), Declaration: true}
	})
	return nil
}
