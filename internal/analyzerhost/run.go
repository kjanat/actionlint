package analyzerhost

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"actionlint.kjanat.dev/analyzer"
	"actionlint.kjanat.dev/diagnostic"
)

// Run isolates each analyzer's input and joins all started work. Results and
// errors retain registration order regardless of execution order. A failing
// analyzer does not discard findings from its peers.
func Run(ctx context.Context, input analyzer.Input, registrations []analyzer.Registration) ([]diagnostic.Diagnostic, error) {
	tasks := make([]Task[diagnostic.Diagnostic], len(registrations))
	for i, registration := range registrations {
		tasks[i] = func(ctx context.Context) ([]diagnostic.Diagnostic, error) {
			if registration.Analyzer == nil {
				return nil, errors.New("nil analyzer registration")
			}
			descriptor := registration.Analyzer.Descriptor()
			if descriptor.Name == "" {
				return nil, errors.New("analyzer name must not be empty")
			}
			for _, requirement := range descriptor.Requires {
				switch requirement {
				case analyzer.Source, analyzer.Scripts, analyzer.Mappings:
					if !slices.Contains(input.Available, requirement) {
						return nil, fmt.Errorf("analyzer %q requires unavailable fact %q", descriptor.Name, requirement)
					}
				default:
					return nil, fmt.Errorf("analyzer %q requires unsupported fact %q", descriptor.Name, requirement)
				}
			}
			supplied := input
			supplied.Available = slices.Clone(input.Available)
			supplied.Options = slices.Clone(registration.Options)
			supplied.Scripts = slices.Clone(input.Scripts)
			supplied.Mappings = slices.Clone(input.Mappings)
			for i := range supplied.Mappings {
				supplied.Mappings[i].Entries = slices.Clone(supplied.Mappings[i].Entries)
			}
			findings, err := registration.Analyzer.Analyze(ctx, supplied)
			for i := range findings {
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
	}
	return RunTasks(ctx, tasks)
}
