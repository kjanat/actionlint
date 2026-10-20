package cachepolicy

import (
	"fmt"
	"slices"

	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/stringutil"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/internal/workflow/references"
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

// Trust evaluates cache grants against the workflow's triggering events.
// Each shared declaration is reported at its first reachable job.
type Trust struct {
	workflow  *workflowlang.CacheMode
	events    []string
	reported  map[*workflowlang.CacheMode]bool
	reachable map[*workflowlang.CacheMode][]string
}

// NewTrust prepares event reachability and shared cache declarations for one workflow.
func NewTrust(w *workflowlang.Workflow) *Trust {
	r := &Trust{workflow: w.CacheMode, events: cacheRestrictedEvents(w), reported: map[*workflowlang.CacheMode]bool{}, reachable: map[*workflowlang.CacheMode][]string{}}
	for _, job := range w.Jobs {
		mode := workflowlang.EffectiveCacheMode(r.workflow, job.CacheMode)
		access, valid := mode.Capabilities()
		if valid && access&2 != 0 {
			r.reachable[mode] = append(r.reachable[mode], cacheReachableEvents(r.events, job)...)
		}
	}
	return r
}

// CheckWrites reports effective write grants reachable from low-trust events.
func (r *Trust) CheckWrites(job *workflowlang.Job) []diagnostic.Diagnostic {
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
	return []diagnostic.Diagnostic{report(mode.Pos, "cache-write-untrusted", "cache-mode %q grants cache writes for low-trust trigger(s) %s. untrusted code or input can poison caches consumed by privileged workflows. use \"read\" or \"none\", or document a reviewed exception with an inline suppression", mode.Kind.String(), stringutil.SortedQuotes(events))}
}

// CheckCall requires an explicit cache ceiling on low-trust reusable calls.
func (r *Trust) CheckCall(job *workflowlang.Job) []diagnostic.Diagnostic {
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
	return []diagnostic.Diagnostic{report(call.Uses.Pos, "cache-call-unrestricted", "reusable workflow call %q has no explicit cache access limit for low-trust trigger(s) %s. the callee can request writes despite the trigger's read-only default. set \"cache-mode: read\" or \"cache-mode: none\" on this job or workflow", call.Uses.Value, stringutil.SortedQuotes(events))}
}

func report(pos *workflowlang.Pos, kind, format string, args ...any) diagnostic.Diagnostic {
	return diagnostic.Diagnostic{Rule: kind, Message: fmt.Sprintf(format, args...), Start: diagnostic.Position{Line: pos.Line, Column: pos.Col}}
}
