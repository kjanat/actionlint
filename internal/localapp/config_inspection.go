package localapp

import (
	"context"
	"errors"
	"io"
	"os"

	"actionlint.kjanat.dev/internal/application"
	"actionlint.kjanat.dev/internal/config"
)

// InspectConfig validates rule names against explicitly selected or project plugins.
func InspectConfig(ctx context.Context, selection config.Selection, paths []string, origins bool) (inspection config.Inspection, err error) {
	if err := ctx.Err(); err != nil {
		return inspection, err
	}
	path, err := config.SelectedPath(selection)
	if err != nil {
		return inspection, err
	}
	inspection.Path = path
	directory, err := os.Getwd()
	if err != nil {
		return inspection, err
	}
	sources, err := (PluginSelection{Paths: paths, ConfigFile: selection.Path, Disabled: selection.Disabled}).Resolve(ctx, directory, application.Input{Kind: application.Repository})
	if err != nil {
		return inspection, err
	}
	plugins, err := loadPlugins(ctx, sources.Paths, io.Discard, false)
	if err != nil {
		return inspection, err
	}
	defer func() { err = errors.Join(err, plugins.close(context.WithoutCancel(ctx))) }()
	rules := make([]string, 0, len(plugins.plugins)+len(plugins.engines))
	for _, plugin := range plugins.plugins {
		if plugin.HasAnalyzer() {
			rules = append(rules, plugin.Descriptor().Name)
		}
	}
	for _, engine := range plugins.engines {
		rules = append(rules, engine.Metadata().Engine.Name)
	}
	if err := ctx.Err(); err != nil {
		return inspection, err
	}
	return config.Inspect(selection, origins, rules...)
}
