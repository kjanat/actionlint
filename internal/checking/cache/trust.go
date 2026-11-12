package cachepolicy

import (
	"cmp"
	"fmt"
	"slices"

	cachemodel "actionlint.kjanat.dev/v2/internal/model/workflows/cache"
	workflowconditions "actionlint.kjanat.dev/v2/internal/model/workflows/conditions"

	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/model/calls/references"
	declaration "actionlint.kjanat.dev/v2/internal/model/workflows"
	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
	"actionlint.kjanat.dev/v2/internal/results"
	"actionlint.kjanat.dev/v2/internal/results/messages"
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
func cacheRestrictedEvents(w declaration.WorkflowView) []string {
	var events []string
	for event := range w.On() {
		name := event.EventName()
		switch cacheEventClasses[name] {
		case cacheEventTrusted, cacheEventRefScoped, cacheEventInherited:
			continue
		case cacheEventUnknown, cacheEventRestricted:
		}
		events = append(events, name)
	}
	slices.Sort(events)
	return slices.Compact(events)
}

// Trust evaluates cache grants against the workflow's triggering events.
// Each shared declaration is assigned to its first reachable job in source order.
// Construction prepares that assignment; checks do not mutate it.
type Trust struct {
	workflow     declaration.CacheModeView
	events       []string
	declarations []cacheDeclaration
}
type cacheDeclaration struct {
	mode      declaration.CacheModeView
	job       declaration.JobView
	reachable []string
}

func (r *Trust) declaration(mode declaration.CacheModeView, job declaration.JobView) *cacheDeclaration {
	for i := range r.declarations {
		if r.declarations[i].mode.SameDeclaration(mode) {
			return &r.declarations[i]
		}
	}
	r.declarations = append(r.declarations, cacheDeclaration{mode: mode, job: job})
	return &r.declarations[len(r.declarations)-1]
}

// NewTrust prepares event reachability and shared cache declarations for one workflow.
func NewTrust(w declaration.WorkflowView) *Trust {
	r := &Trust{workflow: w.CacheMode(), events: cacheRestrictedEvents(w)}
	var jobs []declaration.JobView
	for _, job := range w.Jobs() {
		jobs = append(jobs, job)
	}
	slices.SortFunc(jobs, func(a, b declaration.JobView) int {
		left, right := position(a), position(b)
		if order := cmp.Compare(left.Line, right.Line); order != 0 {
			return order
		}
		if order := cmp.Compare(left.Col, right.Col); order != 0 {
			return order
		}
		return cmp.Compare(a.ID().Value(), b.ID().Value())
	})
	for _, job := range jobs {
		mode := cachemodel.EffectiveMode(r.workflow, job.CacheMode())
		access, valid := cachemodel.FromDeclaration(mode).Capabilities()
		if valid && access&2 != 0 {
			events := workflowconditions.ReachableEvents(r.events, job.If().Value())
			if len(events) != 0 {
				declaration := r.declaration(mode, job)
				declaration.reachable = append(declaration.reachable, events...)
			}
		}
	}
	for i := range r.declarations {
		events := r.declarations[i].reachable
		slices.Sort(events)
		r.declarations[i].reachable = slices.Compact(events)
	}
	return r
}

// CheckWrites reports effective write grants reachable from low-trust events.
func (r *Trust) CheckWrites(job declaration.JobView) []results.Diagnostic {
	for _, declaration := range r.declarations {
		if declaration.job == job {
			mode := declaration.mode
			return []results.Diagnostic{report(position(mode), "cache-write-untrusted", "cache-mode %q grants cache writes for low-trust trigger(s) %s. untrusted code or input can poison caches consumed by privileged workflows. use \"read\" or \"none\", or document a reviewed exception with an inline suppression", mode.Kind().String(), messages.SortedQuotes(declaration.reachable))}
		}
	}
	return nil
}

// CheckCall requires an explicit cache ceiling on low-trust reusable calls.
func (r *Trust) CheckCall(job declaration.JobView) []results.Diagnostic {
	call := job.WorkflowCall()
	if len(r.events) == 0 || !call.Present() || !call.Uses().Present() || call.Uses().ContainsExpression() || cachemodel.EffectiveMode(r.workflow, job.CacheMode()).Present() {
		return nil
	}
	if _, local := references.LocalWorkflow(call.Uses().Value()); !local {
		if _, remote := references.WorkflowRef(call.Uses().Value()); !remote {
			return nil
		}
	}
	events := workflowconditions.ReachableEvents(r.events, job.If().Value())
	if len(events) == 0 {
		return nil
	}
	return []results.Diagnostic{report(position(call.Uses()), "cache-call-unrestricted", "reusable workflow call %q has no explicit cache access limit for low-trust trigger(s) %s. the callee can request writes despite the trigger's read-only default. set \"cache-mode: read\" or \"cache-mode: none\" on this job or workflow", call.Uses().Value(), messages.SortedQuotes(events))}
}

func report(pos workflowlang.Pos, kind, format string, args ...any) results.Diagnostic {
	return results.Diagnostic{Rule: kind, Message: fmt.Sprintf(format, args...), Start: sourcepos.Position{Line: pos.Line, Column: pos.Col}}
}
