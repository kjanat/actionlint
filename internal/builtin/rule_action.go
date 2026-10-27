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
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/internal/workflow/references"
	workflowvisit "actionlint.kjanat.dev/v2/internal/workflow/visit"
)

// RuleAction is a rule to check running action in steps of jobs.
// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#jobsjob_idstepsuses
// Agents: https://docs.github.com/api/article/body?pathname=/en/actions/reference/workflows-and-actions/workflow-syntax
type RuleAction struct {
	workflowvisit.Base
	ruleLogger
	Configuration
	cache      actionlocal.MetadataLookup
	validation *actionMetadataValidation
}

// NewRuleAction creates new RuleAction instance.
func NewRuleAction(cache actionlocal.MetadataLookup) *RuleAction {
	validation := newActionMetadataValidation()

	return &RuleAction{
		Base: builtinRuleBase("action"), tag: "action",
		cache:      cache,
		validation: validation,
	}
}

// VisitStep is callback when visiting Step node.
func (rule *RuleAction) VisitStep(n *workflowlang.Step) error {
	e, ok := n.Exec.(*workflowlang.ExecAction)
	if !ok || e.Uses == nil {
		return nil
	}

	if e.Uses.ContainsExpression() {
		// Cannot parse specification made with interpolation. Give up
		return nil
	}

	spec := e.Uses.Value

	if strings.HasPrefix(spec, "./") {
		// Relative to repository root
		return rule.checkLocalAction(spec, spec, e)
	}

	if strings.HasPrefix(spec, "$/") {
		// Relative to the repository and commit that contain the running workflow.
		// Normalize it to the existing local path form so metadata and output validation still work.
		local, ok := references.SelfRepositoryLocal(spec)
		if !ok {
			rule.Findings = append(rule.Findings, actioncheck.InvalidReference(e.Uses.Pos, spec, "self-repository references do not support an '@ref' suffix"))
			return nil
		}
		return rule.checkLocalAction(local, spec, e)
	}

	if strings.HasPrefix(spec, "docker://") {
		rule.Findings = append(rule.Findings, actioncheck.Docker(e)...)
		return nil
	}

	rule.Findings = append(rule.Findings, actioncheck.Repository(e, actioncatalog.Lookup, rule.Debug)...)
	return nil
}

// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#example-using-action-in-the-same-repository-as-the-workflow
// Agents: https://docs.github.com/api/article/body?pathname=/en/actions/reference/workflows-and-actions/workflow-syntax
func (rule *RuleAction) checkLocalAction(localSpec, displaySpec string, action *workflowlang.ExecAction) error {
	if strings.HasPrefix(displaySpec, "$/") {
		localSpec = displaySpec
	}
	meta, source, err := rule.findMetadataDocument(localSpec)
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if err != nil {
		if _, unresolved := errors.AsType[actionlocal.UnresolvedError](err); unresolved {
			rule.Findings = append(rule.Findings, findingmodel.At(action.Uses.Pos, "missing-local-action", err.Error()))
		} else {
			finding := findingmodel.At(action.Uses.Pos, rule.Name(), err.Error())
			if parseError, ok := errors.AsType[*actionlocal.ParseError](err); ok {
				finding.MetadataParsePath = parseError.Path
				finding.MetadataParseSource = source.Capture().Revision()
				if !finding.MetadataParseSource.Valid() {
					return fmt.Errorf("metadata parse source %q has no captured revision: %w", parseError.Path, err)
				}
			}
			rule.Findings = append(rule.Findings, finding)
		}
		return nil
	}
	if meta == nil {
		return nil
	}

	if err := rule.replayMetadataFindings(meta, source, action, displaySpec); err != nil {
		return err
	}

	rule.Findings = append(rule.Findings, actioncheck.Inputs(meta, action, func(m *actionmeta.Metadata) string {
		return fmt.Sprintf("%q defined at %q", m.Name, displaySpec)
	})...)
	return nil
}

func (rule *RuleAction) ForMetadata(actions *actionlocal.Resolver) workflowvisit.Rule {
	child := NewRuleAction(actions)
	child.validation = rule.validation
	child.SetConfig(rule.Config())
	return child
}

func (rule *RuleAction) CheckStandaloneMetadata(meta *actionmeta.Metadata, snapshot *documentparse.SourceSnapshot) error {
	if snapshot == nil || snapshot.Kind() != documentparse.KindAction {
		return errors.New("standalone metadata has no admitted source snapshot")
	}
	admitted, err := snapshot.Registry().Action(snapshot.Capture(), meta)
	if err != nil {
		return err
	}
	rule.Findings = append(rule.Findings, actioncheck.Standalone(meta)...)
	for _, finding := range rule.Findings {
		finding.MetadataIdentity = &findingmodel.MetadataIdentity{Key: metadataFindingKey(meta, admitted.Reference().Document, finding), Declaration: true}
	}
	return nil
}

func (rule *RuleAction) findMetadataDocument(spec string) (*actionmeta.Metadata, actionlocal.SourceDocument, error) {
	switch cache := rule.cache.(type) {
	case interface {
		FindMetadataDocument(string) (*actionmeta.Metadata, bool, actionlocal.SourceDocument, error)
	}:
		metadata, _, document, err := cache.FindMetadataDocument(spec)
		return metadata, document, err
	default:
		metadata, _, err := rule.cache.FindMetadata(spec)
		return metadata, actionlocal.SourceDocument{}, err
	}
}
