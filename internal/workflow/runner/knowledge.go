package runner

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/documentparse"
	workflowdecl "actionlint.kjanat.dev/v2/internal/workflow/declaration"
	"actionlint.kjanat.dev/v2/internal/workflow/value"
	runnerapi "actionlint.kjanat.dev/v2/runner"
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
	Findings []diagnostic.Diagnostic
	Answers  []runnerapi.Answer
}

// jobRecord is the stored form of JobKnowledge. Runner findings are always
// runner-label rejections, so they are held as plain values; every reader
// receives fresh diagnostics and answers and cannot alter another's view.
type jobRecord struct {
	traits     Traits
	rejections []runnerapi.Rejection
	answers    []runnerapi.Answer
}

func (r jobRecord) knowledge() JobKnowledge {
	return JobKnowledge{Traits: r.traits, Findings: labelFindings(r.rejections), Answers: detachAnswers(r.answers)}
}

// Knowledge contains prepared runner information for each job in a document.
type Knowledge struct {
	jobs   map[workflowdecl.JobView]jobRecord
	report runnerapi.Report
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
		detached[i] = runnerapi.Answer{Resolution: detachResolution(answer.Resolution), Evidence: answer.Evidence}
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

// Prepare resolves a document against the session's selected, immutable evidence.
// Failed resolution leaves unknown facts while independent checks can continue.
func Prepare(ctx context.Context, snapshot *documentparse.SourceSnapshot, custom []string, options runnerapi.Options, scope string) (*Knowledge, error) {
	k := &Knowledge{jobs: make(map[workflowdecl.JobView]jobRecord)}
	if snapshot == nil {
		return k, nil
	}
	workflow, available := snapshot.WorkflowView()
	if !available {
		return k, nil
	}
	if options.Target == "" {
		options.Target = "github"
	}
	hasSnapshot := options.Snapshot != (runnerapi.Snapshot{})
	switch options.Snapshot.Freshness {
	case "":
		options.Snapshot.Freshness = runnerapi.Missing
	case runnerapi.Fresh, runnerapi.Stale, runnerapi.Missing:
	default:
		return k, errors.New("invalid runner snapshot freshness")
	}
	var selected runnerapi.Provider
	builtinLabels := false
	identity := ""
	if options.Scope != "" {
		scope = options.Scope
	}
	if hasSnapshot && options.Snapshot.Scope != scope {
		return k, errors.New("runner snapshot scope does not match requested scope")
	}
	if options.Target == "github" && (options.Provider == "" || options.Provider == "github") {
		selected = builtinProvider{custom: slices.Clone(custom)}
		builtinLabels = true
		identity = "builtin-github"
	}
	if options.Target == "github" && options.Provider == "snapshot" {
		selected = snapshotProvider{}
		identity = "builtin-github-snapshot-v1"
	}
	for _, registration := range options.Providers {
		if registration.Provider == nil {
			return k, errors.New("nil runner provider")
		}
		d := registration.Provider.RunnerDescriptor()
		if options.Target == "github" && options.Provider == "" {
			continue
		}
		if !slices.Contains(d.Targets, options.Target) || options.Provider != "" && d.Name != options.Provider {
			continue
		}
		if selected != nil {
			return k, fmt.Errorf("conflicting runner providers for target %q; select one explicitly", options.Target)
		}
		selected = registration.Provider
		identity = registration.Identity
	}
	if selected == nil {
		return k, fmt.Errorf("no runner provider %q for target %q", options.Provider, options.Target)
	}
	var selectors []runnerapi.Selector
	type jobRange struct {
		job        workflowdecl.JobView
		start, end int
	}
	var ranges []jobRange
	workflowJobs := maps.Collect(workflow.Jobs())
	for _, id := range slices.Sorted(maps.Keys(workflowJobs)) {
		job := workflowJobs[id]
		if !job.RunsOn().Present() {
			continue
		}
		start := len(selectors)
		selectors = append(selectors, Selectors(job)...)
		ranges = append(ranges, jobRange{job, start, len(selectors)})
	}
	if len(selectors) == 0 {
		return k, nil
	}
	request := runnerapi.Request{Target: options.Target, Scope: scope, Snapshot: options.Snapshot, Selectors: selectors}
	descriptor := selected.RunnerDescriptor()
	dependency, err := json.Marshal(struct {
		Request    runnerapi.Request
		Descriptor runnerapi.Descriptor
		Artifact   string
		Custom     []string
	}{request, descriptor, identity, custom})
	if err != nil {
		return k, err
	}
	k.report = runnerapi.Report{Identity: fmt.Sprintf("%x", sha256.Sum256(dependency)), Provider: descriptor, Artifact: identity, Target: options.Target, Scope: scope, Snapshot: options.Snapshot.Identity()}
	answers, err := selected.ResolveRunners(ctx, request)
	if err != nil {
		return k, fmt.Errorf("runner provider %s: %w", selected.RunnerDescriptor().Name, err)
	}
	if len(answers) != len(selectors) {
		return k, fmt.Errorf("runner provider returned %d answers for %d selectors", len(answers), len(selectors))
	}
	for i := range answers {
		a := &answers[i]
		resolution, err := resolutionValue(a.Resolution)
		if err != nil {
			return k, err
		}
		a.Resolution = resolution
		if a.Evidence.Scope != scope {
			return k, errors.New("runner answer scope does not match requested scope")
		}
		if a.Evidence.Dataset == "" {
			return k, errors.New("runner answer has no dataset identity")
		}
		if hasSnapshot {
			freshness := options.Snapshot.Freshness
			if freshness != runnerapi.Fresh {
				a.Evidence.Freshness = freshness
			}
			a.Evidence.Complete = a.Evidence.Complete && options.Snapshot.Complete
		}
		if selectors[i].Incomplete {
			if _, resolved := a.Resolution.(runnerapi.Resolved); resolved {
				a.Resolution = runnerapi.Unknown{Reason: "selector alternatives are incomplete"}
			}
		}
		if noMatch, ok := a.Resolution.(runnerapi.NoMatch); ok {
			for _, rejection := range noMatch.Rejections {
				valid := rejection.Position.Line == 0 && rejection.Position.Column == 0
				for _, label := range selectors[i].Labels {
					valid = valid || label.Position == rejection.Position
				}
				if selectors[i].Group != nil {
					valid = valid || selectors[i].Group.Position == rejection.Position
				}
				if !valid {
					return k, errors.New("runner rejection refers to an unrelated source position")
				}
			}
		}
	}
	k.report.Answers = answers
	evidence, err := json.Marshal(answers)
	if err != nil {
		return k, err
	}
	k.report.Identity = fmt.Sprintf("%x", sha256.Sum256(append(dependency, evidence...)))
	jobs := make(map[workflowdecl.JobView]jobRecord, len(ranges))
	for _, span := range ranges {
		record, err := joinAnswers(span.job, answers[span.start:span.end])
		if err != nil {
			return k, fmt.Errorf("runner provider %s: %w", selected.RunnerDescriptor().Name, err)
		}
		// Supplied snapshots gate diagnostics with the same freshness and
		// completeness policy as their answers. No snapshot uses the fresh catalog.
		if builtinLabels && (!hasSnapshot || options.Snapshot.Freshness == runnerapi.Fresh && options.Snapshot.Complete) {
			// Provider answers describe execution alternatives. Built-in declaration
			// spelling and execution compatibility share Check's diagnostic owner.
			record.rejections = labelRejections(span.job, custom)
		}
		jobs[span.job] = record
	}
	k.jobs = jobs
	return k, nil
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
		if answer.Evidence.Freshness != runnerapi.Fresh {
			uncertain = true
			continue
		}
		switch result := answer.Resolution.(type) {
		case runnerapi.Resolved:
			if len(result.Candidates) == 0 {
				return jobRecord{}, errors.New("resolved runner answer has no candidates")
			}
			if !answer.Evidence.Complete {
				uncertain = true
				continue
			}
			candidates = append(candidates, result.Candidates...)
		case runnerapi.NoMatch:
			if len(result.Rejections) == 0 {
				return jobRecord{}, errors.New("no-match runner answer has no rejections")
			}
			if !answer.Evidence.Complete {
				uncertain = true
				continue
			}
			for _, rejection := range result.Rejections {
				if rejection.Position.Line == 0 {
					if declaration, present := job.Pos(); present {
						rejection.Position = diagnostic.Position{Line: declaration.Line, Column: declaration.Col}
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
