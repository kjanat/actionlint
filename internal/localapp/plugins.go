package localapp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"actionlint.kjanat.dev/analyzer"
	"actionlint.kjanat.dev/internal/component"
	"actionlint.kjanat.dev/internal/corewasm"
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
			return nil, &PluginLoadError{errors.Join(err, loaded.close(context.WithoutCancel(ctx)))}
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

func (l *loadedPlugins) close(ctx context.Context) error {
	var err error
	for _, plugin := range l.plugins {
		err = errors.Join(err, plugin.Close(ctx))
	}
	l.plugins = nil
	return err
}

func scriptEngineLoader(log io.Writer, debug bool) func(context.Context, string) (analyzer.ScriptBackend, error) {
	return func(ctx context.Context, path string) (analyzer.ScriptBackend, error) {
		options := corewasm.Options{CacheDir: engineCacheDir()}
		if debug && options.CacheDir == "" {
			_, _ = fmt.Fprintln(log, "[Linter] No user cache directory; compiling WebAssembly engines without a cache")
		}
		engine, err := corewasm.Load(ctx, path, options)
		if err != nil {
			return nil, err
		}
		if debug {
			metadata := engine.Metadata()
			_, _ = fmt.Fprintf(log, "[Linter] Loaded engine %q (%s %s) from %s (%s)\n", metadata.ID, metadata.Engine.Name, metadata.Engine.Version, path, engine.Limits())
		}
		return engine, nil
	}
}

func engineCacheDir() string {
	base, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	dir := filepath.Join(base, "actionlint", "wazero")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ""
	}
	return dir
}
