// Package executor runs configured analyzer tools under host-owned limits.
package executor

import (
	"context"
	"errors"
	"fmt"
	"slices"

	rulemeta "actionlint.kjanat.dev/v2/internal/checking/rules"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	"actionlint.kjanat.dev/v2/internal/checking/inputs/validation"
	"actionlint.kjanat.dev/v2/internal/results"
	"actionlint.kjanat.dev/v2/internal/results/related"
)

// Run isolates each analyzer's input and joins all started work.
// Findings and errors follow registration order.
func Run(ctx context.Context, input checkinputs.Input, registrations []checkinputs.Registration) ([]results.Diagnostic, error) {
	if err := input.ValidateInvocations(); err != nil {
		return nil, err
	}
	tasks := make([]Task[results.Diagnostic], len(registrations))
	for i, registration := range registrations {
		tasks[i] = func(ctx context.Context) ([]results.Diagnostic, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			prepared, err := Prepare(registration, input.AvailableInputs)
			if err != nil {
				return nil, err
			}
			return RunOne(ctx, input, prepared)
		}
	}
	return RunTasks(ctx, tasks)
}

// PreparedRegistration stores private copies of validated registration metadata.
// Its zero value cannot execute. Analyzer implementations are responsible for
// their own panics and concurrent access to their internal state.
type PreparedRegistration struct {
	data *preparedRegistration
}

type preparedRegistration struct {
	analyzer        checkinputs.Checker
	prepare         func(context.Context, checkinputs.Input) (checkinputs.Input, error)
	descriptor      checkinputs.Descriptor
	options         []byte
	availableInputs []rulemeta.InputKind
	rawErrors       bool
}

// Prepare validates a registration once for an exact set of available input kinds.
func Prepare(registration checkinputs.Registration, available []rulemeta.InputKind) (PreparedRegistration, error) {
	if validation.IsNil(registration.Checker) {
		return PreparedRegistration{}, errors.New("nil analyzer registration")
	}
	descriptor := registration.Checker.Descriptor()
	descriptor.Descriptor = descriptor.Clone()
	options := slices.Clone(registration.Options)
	if descriptor.Name == "" {
		return PreparedRegistration{}, errors.New("analyzer name must not be empty")
	}
	inputKinds, err := validateAvailableInputs(available)
	if err != nil {
		return PreparedRegistration{}, err
	}
	if err := ValidateOptions(descriptor.OptionsSchema, options); err != nil {
		return PreparedRegistration{}, fmt.Errorf("analyzer %q options: %w", descriptor.Name, err)
	}
	for _, requirement := range descriptor.RequiredInputs {
		if !supportedInputKind(requirement) {
			return PreparedRegistration{}, fmt.Errorf("analyzer %q requires unsupported input kind %q", descriptor.Name, requirement)
		}
		if !slices.Contains(inputKinds, requirement) {
			return PreparedRegistration{}, fmt.Errorf("analyzer %q requires unavailable input kind %q", descriptor.Name, requirement)
		}
	}
	return PreparedRegistration{data: &preparedRegistration{analyzer: registration.Checker, prepare: registration.Prepare, descriptor: descriptor, options: options, availableInputs: inputKinds}}, nil
}

// PrepareForwarding admits a transparent adapter's implementation through the
// same executor. Runtime errors use their existing text so an enclosing
// invocation can add its own analyzer context exactly once.
func PrepareForwarding(registration checkinputs.Registration, available []rulemeta.InputKind) (PreparedRegistration, error) {
	prepared, err := Prepare(registration, available)
	if err != nil {
		return PreparedRegistration{}, err
	}
	prepared.data.rawErrors = true
	return prepared, nil
}

func supportedInputKind(kind rulemeta.InputKind) bool {
	switch kind {
	case rulemeta.Source, rulemeta.Scripts, rulemeta.Mappings, rulemeta.Invocations, rulemeta.Execution, rulemeta.Dependencies:
		return true
	default:
		return false
	}
}

func validateAvailableInputs(available []rulemeta.InputKind) ([]rulemeta.InputKind, error) {
	inputKinds := slices.Clone(available)
	slices.Sort(inputKinds)
	for i, kind := range inputKinds {
		if !supportedInputKind(kind) {
			return nil, fmt.Errorf("unsupported available input kind %q", kind)
		}
		if i > 0 && inputKinds[i-1] == kind {
			return nil, fmt.Errorf("duplicate available input kind %q", kind)
		}
	}
	return inputKinds, nil
}

// RunOne isolates one invocation of a prepared registration, preserving partial
// findings and cancellation. Input kinds must match the set validated by Prepare.
func RunOne(ctx context.Context, input checkinputs.Input, registration PreparedRegistration) ([]results.Diagnostic, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := input.ValidateInvocations(); err != nil {
		return nil, err
	}
	if registration.data == nil {
		return nil, errors.New("unprepared analyzer registration")
	}
	inputKinds, err := validateAvailableInputs(input.AvailableInputs)
	if err != nil {
		return nil, err
	}
	if !slices.Equal(inputKinds, registration.data.availableInputs) {
		return nil, errors.New("input available input kinds differ from prepared registration")
	}
	descriptor := registration.data.descriptor
	supplied := input
	supplied.AvailableInputs = slices.Clone(input.AvailableInputs)
	supplied.SelectedScripts = slices.Clone(input.SelectedScripts)
	supplied.Options = slices.Clone(registration.data.options)
	supplied.Scripts = cloneScripts(input.Scripts)
	supplied.Invoked = cloneScripts(input.Invoked)
	supplied.ActionInputs = slices.Clone(input.ActionInputs)
	for i, input := range supplied.ActionInputs {
		supplied.ActionInputs[i].Script = cloneScripts([]checkinputs.Script{input.Script})[0]
	}
	supplied.Dependencies = slices.Clone(input.Dependencies)
	supplied.ResolvedDependencies = slices.Clone(input.ResolvedDependencies)
	supplied.Files = slices.Clone(input.Files)
	supplied.SourceRoots = slices.Clone(input.SourceRoots)
	supplied.Mappings = slices.Clone(input.Mappings)
	for i := range supplied.Mappings {
		supplied.Mappings[i].Entries = slices.Clone(supplied.Mappings[i].Entries)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var preparationErr error
	if registration.data.prepare != nil {
		supplied, preparationErr = registration.data.prepare(ctx, supplied)
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(preparationErr, err)
	}
	findings, err := registration.data.analyzer.CheckDocument(ctx, supplied)
	findings = slices.Clone(findings)
	err = errors.Join(preparationErr, err, related.BindDiagnostics(findings, input.Path, input.Revision, input.Coordinates))
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
	if err != nil && !registration.data.rawErrors {
		return findings, fmt.Errorf("analyzer %q: %w", descriptor.Name, err)
	}
	return findings, err
}

func cloneScripts(scripts []checkinputs.Script) []checkinputs.Script {
	if scripts == nil {
		return nil
	}
	cloned := make([]checkinputs.Script, len(scripts))
	for i, script := range scripts {
		script.Occurrence.Ancestry = slices.Clone(script.Occurrence.Ancestry)
		script.Spans = slices.Clone(script.Spans)
		script.Comments = slices.Clone(script.Comments)
		script.Environment = slices.Clone(script.Environment)
		if script.Runtime != nil {
			runtime := *script.Runtime
			script.Runtime = &runtime
		}
		cloned[i] = script
	}
	return cloned
}
