package checking

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"time"

	"actionlint.kjanat.dev/v2/internal/input/checkrequest"
	"actionlint.kjanat.dev/v2/internal/input/configuration/document"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginselection"
	"actionlint.kjanat.dev/v2/internal/results"
	"actionlint.kjanat.dev/v2/internal/results/report"
)

// CheckProjects discovers inputs once, then loads each repository's plugins in
// its own analysis session. Reports share output destinations, never runtimes or
// configuration. Explicit --config/--plugin selections still apply to every batch.
func CheckProjects(ctx context.Context, options Options, input checkrequest.Request, selection pluginselection.PluginSelection) (*report.Analysis, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if options.ConfigConnections.Transport == nil {
		options.ConfigConnections.Transport = selection.Acquisition.Connections.Transport
	}
	if options.ConfigConnections.Resolver == nil {
		options.ConfigConnections.Resolver = selection.Acquisition.Connections.Resolver
	}
	var err error
	options, err = prepareEnforcedLifecycle(ctx, options)
	if err != nil {
		return nil, err
	}
	options.ExecutionEnvironment = options.captureDate(time.Now)
	if input.Kind == checkrequest.Repository && input.Selection == nil {
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
	check := func(input checkrequest.Request) (*report.Analysis, error) {
		selection, options := selection, options
		// Plugin selection reads configuration from disk. The session shares
		// those files only when it would read the same bytes.
		if options.ReadFile == nil && len(options.SourceOverlays) == 0 {
			files := document.NewFiles()
			selection.ConfigSources, options.configSources = files, files
		}
		sources, err := selection.Resolve(ctx, options.WorkingDir, input)
		if err != nil {
			return nil, err
		}
		result, err := CheckWithPlugins(ctx, options, input, sources)
		return result, err
	}
	if input.Kind != checkrequest.Files || len(input.Paths) == 0 {
		return check(input)
	}
	found, err := workspace.FindFiles(ctx, input.Paths, options.NoIgnore)
	if err != nil {
		return nil, err
	}
	batches := workspace.PartitionFiles(found)
	if len(batches) == 1 {
		result, err := check(checkrequest.FromFiles(batches[0]))
		if result != nil {
			result.Inputs = slices.Compact(slices.Sorted(slices.Values(append(result.Inputs, found.Inputs...))))
		}
		return result, err
	}
	combined := &report.Analysis{Inputs: found.Inputs, Diagnostics: []results.PublishedDiagnostic{}}
	var failures []error
	for _, batch := range batches {
		if err := ctx.Err(); err != nil {
			return combined, errors.Join(append(failures, err)...)
		}
		result, err := check(checkrequest.FromFiles(batch))
		if err != nil {
			failures = append(failures, err)
		}
		if result != nil {
			merged, mergeErr := report.Merge(*combined, *result)
			if mergeErr != nil {
				return combined, errors.Join(append(failures, mergeErr)...)
			}
			*combined = merged
		}
	}
	combined.Inputs = slices.Compact(slices.Sorted(slices.Values(combined.Inputs)))
	return combined, errors.Join(failures...)
}
