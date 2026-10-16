package cli

import (
	"context"
	"fmt"
	"io"
	"path/filepath"

	"actionlint.kjanat.dev/analyzer"
	"actionlint.kjanat.dev/internal/component"
)

type loadedPlugins struct {
	plugins []*component.Plugin
}

func loadPlugins(ctx context.Context, paths []string, log io.Writer, debug bool) (*loadedPlugins, error) {
	loaded := &loadedPlugins{}
	for _, path := range paths {
		var options component.Options
		if debug {
			name := filepath.Base(path)
			options.Log = func(line string) {
				_, _ = fmt.Fprintf(log, "[plugin %s] %s\n", name, line)
			}
		}
		plugin, err := component.Load(ctx, path, options)
		if err != nil {
			loaded.close(ctx)
			return nil, commandUsageError{err}
		}
		if debug {
			_, _ = fmt.Fprintf(log, "[Linter] Loaded plugin %q from %s (%s)\n", plugin.Descriptor().Name, path, plugin.Limits())
		}
		loaded.plugins = append(loaded.plugins, plugin)
	}
	return loaded, nil
}

func (l *loadedPlugins) registrations() []analyzer.Registration {
	registrations := make([]analyzer.Registration, 0, len(l.plugins))
	for _, plugin := range l.plugins {
		registrations = append(registrations, analyzer.Registration{Analyzer: plugin})
	}
	return registrations
}

func (l *loadedPlugins) close(ctx context.Context) {
	for _, plugin := range l.plugins {
		_ = plugin.Close(ctx)
	}
	l.plugins = nil
}
