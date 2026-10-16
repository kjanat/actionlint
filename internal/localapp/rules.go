package localapp

import (
	"context"
	"errors"
	"io"

	"actionlint.kjanat.dev/internal/rulecatalog"
	"actionlint.kjanat.dev/rule"
)

// Rules discovers selected providers without analyzing a document.
func Rules(ctx context.Context, paths []string) (result []rule.Descriptor, err error) {
	plugins, err := loadPlugins(ctx, paths, io.Discard, false)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, plugins.close(context.WithoutCancel(ctx))) }()
	var extensions []rule.Descriptor
	for _, plugin := range plugins.plugins {
		extensions = append(extensions, plugin.Descriptor().Descriptor)
	}
	result, err = rulecatalog.Combine(extensions...)
	if err != nil {
		return nil, err
	}
	for _, engine := range plugins.engines {
		for _, descriptor := range engine.Metadata().Rules {
			// A script backend implements its host rule; it does not add a second rule ID.
			if descriptor.Name != engine.Metadata().Engine.Name {
				continue
			}
			for i := range result {
				if result[i].Name == descriptor.Name {
					result[i] = descriptor.Clone()
				}
			}
		}
	}
	return result, nil
}
