package scripts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	tools "actionlint.kjanat.dev/v2/internal/checking/scripts/contract"
	config "actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/configuration/address"
	inputfiles "actionlint.kjanat.dev/v2/internal/input/files"
	integrationconfig "actionlint.kjanat.dev/v2/internal/plugins/execution/configuration"
	scriptpreparation "actionlint.kjanat.dev/v2/internal/plugins/execution/preparation"
	"actionlint.kjanat.dev/v2/internal/plugins/execution/preparation/mask"
	pythonprofile "actionlint.kjanat.dev/v2/internal/plugins/execution/preparation/python"
	shellprofile "actionlint.kjanat.dev/v2/internal/plugins/execution/preparation/shell"
	"actionlint.kjanat.dev/v2/internal/plugins/execution/process"
	"actionlint.kjanat.dev/v2/internal/plugins/manifests"
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

// inputPreparer binds a manifest's preparation profile to configuration. It
// reads tools.<name> by the provider's rule name and hands the profile typed
// values; the profile itself never reads configuration.
type inputPreparer interface {
	// batch prepares the run blocks of one batch. Errors in a scope's
	// settings are reported once, here; its scripts are then skipped.
	batch(context.Context, tools.Batch) (func(context.Context, tools.Script) (scriptpreparation.Prepared, error), error)
	// input prepares an action input for a snippet engine.
	input(context.Context, tools.Script) (scriptpreparation.Prepared, error)
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
func newPreparation(m manifests.Manifest, via route, programs manifests.Programs) inputPreparer {
	name := m.Rule.Name
	switch m.Preparation {
	case shellprofile.ID:
		files := manifests.ConfigFiles{}
		if m.Process != nil && m.Process.ConfigFiles != nil {
			files = *m.Process.ConfigFiles
		}
		transform, err := integrationconfig.New(m, programs)
		sourceTransform, sourceErr := scriptpreparation.NewSourceTransform(m.PreparationTransform, programs)
		return shellPreparation{name: name, files: files, route: via, transform: transform, sourceTransform: sourceTransform, failure: errors.Join(err, sourceErr)}
	case pythonprofile.ID:
		transform, err := integrationconfig.New(m, programs)
		sourceTransform, sourceErr := scriptpreparation.NewSourceTransform(m.PreparationTransform, programs)
		return pythonPreparation{name: name, route: via, transform: transform, sourceTransform: sourceTransform, transformed: m.ConfigurationTransform != nil || m.PreparationTransform != nil || len(m.ConfigurationPaths) != 0, failure: errors.Join(err, sourceErr)}
	default:
		transform, err := integrationconfig.New(m, programs)
		sourceTransform, sourceErr := scriptpreparation.NewSourceTransform(m.PreparationTransform, programs)
		return genericPreparation{name: name, route: via, transform: transform, sourceTransform: sourceTransform, transformed: m.ConfigurationTransform != nil || m.PreparationTransform != nil || len(m.ConfigurationPaths) != 0, failure: errors.Join(err, sourceErr)}
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
	return cfg.RuleSet().DiagnosticLevel(cfg, name) != "off" && (enabled == nil || *enabled)
}

// configuredInput masks expressions and passes inline tools.<name>.config
// settings to the engine.
func configuredInput(name string, script tools.Script) (scriptpreparation.Prepared, error) {
	prepared := scriptpreparation.Prepared{Source: mask.Expressions(script.Text)}
	if script.Config == nil {
		return prepared, nil
	}
	if script.Config.Tools[name].Config.Path() != "" {
		return scriptpreparation.Prepared{}, fmt.Errorf("tools.%s.config: %w", name, errConfigFilePath)
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
type genericPreparation struct {
	sourceTransform *scriptpreparation.SourceTransform
	route           route
	name            string
	transform       *integrationconfig.Transform
	transformed     bool
	failure         error
}

func (p genericPreparation) batch(context.Context, tools.Batch) (func(context.Context, tools.Script) (scriptpreparation.Prepared, error), error) {
	return p.input, nil
}

func (p genericPreparation) input(ctx context.Context, script tools.Script) (scriptpreparation.Prepared, error) {
	if p.failure != nil {
		return scriptpreparation.Prepared{}, p.failure
	}
	prepared, err := configuredInput(p.name, script)
	if err != nil {
		return prepared, err
	}
	if !p.transformed {
		return applySourcePreparation(ctx, p.sourceTransform, script, prepared, integrationconfig.Settings{Options: prepared.Config}, p.route)
	}
	settings, err := p.transform.Apply(ctx, prepared.Config, "", PathContext(script.Scope).Expand)
	if err != nil {
		return scriptpreparation.Prepared{}, fmt.Errorf("tools.%s.config: %w", p.name, err)
	}
	if settings.Language != nil || settings.ReadAccess.Enabled != nil || len(settings.ReadAccess.Directories) != 0 {
		return scriptpreparation.Prepared{}, errors.New("language and source access settings require shell preparation")
	}
	prepared.Config = settings.Options
	if p.sourceTransform != nil {
		return applySourcePreparation(ctx, p.sourceTransform, script, prepared, settings, p.route)
	}
	if settings.Prefix != "" {
		mapped := shellprofile.Prepare(prepared.Source, settings.Prefix, "")
		prepared.Source, prepared.LineMap = mapped.Text(), mapped.Line
	}
	return applySourcePreparation(ctx, p.sourceTransform, script, prepared, integrationconfig.Settings{Options: prepared.Config}, p.route)
}

// pythonPreparation binds the Python profile.
type pythonPreparation struct {
	sourceTransform *scriptpreparation.SourceTransform
	name            string
	route           route
	transform       *integrationconfig.Transform
	transformed     bool
	failure         error
}

func (p pythonPreparation) prepareOptions(ctx context.Context, script tools.Script) (json.RawMessage, error) {
	if p.failure != nil {
		return nil, p.failure
	}
	inline, err := inlineSettings(script.Config, p.name)
	if err != nil || !p.transformed {
		return inline, err
	}
	settings, err := p.transform.Apply(ctx, inline, "", PathContext(script.Scope).Expand)
	if err != nil {
		return nil, fmt.Errorf("tools.%s.config: %w", p.name, err)
	}
	if settings.Prefix != "" || settings.Language != nil || settings.ReadAccess.Enabled != nil || len(settings.ReadAccess.Directories) != 0 {
		return nil, errors.New("the Python script profile does not support source_prefix, language or read_access settings")
	}
	return settings.Options, nil
}

func (p pythonPreparation) batch(context.Context, tools.Batch) (func(context.Context, tools.Script) (scriptpreparation.Prepared, error), error) {
	return func(ctx context.Context, script tools.Script) (scriptpreparation.Prepared, error) {
		if script.Input != nil {
			return p.input(ctx, script)
		}
		prepared, err := pythonprofile.Run(script.Text, script.Shell)
		if err != nil {
			return scriptpreparation.Prepared{}, fmt.Errorf("%s could not check Python script at %s: %w", p.name, script.Key, err)
		}
		if prepared.Skip != "" {
			return prepared, nil
		}
		settings, err := p.prepareOptions(ctx, script)
		if err != nil {
			return scriptpreparation.Prepared{}, err
		}
		prepared.Config = settings
		if p.route == routeEngine {
			if prepared.Config, err = pythonprofile.EngineConfig(settings, prepared.Vars); err != nil {
				return scriptpreparation.Prepared{}, fmt.Errorf("tools.%s.config: %w", p.name, err)
			}
		}
		return applySourcePreparation(ctx, p.sourceTransform, script, prepared, integrationconfig.Settings{Options: prepared.Config}, p.route)
	}, nil
}

func (p pythonPreparation) input(ctx context.Context, script tools.Script) (scriptpreparation.Prepared, error) {
	prepared, err := pythonprofile.Script(script.Text)
	if err != nil || prepared.Skip != "" {
		return prepared, err
	}
	settings, err := p.prepareOptions(ctx, script)
	if err != nil {
		return scriptpreparation.Prepared{}, err
	}
	if prepared.Config, err = pythonprofile.EngineConfig(settings, nil); err != nil {
		return scriptpreparation.Prepared{}, fmt.Errorf("tools.%s.config: %w", p.name, err)
	}
	return applySourcePreparation(ctx, p.sourceTransform, script, prepared, integrationconfig.Settings{Options: prepared.Config}, routeEngine)
}

// shellPreparation binds the shell profile: inline settings become
// directives, a configuration path selects the native configuration file,
// and path interpolations resolve against each script's scope.
type shellPreparation struct {
	sourceTransform *scriptpreparation.SourceTransform
	transform       *integrationconfig.Transform
	failure         error
	name            string
	files           manifests.ConfigFiles
	route           route
}

// scopeSettings are a scope's resolved shell settings.
type scopeSettings struct {
	settings   *integrationconfig.Settings
	configFile string
	// failed marks a scope whose settings were reported as an error.
	failed bool
}

func (p shellPreparation) batch(ctx context.Context, batch tools.Batch) (func(context.Context, tools.Script) (scriptpreparation.Prepared, error), error) {
	settings := map[*tools.Scope]scopeSettings{}
	var failure, deferred error
	var deferredScope *tools.Scope
	compositePrepared := false
	for _, scope := range batch.Scopes {
		if scope == nil || !toolEnabled(scope.Config, p.name) {
			continue
		}
		resolved, err := p.scope(ctx, scope)
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
	return func(ctx context.Context, script tools.Script) (scriptpreparation.Prepared, error) {
		if script.Input != nil {
			return p.input(ctx, script)
		}
		resolved, ok := settings[script.Scope]
		if !ok {
			var err error
			if resolved, err = p.scope(ctx, script.Scope); err != nil {
				return scriptpreparation.Prepared{}, err
			}
			settings[script.Scope] = resolved
		}
		if resolved.failed {
			if script.Scope == deferredScope {
				deferredScope = nil
				return scriptpreparation.Prepared{}, deferred
			}
			return scriptpreparation.Prepared{Skip: "its tool settings are invalid"}, nil
		}
		run := shellprofile.Run{
			Shell:    shellprofile.Shell{Name: script.Execution.Shell, Implicit: script.Execution.ImplicitShell, Registry: script.Fact.Interpreters, Platform: shellprofile.TargetArguments(script.Fact.Environment)},
			Settings: resolved.settings, Transform: p.sourceTransform, ConfigFile: resolved.configFile,
			Sources: p.route == routeProcess && script.Execution.DirectoryKnown, Engine: p.route == routeEngine,
			Environment: script.Fact.Environment,
		}
		return shellprofile.PrepareRun(ctx, run, script.Text)
	}, failure
}

// scope resolves the directives and configuration file of a scope.
func (p shellPreparation) scope(ctx context.Context, scope *tools.Scope) (scopeSettings, error) {
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
	if p.failure != nil {
		return scopeSettings{}, p.failure
	}
	file := ""
	if cfg != nil {
		file = cfg.Tools[p.name].Config.Path()
	}
	projected, err := p.transform.Apply(ctx, inline, file, paths.context().Expand)
	if err != nil {
		return scopeSettings{}, fmt.Errorf("tools.%s.config: %w", p.name, err)
	}
	resolved.settings = &projected
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
		if d := resolved.settings; d != nil && (len(d.ReadAccess.Directories) != 0 || d.ReadAccess.Enabled != nil && *d.ReadAccess.Enabled) {
			return scopeSettings{}, fmt.Errorf("tools.%s.config: %w", p.name, shellprofile.ErrNoSourceAccess)
		}
	case p.route == routeEmbedded || path == "":
	default:
		base := paths.context().ConfigDir
		if settings.FromInput() {
			base = paths.analysis
		}
		file, err := paths.context().ResolveFrom(path, base)
		if err == nil {
			var files *inputfiles.Reader
			if scope != nil {
				files = scope.Reader
			}
			file, err = process.ConfigFile(files, p.files, file, "")
		}
		if err != nil {
			return scopeSettings{}, fmt.Errorf("tools.%s.config: %w", p.name, err)
		}
		resolved.configFile = file
	}
	return resolved, nil
}

func (p shellPreparation) input(ctx context.Context, script tools.Script) (scriptpreparation.Prepared, error) {
	dialect := ""
	if script.Input != nil {
		dialect = shellprofile.LanguageDialect(script.Input.Language)
	}
	if script.Config != nil && script.Config.Tools[p.name].Config.Path() != "" {
		return scriptpreparation.Prepared{}, fmt.Errorf("tools.%s.config: %w", p.name, errEngineConfigFile)
	}
	inline, err := inlineSettings(script.Config, p.name)
	if err != nil {
		return scriptpreparation.Prepared{}, err
	}
	if p.failure != nil {
		return scriptpreparation.Prepared{}, p.failure
	}
	settings, err := p.transform.Apply(ctx, inline, "", PathContext(script.Scope).Expand)
	if err != nil {
		return scriptpreparation.Prepared{}, fmt.Errorf("tools.%s.config: %w", p.name, err)
	}
	prepared, err := shellprofile.PrepareRun(ctx, shellprofile.Run{Input: true, Engine: true, Language: dialect, Settings: &settings, Environment: script.Fact.Environment, Transform: p.sourceTransform}, script.Text)
	if err != nil {
		return scriptpreparation.Prepared{}, fmt.Errorf("tools.%s.config: %w", p.name, err)
	}
	return prepared, nil
}

// runPaths are the directories a scope's path interpolations resolve
// against.
type runPaths struct {
	analysis, workspace, actionPath string
	configPath                      string
}

// scopePaths defaults the analysis directory to the working directory and
// the workspace to the analysis directory.
func scopePaths(scope *tools.Scope) runPaths {
	var paths runPaths
	if scope != nil {
		paths = runPaths{analysis: scope.AnalysisDir, workspace: scope.Workspace, actionPath: scope.ActionPath, configPath: scope.ConfigProvenance.Path()}
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

// PathContext uses the scope's configuration source, workspace and runner action path.
func PathContext(scope *tools.Scope) config.PathContext {
	return scopePaths(scope).context()
}

// context is the path interpolation context of a scope's configuration.
func (paths runPaths) context() config.PathContext {
	configDir := paths.workspace
	if paths.configPath != "" && !address.Remote(paths.configPath) {
		configDir = filepath.Dir(paths.configPath)
	}
	return config.PathContext{ConfigDir: configDir, GitDir: paths.workspace, Workspace: shellprofile.RunnerWorkspace(paths.workspace), ActionPath: shellprofile.RunnerActionPath(paths.actionPath)}
}
