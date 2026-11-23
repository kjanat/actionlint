package cachepolicy

import (
	"cmp"
	"fmt"
	"slices"

	"actionlint.kjanat.dev/v2/internal/model/workflows/cachegrant"
	"actionlint.kjanat.dev/v2/internal/model/workflows/conditions"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/languages/messages"
	"actionlint.kjanat.dev/v2/internal/model/calls/callreferences"
	"actionlint.kjanat.dev/v2/internal/model/workflows"
	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
	"actionlint.kjanat.dev/v2/internal/results"
)

type cacheEventClass uint8

const (
	cacheEventUnknown cacheEventClass = iota
	cacheEventRestricted
	cacheEventTrusted
	cacheEventRefScoped
	cacheEventInherited
)

// Ref-scoped events use a created ref, release tag, PR merge ref, or merge-group ref.
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

func cacheRestrictedEvents(w workflows.BoundWorkflow) []string {
	events := make([]string, 0, len(w.On))
	for _, event := range w.On {
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

// Trust assigns each shared declaration to its first reachable job in source order.
type Trust struct {
	workflow     *definition.CacheMode
	events       []string
	declarations []cacheDeclaration
}
type cacheDeclaration struct {
	mode      *definition.CacheMode
	job       workflows.BoundJob
	reachable []string
}

func (r *Trust) declaration(mode *definition.CacheMode, job workflows.BoundJob) *cacheDeclaration {
	for i := range r.declarations {
		if r.declarations[i].mode == mode {
			return &r.declarations[i]
		}
	}
	r.declarations = append(r.declarations, cacheDeclaration{mode: mode, job: job})
	return &r.declarations[len(r.declarations)-1]
}

func NewTrust(w workflows.BoundWorkflow) *Trust {
	r := &Trust{workflow: w.CacheMode, events: cacheRestrictedEvents(w)}
	var jobs []workflows.BoundJob
	for _, job := range w.Jobs() {
		jobs = append(jobs, job)
	}
	slices.SortFunc(jobs, func(a, b workflows.BoundJob) int {
		left, right := sourcetext.ZeroIfNil(a.Pos), sourcetext.ZeroIfNil(b.Pos)
		if order := cmp.Compare(left.Line, right.Line); order != 0 {
			return order
		}
		if order := cmp.Compare(left.Column, right.Column); order != 0 {
			return order
		}
		return cmp.Compare(a.ID.Text(), b.ID.Text())
	})
	for _, job := range jobs {
		mode := cachegrant.EffectiveMode(r.workflow, job.CacheMode)
		access, valid := cachegrant.FromDeclaration(mode).Capabilities()
		if valid && access&2 != 0 {
			events := conditions.ReachableEvents(r.events, job.If.Text())
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

func (r *Trust) CheckWrites(job workflows.BoundJob) []results.Diagnostic {
	for _, declaration := range r.declarations {
		if declaration.job == job {
			mode := declaration.mode
			return []results.Diagnostic{report(sourcetext.ZeroIfNil(mode.Pos), "cache-write-untrusted", "cache-mode %q grants cache writes for low-trust trigger(s) %s. untrusted code or input can poison caches consumed by privileged workflows. use \"read\" or \"none\", or document a reviewed exception with an inline suppression", mode.Kind.String(), messages.SortedQuotes(declaration.reachable))}
		}
	}
	return nil
}

func (r *Trust) CheckCall(job workflows.BoundJob) []results.Diagnostic {
	call := job.WorkflowCall
	if len(r.events) == 0 || call == nil || call.Uses == nil || call.Uses.ContainsExpression() || cachegrant.EffectiveMode(r.workflow, job.CacheMode) != nil {
		return nil
	}
	if _, local := callreferences.LocalWorkflow(call.Uses.Value); !local {
		if _, remote := callreferences.WorkflowRef(call.Uses.Value); !remote {
			return nil
		}
	}
	events := conditions.ReachableEvents(r.events, job.If.Text())
	if len(events) == 0 {
		return nil
	}
	return []results.Diagnostic{report(sourcetext.ZeroIfNil(call.Uses.Pos), "cache-call-unrestricted", "reusable workflow call %q has no explicit cache access limit for low-trust trigger(s) %s. the callee can request writes despite the trigger's read-only default. set \"cache-mode: read\" or \"cache-mode: none\" on this job or workflow", call.Uses.Value, messages.SortedQuotes(events))}
}

func report(pos sourcetext.Position, kind, format string, args ...any) results.Diagnostic {
	return results.Diagnostic{Rule: kind, Message: fmt.Sprintf(format, args...), Start: pos}
}
