package engine

import (
	"maps"
	"slices"
	"strings"

	"actionlint.kjanat.dev/internal/workflow/calls"
	"actionlint.kjanat.dev/internal/workflow/references"

	stringutil "actionlint.kjanat.dev/internal/stringutil"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
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
func cacheRestrictedEvents(w *workflowlang.Workflow) []string {
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
	workflow  *workflowlang.CacheMode
	events    []string
	reported  map[*workflowlang.CacheMode]bool
	reachable map[*workflowlang.CacheMode][]string
}

// NewRuleCacheWriteUntrusted creates a cache-write-untrusted rule.
func NewRuleCacheWriteUntrusted() *RuleCacheWriteUntrusted {
	return &RuleCacheWriteUntrusted{RuleBase: builtinRuleBase("cache-write-untrusted")}
}

// VisitWorkflowPre records trigger and workflow defaults.
func (r *RuleCacheWriteUntrusted) VisitWorkflowPre(w *workflowlang.Workflow) error {
	r.workflow, r.events = w.CacheMode, cacheRestrictedEvents(w)
	r.reported = map[*workflowlang.CacheMode]bool{}
	r.reachable = map[*workflowlang.CacheMode][]string{}
	for _, job := range w.Jobs {
		mode := workflowlang.EffectiveCacheMode(r.workflow, job.CacheMode)
		access, valid := mode.Capabilities()
		if valid && access&2 != 0 {
			r.reachable[mode] = append(r.reachable[mode], cacheReachableEvents(r.events, job)...)
		}
	}
	return nil
}

// VisitJobPre checks the job's effective declaration, including reusable calls.
func (r *RuleCacheWriteUntrusted) VisitJobPre(job *workflowlang.Job) error {
	mode := workflowlang.EffectiveCacheMode(r.workflow, job.CacheMode)
	access, valid := mode.Capabilities()
	if len(r.events) == 0 || !valid || access&2 == 0 || r.reported[mode] {
		return nil
	}
	events := cacheReachableEvents(r.events, job)
	if len(events) == 0 {
		return nil
	}
	// Include every reachable event sharing this declaration, even when another
	// job is visited later. Emit at the first reachable job to retain its origin.
	events = append(events, r.reachable[mode]...)
	slices.Sort(events)
	events = slices.Compact(events)
	r.reported[mode] = true
	r.Errorf(mode.Pos, "cache-mode %q grants cache writes for low-trust trigger(s) %s. untrusted code or input can poison caches consumed by privileged workflows. use \"read\" or \"none\", or document a reviewed exception with an inline suppression", mode.Kind.String(), stringutil.SortedQuotes(events))
	return nil
}

// RuleCacheCallUnrestricted checks explicit cache limits on low-trust reusable calls.
type RuleCacheCallUnrestricted struct {
	RuleBase
	workflow *workflowlang.CacheMode
	events   []string
}

// NewRuleCacheCallUnrestricted creates a cache-call-unrestricted rule.
func NewRuleCacheCallUnrestricted() *RuleCacheCallUnrestricted {
	return &RuleCacheCallUnrestricted{RuleBase: builtinRuleBase("cache-call-unrestricted")}
}

// VisitWorkflowPre records trigger and workflow defaults.
func (r *RuleCacheCallUnrestricted) VisitWorkflowPre(w *workflowlang.Workflow) error {
	r.workflow, r.events = w.CacheMode, cacheRestrictedEvents(w)
	return nil
}

// VisitJobPre requires an explicit ceiling at each affected call site.
func (r *RuleCacheCallUnrestricted) VisitJobPre(job *workflowlang.Job) error {
	call := job.WorkflowCall
	if len(r.events) == 0 || call == nil || call.Uses == nil || call.Uses.ContainsExpression() || workflowlang.EffectiveCacheMode(r.workflow, job.CacheMode) != nil {
		return nil
	}
	if _, local := references.LocalWorkflow(call.Uses.Value); !local {
		if _, remote := references.WorkflowRef(call.Uses.Value); !remote {
			return nil
		}
	}
	events := cacheReachableEvents(r.events, job)
	if len(events) == 0 {
		return nil
	}
	r.Errorf(call.Uses.Pos, "reusable workflow call %q has no explicit cache access limit for low-trust trigger(s) %s. the callee can request writes despite the trigger's read-only default. set \"cache-mode: read\" or \"cache-mode: none\" on this job or workflow", call.Uses.Value, stringutil.SortedQuotes(events))
	return nil
}

// RuleCacheOperation checks cache actions which an explicit mode makes ineffective.
type RuleCacheOperation struct {
	RuleBase
	workflow *workflowlang.CacheMode
	job      *workflowlang.CacheMode
	cache    *calls.Cache
}

// NewRuleCacheOperation creates a cache-operation rule.
func NewRuleCacheOperation() *RuleCacheOperation {
	return &RuleCacheOperation{RuleBase: builtinRuleBase("cache-operation")}
}

func newRuleCacheOperation(cache *calls.Cache) *RuleCacheOperation {
	r := NewRuleCacheOperation()
	r.cache = cache
	return r
}

// VisitWorkflowPre records the workflow's explicit mode.
func (r *RuleCacheOperation) VisitWorkflowPre(w *workflowlang.Workflow) error {
	r.workflow = w.CacheMode
	return nil
}

// VisitJobPre resolves job overrides.
func (r *RuleCacheOperation) VisitJobPre(job *workflowlang.Job) error {
	r.job = workflowlang.EffectiveCacheMode(r.workflow, job.CacheMode)
	if call := job.WorkflowCall; call != nil && call.Uses != nil && r.cache != nil {
		r.checkCallOperations(call.Uses.Pos, call.Uses.Value, r.job, map[workflowCacheModeVisit]bool{})
	}
	return nil
}

// checkCallOperations reports inherited restrictions at the call site, where
// inline suppression and policy configuration apply. Metadata stores callee
// operations; each traversal evaluates them against the caller's ceiling.
func (r *RuleCacheOperation) checkCallOperations(pos *workflowlang.Pos, sourceSpec string, granted *workflowlang.CacheMode, checked map[workflowCacheModeVisit]bool) {
	spec, local := references.LocalWorkflow(sourceSpec)
	if !local {
		return
	}
	have, known := granted.Capabilities()
	if granted != nil && !known {
		return
	}
	visit := workflowCacheModeVisit{spec: spec}
	if granted != nil {
		visit.mode = granted.Kind
	}
	if checked[visit] {
		return
	}
	checked[visit] = true
	m, err := r.cache.FindMetadataForCall(sourceSpec)
	if err != nil || m == nil {
		// workflow-call owns lookup errors; they remain cached for that rule.
		return
	}
	for _, id := range slices.Sorted(maps.Keys(m.JobCacheAccess)) {
		job := m.JobCacheAccess[id]
		mode := workflowlang.EffectiveCacheMode(granted, job.Mode)
		want, valid := mode.Capabilities()
		if mode != nil && !valid || known && valid && want&^have != 0 {
			// Invalid declarations and ceiling mismatches have their own diagnostics.
			continue
		}
		if job.Mode == nil && known {
			for _, name := range job.Operations {
				if operation := disabledCacheOperation(name, have); operation != "" {
					r.Errorf(pos, "%q in job %q of %q cannot %s with caller-imposed cache-mode %q. GitHub skips the operation; select an action and mode that match the intended cache access, or document a reviewed exception at this call", name, id, sourceSpec, operation, granted.Kind.String())
				}
			}
		}
		// An explicit intermediate mode owns its downstream operation diagnostics.
		if job.Uses != "" && job.Mode == nil {
			r.checkCallOperations(pos, job.SourceUses, mode, checked)
		}
	}
}

// VisitStep checks the three official cache action entry points.
func (r *RuleCacheOperation) VisitStep(step *workflowlang.Step) error {
	action, ok := step.Exec.(*workflowlang.ExecAction)
	if !ok || action.Uses == nil {
		return nil
	}
	access, valid := r.job.Capabilities()
	if !valid {
		return nil
	}
	name := references.CacheAction(action.Uses.Value)
	if operation := disabledCacheOperation(name, access); operation != "" {
		r.Errorf(action.Uses.Pos, "%q cannot %s with effective cache-mode %q. GitHub skips the operation; remove this cache step or select an action and mode that match the job's intended cache access", name, operation, r.job.Kind.String())
	}
	return nil
}

// references.CacheAction recognizes only the official cache action entry points.

func disabledCacheOperation(name string, access uint8) string {
	if name == "" {
		return ""
	}
	_, rest, _ := strings.Cut(name, "/")
	_, path, _ := strings.Cut(rest, "/")
	switch path {
	case "save":
		if access&2 == 0 {
			return "save caches"
		}
	case "restore":
		if access&1 == 0 {
			return "restore caches"
		}
	case "":
		if access == 0 {
			return "restore or save caches"
		}
	}
	return ""
}
