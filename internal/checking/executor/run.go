// Package executor runs configured analyzer tools under host-owned limits.
package executor

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"actionlint.kjanat.dev/v2/internal/model/rule"

	"actionlint.kjanat.dev/v2/internal/checking/inputs"
	"actionlint.kjanat.dev/v2/internal/checking/inputs/validation"
	"actionlint.kjanat.dev/v2/internal/results"
)

// Run isolates each analyzer's input and joins all started work.
// Findings and errors follow registration order.
func Run(ctx context.Context, input inputs.AnalyzerInput, registrations []inputs.Registration) ([]results.Diagnostic, error) {
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
	analyzer        inputs.Checker
	prepare         func(context.Context, inputs.AnalyzerInput) (inputs.AnalyzerInput, error)
	descriptor      inputs.Descriptor
	options         []byte
	availableInputs []rule.InputKind
	rawErrors       bool
}

// Prepare validates a registration once for an exact set of available input kinds.
func Prepare(registration inputs.Registration, available []rule.InputKind) (PreparedRegistration, error) {
	if validation.IsNil(registration.Checker) {
		return PreparedRegistration{}, errors.New("nil analyzer registration")
	}
	descriptor := registration.Checker.Descriptor().Detached()
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
func PrepareForwarding(registration inputs.Registration, available []rule.InputKind) (PreparedRegistration, error) {
	prepared, err := Prepare(registration, available)
	if err != nil {
		return PreparedRegistration{}, err
	}
	prepared.data.rawErrors = true
	return prepared, nil
}

func supportedInputKind(kind rule.InputKind) bool {
	switch kind {
	case rule.Source, rule.Scripts, rule.Mappings, rule.Invocations, rule.Execution, rule.Dependencies:
		return true
	default:
		return false
	}
}

func validateAvailableInputs(available []rule.InputKind) ([]rule.InputKind, error) {
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
func RunOne(ctx context.Context, input inputs.AnalyzerInput, registration PreparedRegistration) ([]results.Diagnostic, error) {
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
	supplied.Options = nil
	supplied = supplied.Detached()
	supplied.Options = slices.Clone(registration.data.options)
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
	for i := range findings {
		findings[i] = findings[i].Clone()
	}
	err = errors.Join(preparationErr, err, results.BindRelatedInDiagnostics(findings, input.Path, input.Revision, input.Coordinates))
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
