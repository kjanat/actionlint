package checking

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"reflect"
	"slices"

	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
	inputmodel "actionlint.kjanat.dev/v2/internal/input"
	"actionlint.kjanat.dev/v2/internal/input/discovery"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
	plugininstall "actionlint.kjanat.dev/v2/internal/plugins/installation"
	"actionlint.kjanat.dev/v2/internal/results"
	"actionlint.kjanat.dev/v2/internal/results/report"
)

// CheckProjects discovers inputs once, then loads each repository's plugins in
// its own analysis session. Reports share output destinations, never runtimes or
// configuration. Explicit --config/--plugin selections still apply to every batch.
func CheckProjects(ctx context.Context, options Options, input inputmodel.Request, selection plugininstall.Selection) (*Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if input.Kind == inputmodel.Repository && input.Selection == nil {
		directory := input.Directory
		if directory == "" {
			directory = options.WorkingDir
		} else if !filepath.IsAbs(directory) {
			directory = filepath.Join(options.WorkingDir, directory)
		}
		found, err := workspace.FindRepositoryFiles(ctx, directory, options.NoIgnore)
		if err != nil {
			return nil, err
		}
		input.Selection = &found
		if len(found.Files) == 0 {
			selection.DefaultTools = nil
		}
	}
	check := func(input inputmodel.Request) (*Result, error) {
		sources, err := selection.Resolve(ctx, options.WorkingDir, input)
		if err != nil {
			return nil, err
		}
		result, err := CheckWithPlugins(ctx, options, input, sources)
		if result != nil {
			result.Inputs = append(result.Inputs, sources.Inputs...)
		}
		return result, err
	}
	if input.Kind != inputmodel.Files || len(input.Paths) == 0 {
		return check(input)
	}
	found, err := workspace.FindFiles(ctx, input.Paths, options.NoIgnore)
	if err != nil {
		return nil, err
	}
	var batches []workspace.FileSelection
	indices := make(map[string]int)
	for _, file := range found.Files {
		root := discovery.PhysicalRepositoryRoot(file.Path)
		index, exists := indices[root]
		if !exists {
			index = len(batches)
			indices[root] = index
			batches = append(batches, workspace.FileSelection{})
		}
		batches[index].Files = append(batches[index].Files, file)
	}
	if len(batches) == 1 {
		result, err := check(inputmodel.FromFiles(batches[0]))
		if result != nil {
			result.Inputs = slices.Compact(slices.Sorted(slices.Values(append(result.Inputs, found.Inputs...))))
		}
		return result, err
	}
	combined := &Result{Inputs: found.Inputs, Diagnostics: []results.Diagnostic{}}
	var failures []error
	for _, batch := range batches {
		if err := ctx.Err(); err != nil {
			return combined, errors.Join(append(failures, err)...)
		}
		result, err := check(inputmodel.FromFiles(batch))
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

// combineProjectResult rejects conflicting source captures and rule metadata
// before combining results from different repositories.
func combineProjectResult(combined, result *Result) error {
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
	combined.RunnerResults = append(combined.RunnerResults, result.RunnerResults...)
	combined.Documents = append(combined.Documents, result.Documents...)
	combined.Configurations = append(combined.Configurations, result.Configurations...)
	combined.Inputs = append(combined.Inputs, result.Inputs...)
	return nil
}
