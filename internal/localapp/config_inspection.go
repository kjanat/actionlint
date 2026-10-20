package localapp

import (
	"context"
	"errors"
	"io"

	"actionlint.kjanat.dev/internal/config"
)

// InspectConfig validates settings against the explicitly selected plugin rules.
func InspectConfig(ctx context.Context, selection config.Selection, paths []string, origins bool) (inspection config.Inspection, err error) {
	if err := ctx.Err(); err != nil {
		return inspection, err
	}
	plugins, err := loadPlugins(ctx, paths, io.Discard, false)
	if err != nil {
		return inspection, err
	}
	defer func() { err = errors.Join(err, plugins.close(context.WithoutCancel(ctx))) }()
	rules := make([]string, 0, len(plugins.plugins)+len(plugins.engines))
	for _, plugin := range plugins.plugins {
		rules = append(rules, plugin.Descriptor().Name)
	}
	for _, engine := range plugins.engines {
		rules = append(rules, engine.Metadata().Engine.Name)
	}
	if err := ctx.Err(); err != nil {
		return inspection, err
	}
	return config.Inspect(selection, origins, rules...)
}
