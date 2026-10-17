package builtin

import (
	"errors"
	"fmt"
	"strings"

	actionmeta "actionlint.kjanat.dev/internal/action"
	actionlocal "actionlint.kjanat.dev/internal/action/local"
	actioncheck "actionlint.kjanat.dev/internal/action/validation"
	"actionlint.kjanat.dev/internal/actioncatalog"
	findingmodel "actionlint.kjanat.dev/internal/finding"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
	"actionlint.kjanat.dev/internal/workflow/references"
	workflowvisit "actionlint.kjanat.dev/internal/workflow/visit"
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
		rule.checkLocalAction(spec, spec, e)
		return nil
	}

	if strings.HasPrefix(spec, "$/") {
		// Relative to the repository and commit that contain the running workflow.
		// Normalize it to the existing local path form so metadata and output validation still work.
		local, ok := references.SelfRepositoryLocal(spec)
		if !ok {
			rule.Findings = append(rule.Findings, actioncheck.InvalidReference(e.Uses.Pos, spec, "self-repository references do not support an '@ref' suffix"))
			return nil
		}
		rule.checkLocalAction(local, spec, e)
		return nil
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
func (rule *RuleAction) checkLocalAction(localSpec, displaySpec string, action *workflowlang.ExecAction) {
	if strings.HasPrefix(displaySpec, "$/") {
		localSpec = displaySpec
	}
	meta, _, err := rule.cache.FindMetadata(localSpec)
	if err != nil {
		if _, unresolved := errors.AsType[actionlocal.UnresolvedError](err); unresolved {
			rule.Findings = append(rule.Findings, findingmodel.At(action.Uses.Pos, "missing-local-action", err.Error()))
		} else {
			finding := findingmodel.At(action.Uses.Pos, rule.Name(), err.Error())
			if parseError, ok := errors.AsType[*actionlocal.ParseError](err); ok {
				finding.MetadataParsePath = parseError.Path
			}
			rule.Findings = append(rule.Findings, finding)
		}
		return
	}
	if meta == nil {
		return
	}

	rule.replayMetadataFindings(meta, action, displaySpec)

	rule.Findings = append(rule.Findings, actioncheck.Inputs(meta, action, func(m *actionmeta.Metadata) string {
		return fmt.Sprintf("%q defined at %q", m.Name, displaySpec)
	})...)
}

func (rule *RuleAction) ForMetadata(actions *actionlocal.Resolver) workflowvisit.Rule {
	child := NewRuleAction(actions)
	child.validation = rule.validation
	child.SetConfig(rule.Config())
	return child
}

func (rule *RuleAction) CheckStandaloneMetadata(meta *actionmeta.Metadata) {
	rule.Findings = append(rule.Findings, actioncheck.Standalone(meta)...)
	for _, finding := range rule.Findings {
		finding.MetadataIdentity = &findingmodel.MetadataIdentity{Key: metadataFindingKey(meta, finding), Declaration: true}
	}
}
