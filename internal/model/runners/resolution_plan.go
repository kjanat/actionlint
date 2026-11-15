package runner

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"

	"actionlint.kjanat.dev/v2/internal/model/parsing"
	runnerapi "actionlint.kjanat.dev/v2/internal/model/runners/contract"
	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"
)

// ProviderSelection identifies the selected implementation without holding it.
type ProviderSelection struct {
	Descriptor       runnerapi.Descriptor
	ImplementationID string
	BuiltinLabels    bool
}

type jobRange struct {
	job        workflowdecl.Job
	start, end int
}

// ResolutionPlan maps provider answers to the captured jobs that requested them.
// It contains values only and cannot invoke a provider.
type ResolutionPlan struct {
	request     runnerapi.Request
	selection   ProviderSelection
	custom      LabelSet
	ranges      []jobRange
	dependency  []byte
	hasSnapshot bool
}

// UnknownKnowledge supplies the target without inventing job capabilities.
func UnknownKnowledge(target string) *Knowledge { return &Knowledge{target: target} }

// NewResolutionPlan captures selectors and provider identity. hasSnapshot records
// whether an inventory was explicitly supplied before freshness normalization.
func NewResolutionPlan(snapshot parsing.SourceSnapshot, custom LabelSet, request runnerapi.Request, selection ProviderSelection, hasSnapshot bool) (*ResolutionPlan, error) {
	workflow, _ := snapshot.WorkflowView()
	var selectors []runnerapi.Selector
	var ranges []jobRange
	workflowJobs := maps.Collect(workflow.Jobs())
	for _, id := range slices.Sorted(maps.Keys(workflowJobs)) {
		job := workflowJobs[id]
		if job.RunsOn == nil {
			continue
		}
		start := len(selectors)
		selectors = append(selectors, Selectors(job)...)
		ranges = append(ranges, jobRange{job, start, len(selectors)})
	}
	request.Selectors = selectors
	selection.Descriptor = detachDescriptor(selection.Descriptor)
	custom.patterns = slices.Clone(custom.patterns)
	dependency, err := json.Marshal(struct {
		Request     runnerapi.Request
		Descriptor  runnerapi.Descriptor
		Artifact    string
		Custom      []string
		CustomKnown bool
	}{request, selection.Descriptor, selection.ImplementationID, custom.patterns, custom.known})
	if err != nil {
		return nil, err
	}
	return &ResolutionPlan{request: request, selection: selection, custom: custom, ranges: ranges, dependency: dependency, hasSnapshot: hasSnapshot}, nil
}

func (plan *ResolutionPlan) Empty() bool { return len(plan.request.Selectors) == 0 }

// Request returns a detached request so a provider cannot change the plan.
func (plan *ResolutionPlan) Request() runnerapi.Request {
	request := plan.request
	request.Selectors = slices.Clone(request.Selectors)
	for i := range request.Selectors {
		request.Selectors[i].Labels = slices.Clone(request.Selectors[i].Labels)
		if request.Selectors[i].Group != nil {
			group := *request.Selectors[i].Group
			request.Selectors[i].Group = &group
		}
	}
	return request
}

// Unknown returns the provider identity with no established job capabilities.
func (plan *ResolutionPlan) Unknown() *Knowledge {
	return &Knowledge{target: plan.request.Target, report: runnerapi.Report{
		CacheKey: fmt.Sprintf("%x", sha256.Sum256(plan.dependency)), Provider: plan.selection.Descriptor,
		ImplementationID: plan.selection.ImplementationID, Target: plan.request.Target, Scope: plan.request.Scope, SnapshotDigest: plan.request.Snapshot.Digest(), Service: plan.request.Service, AsOf: plan.request.AsOf,
	}}
}

// Complete validates and copies provider-owned answers before building knowledge.
func (plan *ResolutionPlan) Complete(response runnerapi.Response) (*Knowledge, error) {
	k := plan.Unknown()
	catalog := runnerapi.CloneSupportCatalog(response.Support)
	for i := range catalog.Policies {
		for j, architecture := range catalog.Policies[i].Architectures {
			catalog.Policies[i].Architectures[j] = runnerapi.NormalizeArchitecture(architecture)
		}
	}
	if err := runnerapi.ValidateSupportCatalog(catalog, plan.request.Scope); err != nil {
		return k, err
	}
	if plan.hasSnapshot && catalog.DatasetStatus.DatasetID != "" {
		if plan.request.Snapshot.Freshness != runnerapi.Fresh {
			catalog.DatasetStatus.Freshness = plan.request.Snapshot.Freshness
		}
		catalog.DatasetStatus.Complete = catalog.DatasetStatus.Complete && plan.request.Snapshot.Complete
	}
	k.report.Support = catalog
	supplied := response.Answers
	if len(supplied) != len(plan.request.Selectors) {
		return k, fmt.Errorf("runner provider returned %d answers for %d selectors", len(supplied), len(plan.request.Selectors))
	}
	answers := make([]runnerapi.Answer, len(supplied))
	for i, answer := range supplied {
		resolution, err := resolutionValue(answer.Resolution)
		if err != nil {
			return k, err
		}
		if resolved, ok := resolution.(runnerapi.Resolved); ok {
			resolved.Candidates = slices.Clone(resolved.Candidates)
			for j := range resolved.Candidates {
				c := &resolved.Candidates[j]
				if c.Hosting != runnerapi.UnknownHosting && c.Hosting != runnerapi.Hosted && c.Hosting != runnerapi.SelfHosted {
					return k, errors.New("invalid runner hosting")
				}
				switch c.OS {
				case "", runnerapi.UnknownOS, runnerapi.Linux, runnerapi.MacOS, runnerapi.Windows:
				default:
					return k, errors.New("invalid runner OS")
				}
				switch c.CaseSensitivity {
				case "", runnerapi.UnknownCase, runnerapi.CaseSensitive, runnerapi.CaseInsensitive:
				default:
					return k, errors.New("invalid runner case sensitivity")
				}
				c.Architecture = runnerapi.NormalizeArchitecture(c.Architecture)
				if len(c.Architecture) > 128 {
					return k, errors.New("runner architecture exceeds limit")
				}
			}
			resolution = resolved
		}
		answers[i] = runnerapi.Answer{Resolution: detachResolution(resolution), DatasetStatus: answer.DatasetStatus}
	}
	for i := range answers {
		a := &answers[i]
		if a.DatasetStatus.Scope != plan.request.Scope {
			return k, errors.New("runner answer scope does not match requested scope")
		}
		if a.DatasetStatus.DatasetID == "" {
			return k, errors.New("runner answer has no dataset identity")
		}
		switch a.DatasetStatus.Freshness {
		case runnerapi.Fresh, runnerapi.Stale, runnerapi.Missing:
		default:
			return k, errors.New("invalid runner answer freshness")
		}
		if plan.hasSnapshot {
			freshness := plan.request.Snapshot.Freshness
			if freshness != runnerapi.Fresh {
				a.DatasetStatus.Freshness = freshness
			}
			a.DatasetStatus.Complete = a.DatasetStatus.Complete && plan.request.Snapshot.Complete
		}
		if plan.request.Selectors[i].Incomplete {
			if _, resolved := a.Resolution.(runnerapi.Resolved); resolved {
				a.Resolution = runnerapi.Unknown{Reason: "selector alternatives are incomplete"}
			}
		}
		if noMatch, ok := a.Resolution.(runnerapi.NoMatch); ok {
			for _, rejection := range noMatch.Rejections {
				valid := rejection.Position.Line == 0 && rejection.Position.Column == 0
				for _, label := range plan.request.Selectors[i].Labels {
					valid = valid || label.Position == rejection.Position
				}
				if plan.request.Selectors[i].Group != nil {
					valid = valid || plan.request.Selectors[i].Group.Position == rejection.Position
				}
				if !valid {
					return k, errors.New("runner rejection refers to an unrelated source position")
				}
			}
		}
	}
	k.report.Answers = answers
	encodedAnswers, err := json.Marshal(runnerapi.Response{Answers: answers, Support: catalog})
	if err != nil {
		return k, err
	}
	cacheKeyHash := sha256.New()
	cacheKeyHash.Write(plan.dependency)
	cacheKeyHash.Write(encodedAnswers)
	k.report.CacheKey = hex.EncodeToString(cacheKeyHash.Sum(nil))
	jobs := make(map[workflowdecl.Job]jobRecord, len(plan.ranges))
	for _, span := range plan.ranges {
		record, err := joinAnswers(span.job, answers[span.start:span.end])
		if err != nil {
			return k, fmt.Errorf("runner provider %s: %w", plan.selection.Descriptor.Name, err)
		}
		// Supplied snapshots gate label rejections with the same freshness and
		// completeness policy as their answers. No snapshot uses the fresh catalog.
		if plan.selection.BuiltinLabels && (!plan.hasSnapshot || plan.request.Snapshot.Freshness == runnerapi.Fresh && plan.request.Snapshot.Complete) {
			// Provider answers describe execution alternatives. Built-in rejections
			// also describe spelling errors in declared but excluded alternatives.
			record.rejections = LabelRejections(span.job, plan.custom)
		}
		jobs[span.job] = record
	}
	k.jobs = jobs
	return k, nil
}
