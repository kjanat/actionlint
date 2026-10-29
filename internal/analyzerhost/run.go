package analyzerhost

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/analyzerref"
	"actionlint.kjanat.dev/v2/internal/related"
)

// Run isolates each analyzer's input and joins all started work.
// Findings and errors follow registration order.
func Run(ctx context.Context, input analyzer.Input, registrations []analyzer.Registration) ([]diagnostic.Diagnostic, error) {
	if err := input.ValidateInvocations(); err != nil {
		return nil, err
	}
	tasks := make([]Task[diagnostic.Diagnostic], len(registrations))
	for i, registration := range registrations {
		tasks[i] = func(ctx context.Context) ([]diagnostic.Diagnostic, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			prepared, err := Prepare(registration, input.Available)
			if err != nil {
				return nil, err
			}
			return RunOne(ctx, input, prepared)
		}
	}
	return RunTasks(ctx, tasks)
}

// PreparedRegistration retains private copies of validated registration metadata.
// Its zero value cannot execute. Analyzer implementations remain responsible for
// their own panics and concurrent access to their internal state.
type PreparedRegistration struct {
	data *preparedRegistration
}

type preparedRegistration struct {
	analyzer   analyzer.Analyzer
	descriptor analyzer.Descriptor
	options    []byte
	available  []analyzer.Fact
}

// Prepare validates a registration once for an exact set of available facts.
func Prepare(registration analyzer.Registration, available []analyzer.Fact) (PreparedRegistration, error) {
	if analyzerref.IsNil(registration.Analyzer) {
		return PreparedRegistration{}, errors.New("nil analyzer registration")
	}
	descriptor := registration.Analyzer.Descriptor()
	descriptor.Requires = slices.Clone(descriptor.Requires)
	descriptor.Descriptor = descriptor.Clone()
	options := slices.Clone(registration.Options)
	if descriptor.Name == "" {
		return PreparedRegistration{}, errors.New("analyzer name must not be empty")
	}
	facts, err := availableFacts(available)
	if err != nil {
		return PreparedRegistration{}, err
	}
	if err := ValidateOptions(descriptor.OptionsSchema, options); err != nil {
		return PreparedRegistration{}, fmt.Errorf("analyzer %q options: %w", descriptor.Name, err)
	}
	for _, requirement := range descriptor.Requires {
		if !supportedFact(requirement) {
			return PreparedRegistration{}, fmt.Errorf("analyzer %q requires unsupported fact %q", descriptor.Name, requirement)
		}
		if !slices.Contains(facts, requirement) {
			return PreparedRegistration{}, fmt.Errorf("analyzer %q requires unavailable fact %q", descriptor.Name, requirement)
		}
	}
	return PreparedRegistration{data: &preparedRegistration{analyzer: registration.Analyzer, descriptor: descriptor, options: options, available: facts}}, nil
}

func supportedFact(fact analyzer.Fact) bool {
	switch fact {
	case analyzer.Source, analyzer.Scripts, analyzer.Mappings, analyzer.Invocations, analyzer.Execution, analyzer.Dependencies:
		return true
	default:
		return false
	}
}

func availableFacts(available []analyzer.Fact) ([]analyzer.Fact, error) {
	facts := slices.Clone(available)
	slices.Sort(facts)
	for i, fact := range facts {
		if !supportedFact(fact) {
			return nil, fmt.Errorf("unsupported available fact %q", fact)
		}
		if i > 0 && facts[i-1] == fact {
			return nil, fmt.Errorf("duplicate available fact %q", fact)
		}
	}
	return facts, nil
}

// RunOne isolates one invocation of a prepared registration, retaining partial
// findings and cancellation. Input facts must match the set validated by Prepare.
func RunOne(ctx context.Context, input analyzer.Input, registration PreparedRegistration) ([]diagnostic.Diagnostic, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := input.ValidateInvocations(); err != nil {
		return nil, err
	}
	if registration.data == nil {
		return nil, errors.New("unprepared analyzer registration")
	}
	facts, err := availableFacts(input.Available)
	if err != nil {
		return nil, err
	}
	if !slices.Equal(facts, registration.data.available) {
		return nil, errors.New("input available facts differ from prepared registration")
	}
	descriptor := registration.data.descriptor
	supplied := input
	supplied.Available = slices.Clone(input.Available)
	supplied.SelectedScripts = slices.Clone(input.SelectedScripts)
	supplied.Options = slices.Clone(registration.data.options)
	supplied.Scripts = cloneScripts(input.Scripts)
	supplied.Invoked = cloneScripts(input.Invoked)
	supplied.ActionInputs = slices.Clone(input.ActionInputs)
	for i, input := range supplied.ActionInputs {
		supplied.ActionInputs[i].Script = cloneScripts([]analyzer.Script{input.Script})[0]
	}
	supplied.Dependencies = slices.Clone(input.Dependencies)
	supplied.Files = slices.Clone(input.Files)
	supplied.SourceRoots = slices.Clone(input.SourceRoots)
	supplied.Mappings = slices.Clone(input.Mappings)
	for i := range supplied.Mappings {
		supplied.Mappings[i].Entries = slices.Clone(supplied.Mappings[i].Entries)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	findings, err := registration.data.analyzer.Analyze(ctx, supplied)
	findings = slices.Clone(findings)
	err = errors.Join(err, related.BindDiagnostics(findings, input.Path, input.Revision, input.Coordinates))
	if canceled := ctx.Err(); canceled != nil && !errors.Is(err, canceled) {
		err = errors.Join(err, canceled)
	}
	for i := range findings {
		if findings[i].Explanation == "" {
			findings[i].Explanation = descriptor.Explanation
		}
		if findings[i].Help == "" {
			findings[i].Help = descriptor.Help
		}
		if findings[i].DocumentationURL == "" {
			findings[i].DocumentationURL = descriptor.DocumentationURL
		}
		if findings[i].Rule == "" {
			findings[i].Rule = descriptor.Name
		}
		if findings[i].Path == "" {
			findings[i].Path = input.Path
		}
	}
	if err != nil {
		return findings, fmt.Errorf("analyzer %q: %w", descriptor.Name, err)
	}
	return findings, nil
}

func cloneScripts(scripts []analyzer.Script) []analyzer.Script {
	if scripts == nil {
		return nil
	}
	cloned := make([]analyzer.Script, len(scripts))
	for i, script := range scripts {
		script.Occurrence.Ancestry = slices.Clone(script.Occurrence.Ancestry)
		script.Spans = slices.Clone(script.Spans)
		script.Comments = slices.Clone(script.Comments)
		if script.Runtime != nil {
			runtime := *script.Runtime
			script.Runtime = &runtime
		}
		cloned[i] = script
	}
	return cloned
}
