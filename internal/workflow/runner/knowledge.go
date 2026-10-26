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
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/internal/workflow/value"
	runnerapi "actionlint.kjanat.dev/v2/runner"
)

// JobKnowledge is a conservative join of the alternatives for one job. It is
// computed before visitors run and never invokes a provider during traversal.
type JobKnowledge struct {
	PathsKnown                           bool
	Platform                             Platform
	Shell                                string
	CaseInsensitive, ShIsDash, Ephemeral bool
	Findings                             []diagnostic.Diagnostic
	Answers                              []runnerapi.Answer
}

// Knowledge contains prepared runner information for each job in a document.
type Knowledge struct {
	jobs     map[documentparse.JobView]JobKnowledge
	snapshot *documentparse.SourceSnapshot
	Report   runnerapi.Report
}

func (k *Knowledge) Job(job documentparse.JobView) JobKnowledge {
	if k == nil {
		return builtinJob(job)
	}
	return k.jobs[job]
}

// JobCursor resolves an engine hook cursor through the snapshot used for preparation.
func (k *Knowledge) JobCursor(cursor *workflowlang.Job) JobKnowledge {
	if k == nil || k.snapshot == nil {
		return JobKnowledge{}
	}
	job, err := k.snapshot.JobView(cursor)
	if err != nil {
		return JobKnowledge{}
	}
	return k.Job(job)
}

// Prepare resolves a document against the session's selected, immutable evidence.
// Failed resolution leaves unknown facts while independent checks can continue.
func Prepare(ctx context.Context, snapshot *documentparse.SourceSnapshot, custom []string, options runnerapi.Options, scope string) (*Knowledge, error) {
	k := &Knowledge{jobs: make(map[documentparse.JobView]JobKnowledge), snapshot: snapshot}
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
	identity := ""
	if options.Scope != "" {
		scope = options.Scope
	}
	if hasSnapshot && options.Snapshot.Scope != scope {
		return k, errors.New("runner snapshot scope does not match requested scope")
	}
	if options.Target == "github" && (options.Provider == "" || options.Provider == "github") {
		selected = builtinProvider{custom: slices.Clone(custom)}
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
		job        documentparse.JobView
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
	k.Report = runnerapi.Report{Identity: fmt.Sprintf("%x", sha256.Sum256(dependency)), Provider: descriptor, Artifact: identity, Target: options.Target, Scope: scope, Snapshot: options.Snapshot.Identity()}
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
	k.Report.Answers = answers
	evidence, err := json.Marshal(answers)
	if err != nil {
		return k, err
	}
	k.Report.Identity = fmt.Sprintf("%x", sha256.Sum256(append(dependency, evidence...)))
	jobs := make(map[documentparse.JobView]JobKnowledge, len(ranges))
	for _, span := range ranges {
		knowledge, err := joinAnswers(span.job, answers[span.start:span.end])
		if err != nil {
			return k, fmt.Errorf("runner provider %s: %w", selected.RunnerDescriptor().Name, err)
		}
		jobs[span.job] = knowledge
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

func joinAnswers(job documentparse.JobView, answers []runnerapi.Answer) (JobKnowledge, error) {
	k := JobKnowledge{Answers: slices.Clone(answers)}
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
				return k, errors.New("resolved runner answer has no candidates")
			}
			if !answer.Evidence.Complete {
				uncertain = true
				continue
			}
			candidates = append(candidates, result.Candidates...)
		case runnerapi.NoMatch:
			if len(result.Rejections) == 0 {
				return k, errors.New("no-match runner answer has no rejections")
			}
			if !answer.Evidence.Complete {
				uncertain = true
				continue
			}
			for _, rejection := range result.Rejections {
				pos := rejection.Position
				if pos.Line == 0 {
					if declaration, present := job.Pos(); present {
						pos = diagnostic.Position{Line: declaration.Line, Column: declaration.Col}
					}
				}
				finding := diagnostic.Diagnostic{Rule: "runner-label", Message: rejection.Reason, Start: pos}
				if !slices.ContainsFunc(k.Findings, func(d diagnostic.Diagnostic) bool { return d.Start == finding.Start && d.Message == finding.Message }) {
					k.Findings = append(k.Findings, finding)
				}
			}
			uncertain = true
		case runnerapi.Unknown, runnerapi.NotApplicable:
			uncertain = true
		default:
			return k, errors.New("invalid runner resolution")
		}
	}
	if uncertain || len(candidates) == 0 {
		return k, nil
	}
	k.Platform, k.Shell = candidatePlatform(candidates[0]), candidates[0].DefaultShell
	caseSensitivity := candidates[0].CaseSensitivity
	k.ShIsDash, k.Ephemeral = candidates[0].ShIsDash, candidates[0].Ephemeral
	for _, candidate := range candidates[1:] {
		if candidatePlatform(candidate) != k.Platform {
			k.Platform = Any
		}
		if candidate.DefaultShell != k.Shell {
			k.Shell = ""
		}
		if caseSensitivity != candidate.CaseSensitivity {
			caseSensitivity = runnerapi.UnknownCase
		}
		k.ShIsDash = k.ShIsDash && candidate.ShIsDash
		k.Ephemeral = k.Ephemeral && candidate.Ephemeral
	}
	k.PathsKnown = k.Platform != Any && (caseSensitivity == runnerapi.CaseSensitive || caseSensitivity == runnerapi.CaseInsensitive)
	k.CaseInsensitive = caseSensitivity == runnerapi.CaseInsensitive
	return k, nil
}

func candidatePlatform(c runnerapi.Candidate) Platform {
	switch c.OS {
	case runnerapi.Windows:
		return Windows
	case runnerapi.Linux, runnerapi.MacOS:
		return MacOrLinux
	default:
		return Any
	}
}

func builtinJob(job documentparse.JobView) JobKnowledge {
	if !job.Present() {
		return JobKnowledge{}
	}
	selection := job.RunsOn().Selection()
	return builtinKnowledge(selection, Labels(selection))
}

func builtinKnowledge(selection value.Value, labels []value.Value) JobKnowledge {
	k := JobKnowledge{Platform: PlatformOf(selection), Ephemeral: Hosted(selection)}
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
