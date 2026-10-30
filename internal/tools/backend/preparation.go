package backend

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/tools"
	"actionlint.kjanat.dev/v2/internal/tools/profile"
	"actionlint.kjanat.dev/v2/internal/tools/profile/mask"
	pythonprofile "actionlint.kjanat.dev/v2/internal/tools/profile/python"
	shellprofile "actionlint.kjanat.dev/v2/internal/tools/profile/shell"
	"actionlint.kjanat.dev/v2/internal/tools/provider"
	"actionlint.kjanat.dev/v2/internal/tools/provider/process"
)

// route is the transport a preparation feeds.
type route uint8

const (
	// routeProcess feeds a native process: settings travel as argument
	// variables and configuration files.
	routeProcess route = iota
	// routeEmbedded feeds a process function without a filesystem: no
	// configuration file is read and no source is followed.
	routeEmbedded
	// routeEngine feeds a snippet engine, which receives its settings as
	// configuration and reads no files.
	routeEngine
)

// preparation binds a manifest's preparation profile to configuration. It
// reads tools.<name> by the provider's rule name and hands the profile typed
// values; the profile itself never reads configuration.
type preparation interface {
	// batch prepares the run blocks of one batch. Errors in a scope's
	// settings are reported once, here; its scripts are then skipped.
	batch(tools.Batch) (func(tools.Script) (profile.Prepared, error), error)
	// input prepares an action input for a snippet engine.
	input(tools.Script) (profile.Prepared, error)
}

// knownPreparation reports whether the host implements a preparation
// profile. An empty name selects the expression mask.
func knownPreparation(id string) bool {
	switch id {
	case "", mask.ID, shellprofile.ID, pythonprofile.ID:
		return true
	default:
		return false
	}
}

// newPreparation binds the profile a manifest names; RegisterProvider
// rejects profiles the host does not implement. The expression mask passes
// inline settings through.
func newPreparation(m provider.Manifest, via route) preparation {
	name := m.Rule.Name
	switch m.Preparation {
	case shellprofile.ID:
		files := provider.ConfigFiles{}
		if m.Process != nil && m.Process.ConfigFiles != nil {
			files = *m.Process.ConfigFiles
		}
		return shellPreparation{name: name, files: files, route: via}
	case pythonprofile.ID:
		return pythonPreparation{name: name, route: via}
	default:
		return genericPreparation{name: name}
	}
}

// inlineSettings is tools.<name>.config as JSON, or nil when it holds no
// inline settings.
func inlineSettings(cfg *config.Config, name string) (json.RawMessage, error) {
	if cfg == nil {
		return nil, nil
	}
	values := cfg.Tools[name].Config.Inline()
	if values == nil {
		return nil, nil
	}
	data, err := json.Marshal(values)
	if err != nil {
		return nil, fmt.Errorf("tools.%s.config: %w", name, err)
	}
	return data, nil
}

// errEngineConfigFile rejects a configuration file for a snippet engine,
// which reads no files.
var errEngineConfigFile = errors.New("a snippet engine needs inline configuration; a configuration file requires a process backend")

// toolEnabled reports whether configuration leaves a tool on: its rule is
// not off and tools.<name>.enabled is not false.
func toolEnabled(cfg *config.Config, name string) bool {
	if cfg == nil {
		return true
	}
	enabled := cfg.Tools[name].Enabled
	return config.DiagnosticLevel(cfg, name) != "off" && (enabled == nil || *enabled)
}

// configuredInput masks expressions and passes inline tools.<name>.config
// settings to the engine.
func configuredInput(name string, script tools.Script) (profile.Prepared, error) {
	prepared := profile.Prepared{Source: mask.Expressions(script.Text)}
	if script.Config == nil {
		return prepared, nil
	}
	if script.Config.Tools[name].Config.Path() != "" {
		return profile.Prepared{}, fmt.Errorf("tools.%s.config: %w", name, errConfigFilePath)
	}
	settings, err := inlineSettings(script.Config, name)
	prepared.Config = settings
	return prepared, err
}

// errConfigFilePath rejects a configuration file for a provider whose
// preparation reads none, which receives only inline settings.
var errConfigFilePath = errors.New("a tool provider without a preparation profile takes inline settings, not a configuration file path")

// genericPreparation masks expressions and passes inline settings to the
// engine.
type genericPreparation struct{ name string }

func (p genericPreparation) batch(tools.Batch) (func(tools.Script) (profile.Prepared, error), error) {
	return p.input, nil
}

func (p genericPreparation) input(script tools.Script) (profile.Prepared, error) {
	return configuredInput(p.name, script)
}

// pythonPreparation binds the Python profile.
type pythonPreparation struct {
	name  string
	route route
}

func (p pythonPreparation) batch(tools.Batch) (func(tools.Script) (profile.Prepared, error), error) {
	return func(script tools.Script) (profile.Prepared, error) {
		if script.Input != nil {
			return p.input(script)
		}
		prepared, err := pythonprofile.Run(script.Text, script.Shell)
		if err != nil {
			return profile.Prepared{}, fmt.Errorf("%s could not check Python script at %s: %w", p.name, script.Key, err)
		}
		if prepared.Skip != "" {
			return prepared, nil
		}
		settings, err := inlineSettings(script.Config, p.name)
		if err != nil {
			return profile.Prepared{}, err
		}
		prepared.Config = settings
		if p.route == routeEngine {
			if prepared.Config, err = pythonprofile.EngineConfig(settings, prepared.Vars); err != nil {
				return profile.Prepared{}, fmt.Errorf("tools.%s.config: %w", p.name, err)
			}
		}
		return prepared, nil
	}, nil
}

func (p pythonPreparation) input(script tools.Script) (profile.Prepared, error) {
	prepared, err := pythonprofile.Script(script.Text)
	if err != nil || prepared.Skip != "" {
		return prepared, err
	}
	settings, err := inlineSettings(script.Config, p.name)
	if err != nil {
		return profile.Prepared{}, err
	}
	if prepared.Config, err = pythonprofile.EngineConfig(settings, nil); err != nil {
		return profile.Prepared{}, fmt.Errorf("tools.%s.config: %w", p.name, err)
	}
	return prepared, nil
}

// shellPreparation binds the shell profile: inline settings become
// directives, a configuration path selects the native configuration file,
// and path interpolations resolve against each script's scope.
type shellPreparation struct {
	name  string
	files provider.ConfigFiles
	route route
}

// scopeSettings are a scope's resolved shell settings.
type scopeSettings struct {
	directives *shellprofile.Directives
	configFile string
	// failed marks a scope whose settings were reported as an error.
	failed bool
}

func (p shellPreparation) batch(batch tools.Batch) (func(tools.Script) (profile.Prepared, error), error) {
	settings := map[*tools.Scope]scopeSettings{}
	var failure, deferred error
	var deferredScope *tools.Scope
	compositePrepared := false
	for _, scope := range batch.Scopes {
		if scope == nil || !toolEnabled(scope.Config, p.name) {
			continue
		}
		resolved, err := p.scope(scope)
		if err != nil {
			settings[scope] = scopeSettings{failed: true}
			// A workflow scope cannot interpolate the action path; report
			// it only when no composite scope could and a script needs it.
			if !scope.Composite && errors.Is(err, config.ErrActionPathUnavailable) {
				deferred, deferredScope = err, scope
				continue
			}
			failure = errors.Join(failure, err)
			continue
		}
		settings[scope] = resolved
		compositePrepared = compositePrepared || scope.Composite
	}
	if deferred != nil && !compositePrepared {
		failure, deferredScope = errors.Join(failure, deferred), nil
	}
	return func(script tools.Script) (profile.Prepared, error) {
		if script.Input != nil {
			return p.input(script)
		}
		resolved, ok := settings[script.Scope]
		if !ok {
			var err error
			if resolved, err = p.scope(script.Scope); err != nil {
				return profile.Prepared{}, err
			}
			settings[script.Scope] = resolved
		}
		if resolved.failed {
			if script.Scope == deferredScope {
				deferredScope = nil
				return profile.Prepared{}, deferred
			}
			return profile.Prepared{Skip: "its tool settings are invalid"}, nil
		}
		run := shellprofile.Run{
			Shell:      shellprofile.Shell{Name: script.Execution.Shell, Implicit: script.Execution.ImplicitShell},
			Directives: resolved.directives, ConfigFile: resolved.configFile,
			Sources: p.route == routeProcess && script.Execution.DirectoryKnown, Engine: p.route == routeEngine,
		}
		return shellprofile.PrepareRun(run, script.Text)
	}, failure
}

// scope resolves the directives and configuration file of a scope.
func (p shellPreparation) scope(scope *tools.Scope) (scopeSettings, error) {
	paths := scopePaths(scope)
	var cfg *config.Config
	if scope != nil {
		cfg = scope.Config
	}
	var resolved scopeSettings
	inline, err := inlineSettings(cfg, p.name)
	if err != nil {
		return scopeSettings{}, err
	}
	if inline != nil {
		if resolved.directives, err = shellprofile.ParseDirectives(inline); err != nil {
			return scopeSettings{}, fmt.Errorf("tools.%s.config.%w", p.name, err)
		}
		expanded := *resolved.directives
		expanded.SourcePath = make([]string, len(resolved.directives.SourcePath))
		for i, path := range resolved.directives.SourcePath {
			if expanded.SourcePath[i], err = paths.context(cfg).Expand(path); err != nil {
				return scopeSettings{}, fmt.Errorf("tools.%s.config.source-path: %w", p.name, err)
			}
		}
		resolved.directives = &expanded
	}
	if cfg == nil {
		return resolved, nil
	}
	settings := cfg.Tools[p.name].Config
	path := settings.Path()
	switch {
	case p.route == routeEngine:
		if path != "" {
			return scopeSettings{}, fmt.Errorf("tools.%s.config: %w", p.name, errEngineConfigFile)
		}
		if d := resolved.directives; d != nil && (len(d.SourcePath) != 0 || d.ExternalSources != nil && *d.ExternalSources) {
			return scopeSettings{}, fmt.Errorf("tools.%s.config: %w", p.name, shellprofile.ErrNoSourceAccess)
		}
	case p.route == routeEmbedded || path == "":
	default:
		base := paths.context(cfg).ConfigDir
		if settings.FromInput() {
			base = paths.analysis
		}
		file, err := paths.context(cfg).ResolveFrom(path, base)
		if err == nil && scope != nil && scope.Reader != nil {
			file, err = process.ConfigFile(scope.Reader, p.files, file, "")
		}
		if err != nil {
			return scopeSettings{}, fmt.Errorf("tools.%s.config: %w", p.name, err)
		}
		resolved.configFile = file
	}
	return resolved, nil
}

func (p shellPreparation) input(script tools.Script) (profile.Prepared, error) {
	dialect := ""
	if script.Input != nil {
		dialect = shellprofile.LanguageDialect(script.Input.Language)
	}
	if script.Config != nil && script.Config.Tools[p.name].Config.Path() != "" {
		return profile.Prepared{}, fmt.Errorf("tools.%s.config: %w", p.name, errEngineConfigFile)
	}
	inline, err := inlineSettings(script.Config, p.name)
	if err != nil {
		return profile.Prepared{}, err
	}
	var directives *shellprofile.Directives
	if inline != nil {
		if directives, err = shellprofile.ParseDirectives(inline); err != nil {
			return profile.Prepared{}, fmt.Errorf("tools.%s.config.%w", p.name, err)
		}
	}
	prepared, err := shellprofile.EngineInput(script.Text, dialect, directives)
	if err != nil {
		return profile.Prepared{}, fmt.Errorf("tools.%s.config: %w", p.name, err)
	}
	return prepared, nil
}

// runPaths are the directories a scope's path interpolations resolve
// against.
type runPaths struct {
	analysis, workspace, actionPath string
}

// scopePaths defaults the analysis directory to the working directory and
// the workspace to the analysis directory.
func scopePaths(scope *tools.Scope) runPaths {
	var paths runPaths
	if scope != nil {
		paths = runPaths{analysis: scope.AnalysisDir, workspace: scope.Workspace, actionPath: scope.ActionPath}
	}
	if paths.analysis == "" {
		if cwd, err := os.Getwd(); err == nil {
			paths.analysis = cwd
		}
	}
	if paths.workspace == "" {
		paths.workspace = paths.analysis
	}
	return paths
}

// PathContext is the path interpolation context of configuration cfg in a
// script scope: its directory, the workspace and the runner's view of the
// action path.
func PathContext(scope *tools.Scope, cfg *config.Config) config.PathContext {
	return scopePaths(scope).context(cfg)
}

// context is the path interpolation context of a scope's configuration.
func (paths runPaths) context(cfg *config.Config) config.PathContext {
	configDir := paths.workspace
	if cfg.Path() != "" {
		configDir = filepath.Dir(cfg.Path())
	}
	return config.PathContext{ConfigDir: configDir, GitDir: paths.workspace, Workspace: shellprofile.RunnerWorkspace(paths.workspace), ActionPath: shellprofile.RunnerActionPath(paths.actionPath)}
}
