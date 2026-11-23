package runners

import (
	"errors"
	"slices"
	"strings"

	"actionlint.kjanat.dev/v2/internal/model/contexts/value"
	"actionlint.kjanat.dev/v2/internal/model/runners/runnerapi"
	"actionlint.kjanat.dev/v2/internal/model/workflows"
)

type Traits struct {
	PathsKnown                           bool
	Platform                             Platform
	Shell                                string
	CaseInsensitive, ShIsDash, Ephemeral bool
}

// JobKnowledge is computed before visitors run and never invokes a provider during traversal.
type JobKnowledge struct {
	Traits
	Rejections []runnerapi.Rejection
	Answers    []runnerapi.Answer
}

// OperatingSystems adds UnknownOS for unknown or incomplete provider answers and never guesses from labels.
func (k JobKnowledge) OperatingSystems() []runnerapi.OS {
	var systems []runnerapi.OS
	add := func(system runnerapi.OS) {
		if !slices.Contains(systems, system) {
			systems = append(systems, system)
		}
	}
	if len(k.Answers) == 0 {
		switch k.Platform {
		case Windows:
			return []runnerapi.OS{runnerapi.Windows}
		case MacOrLinux:
			return []runnerapi.OS{runnerapi.Linux, runnerapi.MacOS}
		case Any:
			return []runnerapi.OS{runnerapi.UnknownOS}
		default:
			return []runnerapi.OS{runnerapi.UnknownOS}
		}
	}
	for _, answer := range k.Answers {
		resolved, ok := answer.Resolution.(runnerapi.Resolved)
		if !ok || len(resolved.Candidates) == 0 || !answer.DatasetStatus.Complete || answer.DatasetStatus.Freshness != runnerapi.Fresh {
			add(runnerapi.UnknownOS)
			continue
		}
		for _, candidate := range resolved.Candidates {
			add(candidate.OS)
		}
	}
	return systems
}

// Each jobRecord query returns detached slices.
type jobRecord struct {
	traits     Traits
	rejections []runnerapi.Rejection
	answers    []runnerapi.Answer
}

func (r jobRecord) knowledge() JobKnowledge {
	return JobKnowledge{Traits: r.traits, Rejections: slices.Clone(r.rejections), Answers: detachAnswers(r.answers)}
}

type Knowledge struct {
	jobs   map[workflows.BoundJob]jobRecord
	report runnerapi.Report
	target string
}

func (k *Knowledge) Target() string {
	if k == nil {
		return ""
	}
	return k.target
}

func (k *Knowledge) Job(job workflows.BoundJob) JobKnowledge {
	if k == nil {
		return builtinJob(job)
	}
	return k.jobs[job].knowledge()
}

// Report returns a zero Identity when no provider was consulted.
func (k *Knowledge) Report() runnerapi.Report {
	if k == nil {
		return runnerapi.Report{}
	}
	report := k.report
	report.Provider = detachDescriptor(report.Provider)
	report.Answers = detachAnswers(report.Answers)
	report.Support = runnerapi.CloneSupportCatalog(report.Support)
	return report
}

func detachDescriptor(descriptor runnerapi.Descriptor) runnerapi.Descriptor {
	descriptor.Targets = slices.Clone(descriptor.Targets)
	return descriptor
}

func detachAnswers(answers []runnerapi.Answer) []runnerapi.Answer {
	if answers == nil {
		return nil
	}
	detached := make([]runnerapi.Answer, len(answers))
	for i, answer := range answers {
		detached[i] = runnerapi.Answer{Resolution: detachResolution(answer.Resolution), DatasetStatus: answer.DatasetStatus}
	}
	return detached
}

func detachResolution(resolution runnerapi.Resolution) runnerapi.Resolution {
	switch value := resolution.(type) {
	case runnerapi.Resolved:
		return runnerapi.Resolved{Candidates: slices.Clone(value.Candidates)}
	case runnerapi.NoMatch:
		return runnerapi.NoMatch{Rejections: slices.Clone(value.Rejections)}
	case runnerapi.Unknown:
		return value
	case runnerapi.NotApplicable:
		return value
	}
	return nil
}

// Value-receiver markers permit native providers to return pointers too.
func resolutionValue(resolution runnerapi.Resolution) (runnerapi.Resolution, error) {
	switch value := resolution.(type) {
	case runnerapi.Resolved, runnerapi.NoMatch, runnerapi.Unknown, runnerapi.NotApplicable:
		return value, nil
	case *runnerapi.Resolved:
		if value != nil {
			return *value, nil
		}
	case *runnerapi.NoMatch:
		if value != nil {
			return *value, nil
		}
	case *runnerapi.Unknown:
		if value != nil {
			return *value, nil
		}
	case *runnerapi.NotApplicable:
		if value != nil {
			return *value, nil
		}
	}
	return nil, errors.New("invalid runner resolution")
}

func joinAnswers(job workflows.BoundJob, answers []runnerapi.Answer) (jobRecord, error) {
	record := jobRecord{answers: slices.Clone(answers)}
	var candidates []runnerapi.Candidate
	uncertain := false
	for _, answer := range answers {
		if answer.DatasetStatus.Freshness != runnerapi.Fresh {
			uncertain = true
			continue
		}
		switch result := answer.Resolution.(type) {
		case runnerapi.Resolved:
			if len(result.Candidates) == 0 {
				return jobRecord{}, errors.New("resolved runner answer has no candidates")
			}
			if !answer.DatasetStatus.Complete {
				uncertain = true
				continue
			}
			candidates = append(candidates, result.Candidates...)
		case runnerapi.NoMatch:
			if len(result.Rejections) == 0 {
				return jobRecord{}, errors.New("no-match runner answer has no rejections")
			}
			if !answer.DatasetStatus.Complete {
				uncertain = true
				continue
			}
			for _, rejection := range result.Rejections {
				if rejection.Position.Line == 0 {
					if declaration, present := job.Position(); present {
						rejection.Position = declaration
					}
				}
				if !slices.Contains(record.rejections, rejection) {
					record.rejections = append(record.rejections, rejection)
				}
			}
			uncertain = true
		case runnerapi.Unknown, runnerapi.NotApplicable:
			uncertain = true
		default:
			return jobRecord{}, errors.New("invalid runner resolution")
		}
	}
	if uncertain || len(candidates) == 0 {
		return record, nil
	}
	record.traits.Platform, record.traits.Shell = candidatePlatform(candidates[0]), candidates[0].DefaultShell
	caseSensitivity := candidates[0].CaseSensitivity
	record.traits.ShIsDash, record.traits.Ephemeral = candidates[0].ShIsDash, candidates[0].Ephemeral
	for _, candidate := range candidates[1:] {
		if candidatePlatform(candidate) != record.traits.Platform {
			record.traits.Platform = Any
		}
		if candidate.DefaultShell != record.traits.Shell {
			record.traits.Shell = ""
		}
		if caseSensitivity != candidate.CaseSensitivity {
			caseSensitivity = runnerapi.UnknownCase
		}
		record.traits.ShIsDash = record.traits.ShIsDash && candidate.ShIsDash
		record.traits.Ephemeral = record.traits.Ephemeral && candidate.Ephemeral
	}
	record.traits.PathsKnown = record.traits.Platform != Any && (caseSensitivity == runnerapi.CaseSensitive || caseSensitivity == runnerapi.CaseInsensitive)
	record.traits.CaseInsensitive = caseSensitivity == runnerapi.CaseInsensitive
	return record, nil
}

func candidatePlatform(c runnerapi.Candidate) Platform {
	switch c.OS {
	case runnerapi.Windows:
		return Windows
	case runnerapi.Linux, runnerapi.MacOS:
		return MacOrLinux
	case runnerapi.UnknownOS:
	}
	return Any
}

func builtinJob(job workflows.BoundJob) JobKnowledge {
	if !job.Present() {
		return JobKnowledge{}
	}
	selection := workflows.RunnerSelection(job.RunsOn)
	return JobKnowledge{Traits: builtinTraits(selection, Labels(selection))}
}

func builtinTraits(selection value.Value, labels []value.Value) Traits {
	k := Traits{Platform: PlatformOf(selection), Ephemeral: Hosted(selection)}
	// Preserve GitHub's established default-shell behavior.
	if k.Platform == MacOrLinux {
		k.Shell = "bash"
	}
	if k.Platform == Windows {
		k.Shell = "pwsh"
	}
	k.CaseInsensitive = k.Platform == Windows
	mac, ubuntu, macOS := k.Ephemeral, k.Ephemeral, false
	for _, label := range labels {
		name, known := label.ScalarString()
		if !known {
			continue
		}
		name = strings.ToLower(name)
		mac = mac && (strings.HasPrefix(name, "macos-") || strings.HasPrefix(name, "xcode-"))
		ubuntu = ubuntu && strings.HasPrefix(name, "ubuntu-")
		macOS = macOS || defaultRunnerOSCompats[name]&defaultRunnerOSCompats["macos"] != 0
	}
	k.CaseInsensitive = k.CaseInsensitive || mac
	k.ShIsDash = ubuntu
	k.PathsKnown = k.Platform != Any && (!macOS || k.Ephemeral)
	return k
}
