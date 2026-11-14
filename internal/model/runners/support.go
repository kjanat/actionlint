package runner

import (
	"slices"

	runnerapi "actionlint.kjanat.dev/v2/internal/model/runners/contract"
	"actionlint.kjanat.dev/v2/internal/model/services"
	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"
)

const (
	nodeRetirementURL = "https://github.blog/changelog/2026-09-23-node-20-is-no-longer-available-in-github-actions/"
	armRetirementURL  = "https://github.blog/changelog/2026-11-12-actions-for-arm32-linux-windows-retirement/"
)

func supportDate(s string) runnerapi.Date {
	d, err := runnerapi.ParseDate(s)
	if err != nil {
		panic(err)
	}
	return d
}

func builtinSupport(scope string) runnerapi.SupportCatalog {
	retired := []runnerapi.SupportChange{{On: supportDate("2026-09-23"), State: runnerapi.Unsupported}}
	deadline := []runnerapi.SupportChange{{On: supportDate("2026-11-12"), State: runnerapi.Deprecated}, {On: supportDate("2026-12-08"), State: runnerapi.Unsupported}}
	return runnerapi.SupportCatalog{DatasetStatus: runnerapi.DatasetStatus{DatasetID: "github-support:2026-11-12", Scope: scope, Freshness: runnerapi.Fresh, Complete: true}, Policies: []runnerapi.SupportPolicy{
		{
			ID: "github-self-hosted-arm32", Description: "self-hosted ARM32 runners on GitHub.com",
			URL: nodeRetirementURL, Capability: runnerapi.JobExecution,
			Service: runnerapi.ServiceScope{Kind: services.GitHubCom},
			Hosting: []runnerapi.Hosting{runnerapi.SelfHosted}, Architectures: []string{"arm32"}, Changes: retired,
		},
		{
			ID: "data-residency-self-hosted-arm32", Description: "self-hosted ARM32 runners on GitHub with Data Residency",
			URL: nodeRetirementURL, Capability: runnerapi.JobExecution,
			Service: runnerapi.ServiceScope{Kind: services.GitHubDataResidency},
			Hosting: []runnerapi.Hosting{runnerapi.SelfHosted}, Architectures: []string{"arm32"}, Changes: retired,
		},
		{
			ID: "github-hosted-arm32", Description: "ARM32 job execution on GitHub-hosted Linux and Windows runners",
			URL: armRetirementURL, Capability: runnerapi.JobExecution,
			Service: runnerapi.ServiceScope{Kind: services.GitHubCom},
			Hosting: []runnerapi.Hosting{runnerapi.Hosted}, OS: []runnerapi.OS{runnerapi.Linux, runnerapi.Windows},
			Architectures: []string{"arm32"}, Changes: deadline,
		},
		{
			ID: "github-hosted-aarch32-userspace", Description: "native AArch32 userspace on GitHub-hosted Linux and Windows runners",
			URL: armRetirementURL, Capability: runnerapi.NativeAArch32Userspace,
			Service: runnerapi.ServiceScope{Kind: services.GitHubCom},
			Hosting: []runnerapi.Hosting{runnerapi.Hosted}, OS: []runnerapi.OS{runnerapi.Linux, runnerapi.Windows}, Changes: deadline,
		},
	}}
}

type SupportProblem struct {
	Policy    runnerapi.SupportPolicy
	State     runnerapi.SupportState
	Effective runnerapi.Date
	Next      *runnerapi.SupportChange
	Selector  runnerapi.Selector
}

// Support reports policy effects established for every candidate of a selector.
func (k *Knowledge) Support(job workflowdecl.JobView, capability runnerapi.Capability) []SupportProblem {
	if k == nil || k.report.AsOf.IsZero() {
		return nil
	}
	catalog := k.report.Support
	if catalog.DatasetStatus.Freshness != runnerapi.Fresh || !catalog.DatasetStatus.Complete {
		return nil
	}
	if job.Strategy().Matrix().Present() {
		if rows, complete := job.Strategy().MatrixResolution().Combinations(256); complete && len(rows) == 0 {
			return nil
		}
	}
	record := k.jobs[job]
	var out []SupportProblem
	seen := make(map[int]bool)
	for _, alternative := range Alternatives(job) {
		i := alternative.AnswerIndex
		if i < 0 || i >= len(record.answers) || seen[i] || alternative.Selector.Incomplete {
			continue
		}
		seen[i] = true
		answer := record.answers[i]
		resolved, ok := answer.Resolution.(runnerapi.Resolved)
		if !ok || len(resolved.Candidates) == 0 || answer.DatasetStatus.Freshness != runnerapi.Fresh || !answer.DatasetStatus.Complete {
			continue
		}
		covered := make([]bool, len(resolved.Candidates))
		var matched []SupportProblem
		for _, p := range catalog.Policies {
			if p.Capability != capability || !serviceMatches(p.Service, k.report.Service) {
				continue
			}
			state, effective, next := policyState(p, k.report.AsOf)
			if state != runnerapi.Deprecated && state != runnerapi.Unsupported {
				continue
			}
			found := false
			for j, candidate := range resolved.Candidates {
				if candidateMatches(p, candidate) {
					covered[j] = true
					found = true
				}
			}
			if found {
				cloned := runnerapi.CloneSupportCatalog(runnerapi.SupportCatalog{Policies: []runnerapi.SupportPolicy{p}})
				matched = append(matched, SupportProblem{Policy: cloned.Policies[0], State: state, Effective: effective, Next: next, Selector: cloneSelector(alternative.Selector)})
			}
		}
		if !slices.Contains(covered, false) {
			out = append(out, matched...)
		}
	}
	return out
}

func candidateMatches(p runnerapi.SupportPolicy, c runnerapi.Candidate) bool {
	return (len(p.Hosting) == 0 || c.Hosting != runnerapi.UnknownHosting && slices.Contains(p.Hosting, c.Hosting)) &&
		(len(p.OS) == 0 || c.OS != runnerapi.UnknownOS && c.OS != "" && slices.Contains(p.OS, c.OS)) &&
		(len(p.Architectures) == 0 || c.Architecture != "" && slices.Contains(p.Architectures, runnerapi.NormalizeArchitecture(c.Architecture)))
}

func serviceMatches(scope runnerapi.ServiceScope, service services.Service) bool {
	if scope.Kind == services.Unknown {
		return true
	}
	if scope.Kind != service.Kind() {
		return false
	}
	if scope.MinVersion == "" && scope.BeforeVersion == "" {
		return true
	}
	version := service.Version()
	if version == "" {
		return false
	}
	return (scope.MinVersion == "" || services.CompareVersions(version, scope.MinVersion) >= 0) && (scope.BeforeVersion == "" || services.CompareVersions(version, scope.BeforeVersion) < 0)
}

func policyState(p runnerapi.SupportPolicy, date runnerapi.Date) (runnerapi.SupportState, runnerapi.Date, *runnerapi.SupportChange) {
	state := p.Initial
	var effective runnerapi.Date
	var next *runnerapi.SupportChange
	for _, c := range p.Changes {
		if c.On.Compare(date) > 0 {
			next = &c
			break
		}
		state, effective = c.State, c.On
	}
	return state, effective, next
}

func cloneSelector(s runnerapi.Selector) runnerapi.Selector {
	s.Labels = slices.Clone(s.Labels)
	if s.Group != nil {
		g := *s.Group
		s.Group = &g
	}
	return s
}
