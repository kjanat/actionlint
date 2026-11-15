package runners

import (
	"slices"

	"actionlint.kjanat.dev/v2/internal/model/lifecycle"
	runnerapi "actionlint.kjanat.dev/v2/internal/model/runners/runnerapi"
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
	Evidence  *lifecycle.Evidence
}

// Support reports policy effects established for every candidate of a selector.
func (k *Knowledge) Support(job workflowdecl.Job, capability runnerapi.Capability) []SupportProblem {
	return k.supportProblems(job, capability, false)
}

// UpcomingSupport reports future events that one policy establishes for every candidate.
// These observations do not establish a current support problem.
func (k *Knowledge) UpcomingSupport(job workflowdecl.Job, capability runnerapi.Capability) []SupportProblem {
	return k.supportProblems(job, capability, true)
}

func (k *Knowledge) supportProblems(job workflowdecl.Job, capability runnerapi.Capability, upcoming bool) []SupportProblem {
	if k == nil || k.report.AsOf.IsZero() {
		return nil
	}
	catalog := k.report.Support
	if catalog.DatasetStatus.Freshness != runnerapi.Fresh || !catalog.DatasetStatus.Complete {
		return nil
	}
	if job.Strategy != nil && job.Strategy.Matrix != nil {
		if rows, complete := workflowdecl.MatrixResolution(job.Strategy).Combinations(256); complete && len(rows) == 0 {
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
			current := state == runnerapi.Deprecated || state == runnerapi.Unsupported
			if current == upcoming {
				continue
			}
			if upcoming {
				next = upcomingChange(p, k.report.AsOf, state)
				if next == nil {
					continue
				}
			}
			found := false
			all := true
			for j, candidate := range resolved.Candidates {
				if candidateMatches(p, candidate) {
					if !upcoming {
						covered[j] = true
					}
					found = true
				} else {
					all = false
				}
			}
			if found && (!upcoming || all) {
				cloned := runnerapi.CloneSupportCatalog(runnerapi.SupportCatalog{Policies: []runnerapi.SupportPolicy{p}})
				problem := SupportProblem{Policy: cloned.Policies[0], State: state, Effective: effective, Next: next, Selector: cloneSelector(alternative.Selector)}
				if all {
					problem.Evidence = supportEvidence(problem, k.report.Service, k.report.AsOf, current)
				}
				matched = append(matched, problem)
			}
		}
		if upcoming || !slices.Contains(covered, false) {
			if !upcoming {
				proof := currentSupportProof(matched, resolved.Candidates, k.report.Service, k.report.AsOf)
				for j := range matched {
					if matched[j].Evidence == nil {
						matched[j].Evidence = supportEvidence(matched[j], k.report.Service, k.report.AsOf, true)
						matched[j].Evidence.AllOf = proof
					}
				}
			}
			out = append(out, matched...)
		}
	}
	return out
}

// Each candidate needs an established policy; overlapping policies are alternatives
// for that candidate. Keeping their dates separate lets notification policy require
// every candidate to reach a stage without inventing an aggregate deadline.
func currentSupportProof(problems []SupportProblem, candidates []runnerapi.Candidate, service services.Service, asOf runnerapi.Date) []lifecycle.Evidence {
	proof := make([]lifecycle.Evidence, 0, len(candidates))
	for _, candidate := range candidates {
		var alternatives []lifecycle.Evidence
		for _, problem := range problems {
			if candidateMatches(problem.Policy, candidate) {
				alternatives = append(alternatives, *supportEvidence(problem, service, asOf, true))
			}
		}
		evidence := alternatives[0]
		if len(alternatives) > 1 {
			evidence.AnyOf = alternatives
		}
		proof = append(proof, evidence)
	}
	return proof
}

func upcomingChange(policy runnerapi.SupportPolicy, asOf runnerapi.Date, state runnerapi.SupportState) *runnerapi.SupportChange {
	for _, change := range policy.Changes {
		if change.On.Compare(asOf) <= 0 {
			continue
		}
		switch change.State {
		case runnerapi.Unsupported:
			return &change
		case runnerapi.Supported:
			if state == runnerapi.SupportUnknown {
				return &change
			}
			return nil
		case runnerapi.SupportUnknown:
			return nil
		case runnerapi.Deprecated:
		}
	}
	return nil
}

func supportEvidence(problem SupportProblem, service services.Service, asOf runnerapi.Date, current bool) *lifecycle.Evidence {
	e := &lifecycle.Evidence{Subject: lifecycle.Runners, Name: problem.Policy.ID, Kind: lifecycle.SupportEnds, Service: service, AsOf: asOf, Source: problem.Policy.URL, Current: current}
	switch {
	case current && problem.State == runnerapi.Unsupported:
		e.Effective = problem.Effective
		if e.Effective.IsZero() && problem.Next != nil && problem.Next.State == runnerapi.Supported {
			e.Kind, e.Effective = lifecycle.SupportStarts, problem.Next.On
		}
	case current && problem.State == runnerapi.Deprecated:
		if next := upcomingChange(problem.Policy, asOf, problem.State); next != nil && next.State == runnerapi.Unsupported {
			e.Effective = next.On
		}
	case problem.Next != nil:
		switch problem.Next.State {
		case runnerapi.Unsupported:
			e.Effective = problem.Next.On
		case runnerapi.Supported:
			if !current {
				e.Kind, e.Effective = lifecycle.SupportStarts, problem.Next.On
			}
		case runnerapi.SupportUnknown, runnerapi.Deprecated:
		}
	}
	switch {
	case problem.Policy.URL == nodeRetirementURL && (problem.Policy.ID == "github-self-hosted-arm32" || problem.Policy.ID == "data-residency-self-hosted-arm32"):
		e.Announced = supportDate("2026-08-25")
	case problem.Policy.URL == armRetirementURL && (problem.Policy.ID == "github-hosted-arm32" || problem.Policy.ID == "github-hosted-aarch32-userspace"):
		e.Announced = supportDate("2026-11-12")
	}
	return e
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
