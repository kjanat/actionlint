package actionlint

import (
	"slices"
	"strings"
)

type cacheEventClass uint8

const (
	cacheEventUnknown cacheEventClass = iota
	cacheEventRestricted
	cacheEventTrusted
	cacheEventRefScoped
	cacheEventInherited
)

// cacheEventClasses records a cache trust decision for every generated webhook.
// Ref-scoped events use a created ref, release tag, PR merge ref, or merge-group ref.
//
// workflow_call inherits its caller's context.
//
// - https://docs.github.com/en/actions/reference/workflows-and-actions/dependency-caching#cache-access-for-low-trust-workflow-triggers
//
// - https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows
var cacheEventClasses = map[string]cacheEventClass{
	"branch_protection_rule":      cacheEventRestricted,
	"check_run":                   cacheEventRestricted,
	"check_suite":                 cacheEventRestricted,
	"create":                      cacheEventRefScoped,
	"delete":                      cacheEventTrusted,
	"deployment":                  cacheEventRestricted,
	"deployment_status":           cacheEventRestricted,
	"discussion":                  cacheEventRestricted,
	"discussion_comment":          cacheEventRestricted,
	"fork":                        cacheEventRestricted,
	"gollum":                      cacheEventRestricted,
	"image_version":               cacheEventRestricted,
	"issue_comment":               cacheEventRestricted,
	"issues":                      cacheEventRestricted,
	"label":                       cacheEventRestricted,
	"merge_group":                 cacheEventRefScoped,
	"milestone":                   cacheEventRestricted,
	"page_build":                  cacheEventTrusted,
	"public":                      cacheEventRestricted,
	"pull_request":                cacheEventRefScoped,
	"pull_request_review":         cacheEventRefScoped,
	"pull_request_review_comment": cacheEventRefScoped,
	"pull_request_target":         cacheEventRestricted,
	"push":                        cacheEventTrusted,
	"registry_package":            cacheEventTrusted,
	"release":                     cacheEventRefScoped,
	"repository_dispatch":         cacheEventTrusted,
	"schedule":                    cacheEventTrusted,
	"status":                      cacheEventRestricted,
	"watch":                       cacheEventRestricted,
	"workflow_call":               cacheEventInherited,
	"workflow_dispatch":           cacheEventTrusted,
	"workflow_run":                cacheEventRestricted,
}

// cacheRestrictedEvents includes unclassified triggers conservatively.
//
// Inventory tests require an explicit decision before a new generated event can ship.
func cacheRestrictedEvents(w *Workflow) []string {
	var events []string
	for _, event := range w.On {
		name := event.EventName()
		switch cacheEventClasses[name] {
		case cacheEventTrusted, cacheEventRefScoped, cacheEventInherited:
			continue
		default:
			events = append(events, name)
		}
	}
	slices.Sort(events)
	return slices.Compact(events)
}

// RuleCacheWriteUntrusted checks write grants on triggers with restricted cache defaults.
type RuleCacheWriteUntrusted struct {
	RuleBase
	workflow *CacheMode
	events   []string
	reported map[*CacheMode]bool
}

// NewRuleCacheWriteUntrusted creates a cache-write-untrusted rule.
func NewRuleCacheWriteUntrusted() *RuleCacheWriteUntrusted {
	return &RuleCacheWriteUntrusted{RuleBase: NewRuleBase("cache-write-untrusted", "Checks cache write grants on low-trust triggers")}
}

// VisitWorkflowPre records trigger and workflow defaults.
func (r *RuleCacheWriteUntrusted) VisitWorkflowPre(w *Workflow) error {
	r.workflow, r.events = w.CacheMode, cacheRestrictedEvents(w)
	r.reported = map[*CacheMode]bool{}
	return nil
}

// VisitJobPre checks the job's effective declaration, including reusable calls.
func (r *RuleCacheWriteUntrusted) VisitJobPre(job *Job) error {
	mode := effectiveCacheMode(r.workflow, job.CacheMode)
	access, valid := mode.capabilities()
	if len(r.events) == 0 || !valid || access&2 == 0 || r.reported[mode] {
		return nil
	}
	r.reported[mode] = true
	r.Errorf(mode.Pos, "cache-mode %q grants cache writes for low-trust trigger(s) %s. untrusted code or input can poison caches consumed by privileged workflows. use \"read\" or \"none\", or document a reviewed exception with an inline suppression", mode.Kind.String(), sortedQuotes(r.events))
	return nil
}

// RuleCacheCallUnrestricted checks explicit cache limits on low-trust reusable calls.
type RuleCacheCallUnrestricted struct {
	RuleBase
	workflow *CacheMode
	events   []string
}

// NewRuleCacheCallUnrestricted creates a cache-call-unrestricted rule.
func NewRuleCacheCallUnrestricted() *RuleCacheCallUnrestricted {
	return &RuleCacheCallUnrestricted{RuleBase: NewRuleBase("cache-call-unrestricted", "Checks cache access ceilings on low-trust reusable workflow calls")}
}

// VisitWorkflowPre records trigger and workflow defaults.
func (r *RuleCacheCallUnrestricted) VisitWorkflowPre(w *Workflow) error {
	r.workflow, r.events = w.CacheMode, cacheRestrictedEvents(w)
	return nil
}

// VisitJobPre requires an explicit ceiling at each affected call site.
func (r *RuleCacheCallUnrestricted) VisitJobPre(job *Job) error {
	call := job.WorkflowCall
	if len(r.events) == 0 || call == nil || call.Uses == nil || call.Uses.ContainsExpression() || effectiveCacheMode(r.workflow, job.CacheMode) != nil {
		return nil
	}
	if _, local := workflowCallUsesLocalSpec(call.Uses.Value); !local {
		if _, remote := workflowCallUsesRepoRef(call.Uses.Value); !remote {
			return nil
		}
	}
	r.Errorf(call.Uses.Pos, "reusable workflow call %q has no explicit cache access limit for low-trust trigger(s) %s. the callee can request writes despite the trigger's read-only default. set \"cache-mode: read\" or \"cache-mode: none\" on this job or workflow", call.Uses.Value, sortedQuotes(r.events))
	return nil
}

// RuleCacheOperation checks cache actions which an explicit mode makes ineffective.
type RuleCacheOperation struct {
	RuleBase
	workflow *CacheMode
	job      *CacheMode
}

// NewRuleCacheOperation creates a cache-operation rule.
func NewRuleCacheOperation() *RuleCacheOperation {
	return &RuleCacheOperation{RuleBase: NewRuleBase("cache-operation", "Checks cache actions disabled by explicit cache access modes")}
}

// VisitWorkflowPre records the workflow's explicit mode.
func (r *RuleCacheOperation) VisitWorkflowPre(w *Workflow) error {
	r.workflow = w.CacheMode
	return nil
}

// VisitJobPre resolves job overrides.
func (r *RuleCacheOperation) VisitJobPre(job *Job) error {
	r.job = effectiveCacheMode(r.workflow, job.CacheMode)
	return nil
}

// VisitStep checks the three official cache action entry points.
func (r *RuleCacheOperation) VisitStep(step *Step) error {
	action, ok := step.Exec.(*ExecAction)
	if !ok || action.Uses == nil {
		return nil
	}
	access, valid := r.job.capabilities()
	if !valid {
		return nil
	}
	name, ref, ok := strings.Cut(action.Uses.Value, "@")
	if !ok || ref == "" || ContainsExpression(action.Uses.Value) {
		return nil
	}
	owner, rest, ok := strings.Cut(name, "/")
	if !ok || !strings.EqualFold(owner, "actions") {
		return nil
	}
	repo, path, hasPath := strings.Cut(rest, "/")
	if !strings.EqualFold(repo, "cache") || hasPath && path == "" {
		return nil
	}
	// Repository names are case-insensitive; action subpaths address files in the downloaded repository and retain their case.
	operation := ""
	switch path {
	case "save":
		if access&2 == 0 {
			operation = "save caches"
		}
	case "restore":
		if access&1 == 0 {
			operation = "restore caches"
		}
	case "":
		if access == 0 {
			operation = "restore or save caches"
		}
	}
	if operation != "" {
		r.Errorf(action.Uses.Pos, "%q cannot %s with effective cache-mode %q. GitHub skips the operation; remove this cache step or select an action and mode that match the job's intended cache access", name, operation, r.job.Kind.String())
	}
	return nil
}
