package localapp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"

	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/report"
	"actionlint.kjanat.dev/v2/internal/workspace"
	"actionlint.kjanat.dev/v2/rule"
)

// CheckProjects discovers inputs once, then loads each repository's plugins in
// its own analysis session. Reports share output destinations, never runtimes or
// configuration. Explicit --config/--plugin selections still apply to every batch.
func CheckProjects(ctx context.Context, options application.Options, input application.Input, selection PluginSelection) (*application.Result, error) {
	check := func(input application.Input) (*application.Result, error) {
		sources, err := selection.Resolve(ctx, options.WorkingDir, input)
		if err != nil {
			return nil, err
		}
		result, err := CheckSources(ctx, options, input, sources)
		if result != nil {
			result.Inputs = append(result.Inputs, sources.Inputs...)
		}
		return result, err
	}
	if input.Kind != application.Files || len(input.Paths) == 0 {
		return check(input)
	}
	found, err := workspace.FindFiles(ctx, input.Paths, options.NoIgnore)
	if err != nil {
		return nil, err
	}
	var batches []workspace.FileSelection
	indices := make(map[string]int)
	for _, file := range found.Files {
		root := pluginProjectRoot(file.Path)
		index, exists := indices[root]
		if !exists {
			index = len(batches)
			indices[root] = index
			batches = append(batches, workspace.FileSelection{})
		}
		batches[index].Files = append(batches[index].Files, file)
	}
	if len(batches) == 1 {
		result, err := check(application.FileInput(batches[0]))
		if result != nil {
			result.Inputs = slices.Compact(slices.Sorted(slices.Values(append(result.Inputs, found.Inputs...))))
		}
		return result, err
	}
	combined := &application.Result{Inputs: found.Inputs, Diagnostics: []diagnostic.Diagnostic{}}
	var failures []error
	for _, batch := range batches {
		if err := ctx.Err(); err != nil {
			return combined, errors.Join(append(failures, err)...)
		}
		result, err := check(application.FileInput(batch))
		if err != nil {
			failures = append(failures, err)
		}
		if result != nil {
			if err := combineProjectResult(combined, result); err != nil {
				return combined, errors.Join(append(failures, err)...)
			}
		}
	}
	combined.Inputs = slices.Compact(slices.Sorted(slices.Values(combined.Inputs)))
	return combined, errors.Join(failures...)
}

// Refuse conflicting evidence instead of binding findings from independent
// sessions to a different revision or a different plugin's rule description.
func combineProjectResult(combined, result *application.Result) error {
	for path, captured := range result.CapturedSources {
		if previous, exists := combined.CapturedSources[path]; exists &&
			(previous.Revision != captured.Revision || previous.Coordinates != captured.Coordinates ||
				previous.CoordinatesAvailable != captured.CoordinatesAvailable || !bytes.Equal(previous.Content, captured.Content)) {
			return fmt.Errorf("source %q changed between repository analyses", path)
		}
	}
	for _, descriptor := range result.Rules {
		for _, previous := range combined.Rules {
			if previous.Name == descriptor.Name && !reflect.DeepEqual(previous, descriptor) {
				return fmt.Errorf("repositories provide conflicting metadata for rule %q; check them separately", descriptor.Name)
			}
		}
	}
	if combined.CapturedSources == nil {
		combined.CapturedSources = make(map[string]report.CapturedSource)
	}
	maps.Copy(combined.CapturedSources, result.CapturedSources)
	for _, descriptor := range result.Rules {
		if !slices.ContainsFunc(combined.Rules, func(previous rule.Descriptor) bool { return previous.Name == descriptor.Name }) {
			combined.Rules = append(combined.Rules, descriptor)
		}
	}
	combined.FileCount += result.FileCount
	combined.Diagnostics = append(combined.Diagnostics, result.Diagnostics...)
	combined.Sources = append(combined.Sources, result.Sources...)
	combined.ActionMetadata = append(combined.ActionMetadata, result.ActionMetadata...)
	combined.RunnerEvidence = append(combined.RunnerEvidence, result.RunnerEvidence...)
	combined.Documents = append(combined.Documents, result.Documents...)
	combined.Configurations = append(combined.Configurations, result.Configurations...)
	combined.Inputs = append(combined.Inputs, result.Inputs...)
	return nil
}
