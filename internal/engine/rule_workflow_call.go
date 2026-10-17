package engine

import (
	"fmt"
	"slices"
	"strings"

	"actionlint.kjanat.dev/internal/workflow/calls"
	"actionlint.kjanat.dev/internal/workflow/references"

	stringutil "actionlint.kjanat.dev/internal/stringutil"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
)

// RuleWorkflowCall is a rule checker to check workflow call at jobs.<job_id>.
type RuleWorkflowCall struct {
	RuleBase
	ruleLogger
	ruleConfiguration
	workflowCallEventPos *workflowlang.Pos
	workflowPath         string
	workflowPermissions  *workflowlang.Permissions
	workflowCacheMode    *workflowlang.CacheMode
	cache                *calls.Cache
}

// NewRuleWorkflowCall creates a new RuleWorkflowCall instance. 'workflowPath' is a file path to
// the workflow which is relative to a project root directory or an absolute path.
func NewRuleWorkflowCall(workflowPath string, cache *calls.Cache) *RuleWorkflowCall {
	return &RuleWorkflowCall{
		RuleBase: builtinRuleBase("workflow-call"), tag: "workflow-call",
		workflowCallEventPos: nil,
		workflowPath:         workflowPath,
		cache:                cache,
	}
}

// VisitWorkflowPre is callback when visiting Workflow node before visiting its children.
func (rule *RuleWorkflowCall) VisitWorkflowPre(n *workflowlang.Workflow) error {
	rule.workflowPermissions = n.Permissions
	rule.workflowCacheMode = n.CacheMode
	for _, e := range n.On {
		if e, ok := e.(*workflowlang.WorkflowCallEvent); ok {
			rule.workflowCallEventPos = e.Pos
			// Register this reusable workflow in cache so that it does not need to parse this workflow
			// file again when this workflow is called by other workflows.
			rule.cache.WriteWorkflowCallEventFromWorkflow(rule.workflowPath, e, n)
			break
		}
	}
	return nil
}

// VisitJobPre is callback when visiting Job node before visiting its children.
func (rule *RuleWorkflowCall) VisitJobPre(n *workflowlang.Job) error {
	if n.WorkflowCall == nil {
		return nil
	}

	u := n.WorkflowCall.Uses
	if u == nil || u.Value == "" || u.ContainsExpression() {
		return nil
	}

	if local, ok := references.LocalWorkflow(u.Value); ok {
		rule.checkWorkflowCallUsesLocal(n.WorkflowCall, n.Permissions, n.CacheMode, local)
		return nil
	}

	if _, ok := references.WorkflowRef(u.Value); ok {
		return nil
	}

	if strings.HasPrefix(u.Value, "./") || strings.HasPrefix(u.Value, "$/") {
		// When the specification is invalid and it is local reusable workflow call, remember it caused
		// an error by setting `nil` to cache. This can prevent redundant 'could not read workflow call'
		// error.
		rule.cache.IgnoreInvalid(u.Value)
	}

	rule.Errorf(
		u.Pos,
		"reusable workflow call %q at \"uses\" is not following the format \"owner/repo/path/to/workflow.yml@ref\", \"./path/to/workflow.yml\", nor \"$/path/to/workflow.yml\". see https://docs.github.com/en/actions/learn-github-actions/reusing-workflows for more details",
		u.Value,
	)
	return nil
}

func (rule *RuleWorkflowCall) checkWorkflowCallUsesLocal(call *workflowlang.WorkflowCall, jobPerms *workflowlang.Permissions, jobCacheMode *workflowlang.CacheMode, localSpec string) {
	u := call.Uses
	m, err := rule.cache.FindMetadataForCall(u.Value)
	if err != nil {
		rule.errs = append(rule.errs, workflowLookupError(u, err))
		return
	}
	if m == nil {
		rule.Debug("Skip workflow call %q since no metadata was found", u.Value)
		return
	}

	// Validate inputs
	for n, i := range m.Inputs {
		if i != nil && i.Required {
			if _, ok := call.Inputs[n]; !ok {
				rule.Errorf(u.Pos, "input %q is required by %q reusable workflow", i.Name, u.Value)
			}
		}
	}
	for n, i := range call.Inputs {
		if _, ok := m.Inputs[n]; !ok {
			note := "no input is defined"
			if len(m.Inputs) > 0 {
				is := make([]string, 0, len(m.Inputs))
				for _, i := range m.Inputs {
					is = append(is, i.Name)
				}
				if len(is) == 1 {
					note = fmt.Sprintf("defined input is %q", is[0])
				} else {
					note = "defined inputs are " + stringutil.SortedQuotes(is)
				}
			}
			rule.Errorf(i.Name.Pos, "input %q is not defined in %q reusable workflow. %s", i.Name.Value, u.Value, note)
		}
	}

	// Validate secrets
	if !call.InheritSecrets {
		for n, s := range m.Secrets {
			if s.Required {
				if _, ok := call.Secrets[n]; !ok {
					rule.Errorf(u.Pos, "secret %q is required by %q reusable workflow", s.Name, u.Value)
				}
			}
		}
		for n, s := range call.Secrets {
			if _, ok := m.Secrets[n]; !ok {
				note := "no secret is defined"
				if len(m.Secrets) > 0 {
					ss := make([]string, 0, len(m.Secrets))
					for _, s := range m.Secrets {
						ss = append(ss, s.Name)
					}
					if len(ss) == 1 {
						note = fmt.Sprintf("defined secret is %q", ss[0])
					} else {
						note = "defined secrets are " + stringutil.SortedQuotes(ss)
					}
				}
				rule.Errorf(s.Name.Pos, "secret %q is not defined in %q reusable workflow. %s", s.Name.Value, u.Value, note)
			}
		}
	}

	rule.checkWorkflowCallPermissions(call, jobPerms, m)
	rule.checkWorkflowCallCacheMode(call.Uses.Pos, localSpec, call.Uses.Value, workflowlang.EffectiveCacheMode(rule.workflowCacheMode, jobCacheMode), m, map[workflowCacheModeVisit]bool{})

	rule.Debug("Validated reusable workflow %q", u.Value)
}

// checkWorkflowCallPermissions compares the permissions each job of the called workflow requires
// against the permissions the calling job grants. GitHub validates this when the workflow is loaded,
// so a shortfall aborts the whole run before any job starts, including any "if: failure()" handler.
// "if:" on a job of the called workflow does not affect the comparison.
func (rule *RuleWorkflowCall) checkWorkflowCallPermissions(call *workflowlang.WorkflowCall, jobPerms *workflowlang.Permissions, m *calls.Metadata) {
	if len(m.JobPermissions) == 0 {
		return
	}

	var granted calls.ResolvedPermissions
	switch {
	case jobPerms != nil:
		granted = calls.ResolvePermissionsAST(jobPerms)
	case rule.workflowPermissions != nil:
		granted = calls.ResolvePermissionsAST(rule.workflowPermissions)
	default:
		a := DefaultPermissionsAssumptionUnset
		if cfg := rule.Config(); cfg != nil {
			a = cfg.AssumeDefaultPermissions
		}
		granted = calls.ResolvedPermissions{Kind: calls.PermissionsDeclared, Levels: calls.DefaultPermissionLevels(a == DefaultPermissionsAssumptionPermissive)}
	}
	if granted.Kind != calls.PermissionsDeclared {
		return
	}

	ids := make([]string, 0, len(m.JobPermissions))
	for id := range m.JobPermissions {
		ids = append(ids, id)
	}
	slices.Sort(ids)

	for _, id := range ids {
		required := m.JobPermissions[id]
		ss := make([]string, 0, len(required))
		for s := range required {
			ss = append(ss, s)
		}
		slices.Sort(ss)

		want, have := []string{}, []string{}
		for _, s := range ss {
			if granted.Levels[s] >= required[s] {
				continue
			}
			want = append(want, s+": "+required[s].String())
			have = append(have, s+": "+granted.Levels[s].String())
		}
		if len(want) == 0 {
			continue
		}

		rule.Errorf(
			call.Uses.Pos,
			"nested job %q of %q requires %s but the calling job grants %s",
			id,
			call.Uses.Value,
			stringutil.Quotes(want),
			stringutil.Quotes(have),
		)
	}
}

// workflowLookupError assigns dependency failures to their owning rule, even
// when expression inference requested the same metadata. Selection and
// suppression consequently do not depend on which consumer ran first.
func workflowLookupError(uses *workflowlang.String, err error) *Error {
	return errorAt(uses.Pos, "workflow-call", err.Error())
}
