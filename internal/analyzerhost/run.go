package analyzerhost

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/diagnostic"
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
			if err := ValidateOptions(descriptor.OptionsSchema, registration.Options); err != nil {
				return nil, fmt.Errorf("analyzer %q options: %w", descriptor.Name, err)
			}
			for _, requirement := range descriptor.Requires {
				switch requirement {
				case analyzer.Source, analyzer.Scripts, analyzer.Mappings, analyzer.Invocations, analyzer.Execution, analyzer.Dependencies:
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
			findings, err := registration.Analyzer.Analyze(ctx, supplied)
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
	}
	return RunTasks(ctx, tasks)
}

func cloneScripts(scripts []analyzer.Script) []analyzer.Script {
	if scripts == nil {
		return nil
	}
	cloned := make([]analyzer.Script, len(scripts))
	for i, script := range scripts {
		script.Spans = slices.Clone(script.Spans)
		script.Comments = slices.Clone(script.Comments)
		if script.Invocation != nil {
			invocation := *script.Invocation
			script.Invocation = &invocation
		}
		if script.Runtime != nil {
			runtime := *script.Runtime
			script.Runtime = &runtime
		}
		cloned[i] = script
	}
	return cloned
}
