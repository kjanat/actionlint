package runner

import (
	"errors"
	"slices"
	"strings"

	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	value "actionlint.kjanat.dev/v2/internal/model/contexts/values"
	runnerapi "actionlint.kjanat.dev/v2/internal/model/runners/contract"
	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"
)

// Traits are the execution guarantees shared by every alternative of a job.
type Traits struct {
	PathsKnown                           bool
	Platform                             Platform
	Shell                                string
	CaseInsensitive, ShIsDash, Ephemeral bool
}

// JobKnowledge is a conservative join of the alternatives for one job. It is
// computed before visitors run and never invokes a provider during traversal.
// Each value is owned by its reader.
type JobKnowledge struct {
	Traits
	Rejections []runnerapi.Rejection
	Answers    []runnerapi.Answer
}

// jobRecord stores runner traits, rejections and answers. Each query returns
// detached slices so readers cannot change the stored knowledge.
type jobRecord struct {
	traits     Traits
	rejections []runnerapi.Rejection
	answers    []runnerapi.Answer
}

func (r jobRecord) knowledge() JobKnowledge {
	return JobKnowledge{Traits: r.traits, Rejections: slices.Clone(r.rejections), Answers: detachAnswers(r.answers)}
}

// Knowledge contains prepared runner information for each job in a document.
type Knowledge struct {
	jobs   map[workflowdecl.JobView]jobRecord
	report runnerapi.Report
	target string
}

// Target identifies the selected execution platform, including documents without
// runner requests, such as standalone actions and reusable-workflow callers.
func (k *Knowledge) Target() string {
	if k == nil {
		return ""
	}
	return k.target
}

func (k *Knowledge) Job(job workflowdecl.JobView) JobKnowledge {
	if k == nil {
		return builtinJob(job)
	}
	return k.jobs[job].knowledge()
}

// Report binds the prepared answers to the selected provider. A zero Identity
// means no provider was consulted. The result is detached from k.
func (k *Knowledge) Report() runnerapi.Report {
	if k == nil {
		return runnerapi.Report{}
	}
	report := k.report
	report.Provider = detachDescriptor(report.Provider)
	report.Answers = detachAnswers(report.Answers)
	return report
}

func detachDescriptor(descriptor runnerapi.Descriptor) runnerapi.Descriptor {
	descriptor.Targets = slices.Clone(descriptor.Targets)
	return descriptor
}

// detachAnswers deep-copies answers. Stored answers hold only the value
// resolutions normalized by resolutionValue.
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

// Value-receiver markers permit native providers to return pointers too. Keep
// downstream validation and report identity independent of that Go spelling.
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

func joinAnswers(job workflowdecl.JobView, answers []runnerapi.Answer) (jobRecord, error) {
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
					if declaration, present := job.Pos(); present {
						rejection.Position = sourcepos.Position{Line: declaration.Line, Column: declaration.Col}
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

func builtinJob(job workflowdecl.JobView) JobKnowledge {
	if !job.Present() {
		return JobKnowledge{}
	}
	selection := job.RunsOn().Selection()
	return JobKnowledge{Traits: builtinTraits(selection, Labels(selection))}
}

func builtinTraits(selection value.Value, labels []value.Value) Traits {
	k := Traits{Platform: PlatformOf(selection), Ephemeral: Hosted(selection)}
	// Preserve GitHub's established default-shell behavior. External unknown
	// answers never reach these label-based assumptions.
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
