package analyzerhost

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"actionlint.kjanat.dev/analyzer"
	"actionlint.kjanat.dev/diagnostic"
	"golang.org/x/sync/errgroup"
)

// Run isolates each analyzer's input and joins all started work. Results and
// errors retain registration order regardless of execution order. A failing
// analyzer does not discard findings from its peers.
func Run(ctx context.Context, input analyzer.Input, registrations []analyzer.Registration) ([]diagnostic.Diagnostic, error) {
	type outcome struct {
		findings []diagnostic.Diagnostic
		err      error
	}
	outcomes := make([]outcome, len(registrations))
	var group errgroup.Group
	group.SetLimit(8)
	for i, registration := range registrations {
		group.Go(func() error {
			if err := ctx.Err(); err != nil {
				outcomes[i].err = err
				return err
			}
			if registration.Analyzer == nil {
				outcomes[i].err = errors.New("nil analyzer registration")
				return nil
			}
			descriptor := registration.Analyzer.Descriptor()
			if descriptor.Name == "" {
				outcomes[i].err = errors.New("analyzer name must not be empty")
				return nil
			}
			for _, requirement := range descriptor.Requires {
				switch requirement {
				case analyzer.Source, analyzer.Scripts, analyzer.Mappings:
					if !slices.Contains(input.Available, requirement) {
						outcomes[i].err = fmt.Errorf("analyzer %q requires unavailable fact %q", descriptor.Name, requirement)
						return nil
					}
				default:
					outcomes[i].err = fmt.Errorf("analyzer %q requires unsupported fact %q", descriptor.Name, requirement)
					return nil
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
			outcomes[i].findings = findings
			if err != nil {
				outcomes[i].err = fmt.Errorf("analyzer %q: %w", descriptor.Name, err)
			}
			return nil
		})
	}
	_ = group.Wait()
	var findings []diagnostic.Diagnostic
	failures := make([]error, 0, len(outcomes))
	for _, result := range outcomes {
		findings = append(findings, result.findings...)
		failures = append(failures, result.err)
	}
	return findings, errors.Join(failures...)
}
