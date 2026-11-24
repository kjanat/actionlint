package process

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"actionlint.kjanat.dev/v2/internal/integrations/network"
	"actionlint.kjanat.dev/v2/internal/model/scriptengine"
	"actionlint.kjanat.dev/v2/internal/model/scriptengine/enginediagnostics"

	"actionlint.kjanat.dev/v2/internal/integrations/hostprocess"
	"actionlint.kjanat.dev/v2/internal/model/toolmanifest"
	"actionlint.kjanat.dev/v2/internal/model/toolsettings"
)

const canceledDecodeLimit = 5 * time.Second

type Options struct {
	Programs func(digest string) ([]byte, bool)
	// Required makes a failed version probe an error. Without Required, a provider
	// whose version cannot be established is skipped with one warning.
	Required bool
	// Probe holds the version probe result shared with other backends of
	// the same command. Nil gives the backend a probe of its own.
	Probe *VersionProbe
	Warn  func(error)
	Debug func(string, ...any)
	// Unlike Warn, Skipped runs for every consumer of a cached failed probe.
	Skipped func()
}

type Backend struct {
	name           string
	spec           toolmanifest.ProcessSpec
	capabilities   toolmanifest.Capabilities
	command        *hostprocess.Command
	options        Options
	decoder        Decoder
	configProgram  *processConfigProgram
	schemas        []*toolsettings.Schema
	version        *regexp.Regexp
	env            []string
	launcher, user []string
	commandProblem string
	probe          *VersionProbe
	templates      sync.Once
}

func NewBackend(manifest toolmanifest.Manifest, command *hostprocess.Command, options Options) (*Backend, error) {
	if manifest.Process == nil {
		return nil, fmt.Errorf("tool provider %s has no process transport", manifest.Rule.Name)
	}
	if command == nil {
		return nil, fmt.Errorf("tool provider %s has no command", manifest.Rule.Name)
	}
	spec := *manifest.Process
	if err := spec.ValidateProgram(); err != nil {
		return nil, err
	}
	if spec.Program != nil {
		program := *spec.Program
		spec.Program = &program
	}
	b := &Backend{name: manifest.Rule.Name, spec: spec, capabilities: manifest.Capabilities, command: command, options: options, probe: options.Probe}
	if b.probe == nil {
		b.probe = &VersionProbe{}
	}
	var err error
	if b.decoder, err = newDecoder(spec.Output, options.Programs); err != nil {
		return nil, err
	}
	if spec.Config.Transform != nil {
		source, err := toolmanifest.ResolveTransform(spec.Config.Transform, options.Programs)
		if err != nil {
			return nil, err
		}
		compiled, err := compileJQ(source)
		if err != nil {
			return nil, fmt.Errorf("configuration %w", err)
		}
		b.configProgram = &processConfigProgram{program: compiled}
	}
	schemas := []json.RawMessage{spec.ConfigSchema}
	if manifest.ConfigurationTransform == nil && manifest.PreparationTransform == nil && len(manifest.ConfigurationPaths) == 0 {
		schemas = append(schemas, manifest.ConfigSchema)
	}
	for _, document := range schemas {
		if len(document) == 0 {
			continue
		}
		schema, err := toolsettings.Compile(document)
		if err != nil {
			return nil, fmt.Errorf("tool provider %s configuration schema: %w", b.name, err)
		}
		b.schemas = append(b.schemas, schema)
	}
	if spec.Version != nil {
		if b.version, err = regexp.Compile(spec.Version.Pattern); err != nil {
			return nil, fmt.Errorf("tool provider %s version pattern: %w", b.name, err)
		}
	}
	var launchers []launcher
	if spec.UserArgs != nil {
		if launchers, err = compileLaunchers(spec.UserArgs.Launchers); err != nil {
			return nil, fmt.Errorf("tool provider %s: %w", b.name, err)
		}
	}
	prefix, user, err := splitLauncher(launchers, command.Exe, command.Args)
	switch {
	case err != nil:
		b.commandProblem = fmt.Sprintf("tools.%s: %v", b.name, err)
	case len(user) > 0 && !slices.Contains(spec.Args, "{user-args}"):
		b.commandProblem = fmt.Sprintf("tools.%s: %v", b.name, errNoUserArgs)
	default:
		// Reject forbidden user arguments before the version probe runs anything.
		if err := checkUserArgs(b.name, spec.UserArgs, user, command.Dir); err != nil {
			b.commandProblem = err.Error()
		}
	}
	b.launcher, b.user = slices.Concat(command.LauncherArgs, prefix), user
	for _, name := range slices.Sorted(maps.Keys(spec.Env)) {
		b.env = append(b.env, name+"="+spec.Env[name])
	}
	for _, name := range spec.EnvPassthrough {
		if value, ok := os.LookupEnv(name); ok {
			b.env = append(b.env, name+"="+value)
		}
	}
	b.env = append(b.env, command.Env...)
	return b, nil
}

func newDecoder(output toolmanifest.Output, programs func(string) ([]byte, bool)) (Decoder, error) {
	switch output.Format {
	case toolmanifest.FormatEnvelope:
		return Envelope{}, nil
	case toolmanifest.FormatSARIF:
		return SARIF{}, nil
	case toolmanifest.FormatJSON, toolmanifest.FormatText:
		if output.Transform == nil {
			return nil, fmt.Errorf("output format %q needs a transform", output.Format)
		}
		source, err := toolmanifest.ResolveTransform(output.Transform, programs)
		if err != nil {
			return nil, err
		}
		input := JQJSON
		if output.Format == toolmanifest.FormatText {
			input = JQText
		}
		return NewJQ(source, input)
	default:
		return nil, fmt.Errorf("unknown output format %q", output.Format)
	}
}

func (b *Backend) Concurrent() bool {
	return b.spec.Concurrency != toolmanifest.ConcurrencySerial
}

func (b *Backend) Check(ctx context.Context, request scriptengine.Request) ([]scriptengine.Diagnostic, error) {
	result, err := b.Run(ctx, request, nil)
	return result.Diagnostics, err
}

func (b *Backend) ValidateConfig(_ context.Context, config json.RawMessage) error {
	if len(config) == 0 {
		return nil
	}
	for _, schema := range b.schemas {
		if err := schema.Validate(config); err != nil {
			return fmt.Errorf("tools.%s.config: %w", b.name, err)
		}
	}
	return nil
}

func (b *Backend) ValidateTemplates(ctx context.Context) (bool, error) {
	if available, err := b.available(ctx); err != nil || !available {
		return false, detachError(ctx, err)
	}
	if b.spec.Version == nil || b.spec.Version.TemplateEffects == "" {
		return true, nil
	}
	version, err := b.probe.run(ctx, func() (string, error) { return b.identify(ctx) }, nil)
	if err != nil {
		return false, detachError(ctx, err)
	}
	if version == b.spec.Version.TemplateEffects {
		return true, nil
	}
	mismatch := fmt.Errorf("%s template effects were reviewed for version %s; found %s", b.name, b.spec.Version.TemplateEffects, version)
	if b.options.Required {
		return false, mismatch
	}
	b.templates.Do(func() { b.warn(mismatch) })
	return false, nil
}

func (b *Backend) available(ctx context.Context) (bool, error) {
	settings := network.Settings(ctx)
	if settings.WarmCache {
		if b.options.Skipped != nil {
			b.options.Skipped()
		}
		return false, nil
	}
	if b.version == nil {
		return true, nil
	}
	if b.commandProblem != "" {
		return false, errors.New(b.commandProblem)
	}
	_, err := b.probe.run(ctx, func() (string, error) { return b.identify(ctx) }, func(err error) {
		if !b.options.Required {
			b.warn(fmt.Errorf("skipping automatically discovered %s: %w", b.name, err))
		}
	})
	switch {
	case err == nil:
		return true, nil
	case canceled(err) || b.options.Required:
		return false, err
	default:
		if b.options.Skipped != nil {
			b.options.Skipped()
		}
		return false, nil
	}
}

func (b *Backend) ProbeVersion(ctx context.Context) (string, error) {
	if b.version == nil {
		return "", nil
	}
	if b.commandProblem != "" {
		return "", errors.New(b.commandProblem)
	}
	version, err := b.identify(ctx)
	return version, detachError(ctx, err)
}

func (b *Backend) identify(ctx context.Context) (string, error) {
	execution := b.execution(slices.Clone(b.spec.Version.Args), "", b.command.Dir)
	execution.ExitCodes = []int{0}
	output, _, err := b.command.Invoke(ctx, execution)
	if err != nil {
		return "", err
	}
	return identifyVersion(b.name, b.version, b.spec.Version.Minimum, string(output))
}

func (b *Backend) warn(err error) {
	if b.options.Warn != nil {
		b.options.Warn(err)
	}
}

func (b *Backend) debug(format string, args ...any) {
	if b.options.Debug != nil {
		b.options.Debug(format, args...)
	}
}

func (b *Backend) execution(args []string, stdin, directory string) hostprocess.Execution {
	exitCodes := slices.Clone(b.spec.ExitCodes)
	if len(exitCodes) == 0 {
		exitCodes = []int{0}
	}
	return hostprocess.Execution{
		OutputLimit: MaxOutputBytes,
		Args:        append(slices.Clone(b.launcher), args...), Stdin: stdin, Dir: directory,
		Env: slices.Clone(b.env), UnsetEnv: append(slices.Clone(b.command.UnsetEnv), b.spec.EnvUnset...), ExitCodes: exitCodes,
	}
}

func (b *Backend) Run(ctx context.Context, request scriptengine.Request, vars map[string]string) (DecodedCheck, error) {
	result, err := b.run(ctx, request, vars, nil)
	return result, detachError(ctx, err)
}

func detachError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if cause := ctx.Err(); cause != nil {
		return cause
	}
	return errors.New(err.Error())
}

func (b *Backend) run(ctx context.Context, request scriptengine.Request, vars map[string]string, document *DocumentRequest) (result DecodedCheck, err error) {
	if err := ctx.Err(); err != nil {
		return DecodedCheck{}, err
	}
	if b.commandProblem != "" {
		return DecodedCheck{}, errors.New(b.commandProblem)
	}
	if available, err := b.available(ctx); err != nil || !available {
		return DecodedCheck{Skipped: true}, err
	}
	directory, err := b.directory(request.Directory)
	if err != nil {
		return DecodedCheck{}, err
	}
	if err := checkUserArgs(b.name, b.spec.UserArgs, b.user, directory); err != nil {
		return DecodedCheck{}, err
	}
	values := maps.Clone(vars)
	if values == nil {
		values = map[string]string{}
	}
	values["filename"] = request.Filename
	values["filename_abs"] = filepath.Join(directory, request.Filename)
	if filepath.IsAbs(request.Filename) {
		values["filename_abs"] = request.Filename
	}
	config, err := decodeConfig(request.Config)
	if err != nil {
		return DecodedCheck{}, fmt.Errorf("tools.%s.config: %w", b.name, err)
	}
	engine := engineConfig{value: config}
	if b.configProgram != nil {
		if engine, err = b.configProgram.apply(ctx, config, values); err != nil {
			return DecodedCheck{}, fmt.Errorf("tools.%s.config: %w", b.name, err)
		}
	}
	for name, value := range engine.vars {
		if value == nil {
			delete(values, name)
		} else {
			values[name] = *value
		}
	}
	envArgs, inherited := environmentOptions(b.spec.UserArgs, b.spec.EnvPassthrough, b.command.Env)
	for _, name := range slices.Sorted(maps.Keys(inherited)) {
		b.debug("%s: Inherited %s=%q", b.name, name, inherited[name])
	}
	for _, name := range overriddenVars(b.spec.UserArgs, slices.Concat(envArgs, b.user)) {
		delete(values, name)
	}
	if err := checkForbidden(b.name, engine.value, b.spec.Config.Forbidden); err != nil {
		return DecodedCheck{}, err
	}
	in := argInputs{vars: values, user: b.user}
	if b.spec.Program != nil {
		path, cleanup, programErr := materializeProgram(ctx, *b.spec.Program)
		if programErr != nil {
			return DecodedCheck{}, programErr
		}
		defer func() { err = errors.Join(err, cleanup()) }()
		in.programFile = path
	}
	switch b.spec.Config.Render {
	case toolmanifest.RenderTOMLPairs:
		if in.tomlPairs, err = tomlPairs(pairsOption(b.spec.Args), engine.value); err != nil {
			return DecodedCheck{}, fmt.Errorf("tools.%s.%w", b.name, err)
		}
	case toolmanifest.RenderTOMLFile, toolmanifest.RenderJSONFile:
		path, remove, err := writeConfigFile(b.spec.Config.Render, engine.value)
		if err != nil {
			return DecodedCheck{}, fmt.Errorf("tools.%s.config: %w", b.name, err)
		}
		defer remove()
		in.configFile = path
	case toolmanifest.RenderNone, toolmanifest.RenderJSONStdin:
	}
	args, err := renderArgs(b.spec.Args, in)
	if err != nil {
		return DecodedCheck{}, err
	}
	var stdin string
	if document != nil && b.spec.Stdin == toolmanifest.StdinRequest {
		payload := *document
		if b.spec.Config.Render == toolmanifest.RenderJSONStdin && engine.value != nil {
			payload.Config, err = json.Marshal(engine.value)
		}
		if err == nil {
			var encoded []byte
			encoded, err = json.Marshal(payload)
			stdin = string(encoded)
		}
	} else {
		stdin, err = b.stdin(request, engine.value)
	}
	if err != nil {
		return DecodedCheck{}, err
	}
	execution := b.execution(args, stdin, directory)
	b.debug("%s: Running %q with arguments %q in %q", b.name, b.command.Exe, execution.Args, directory)
	stdout, code, err := b.command.Invoke(ctx, execution)
	if err != nil {
		return DecodedCheck{}, err
	}
	values["exit_code"] = strconv.Itoa(code)
	decoding := ctx
	if ctx.Err() != nil {
		var cancel context.CancelFunc
		decoding, cancel = context.WithTimeout(context.WithoutCancel(ctx), canceledDecodeLimit)
		defer cancel()
	}
	result, err = b.decoder.Decode(decoding, stdout, values)
	if err != nil {
		return DecodedCheck{}, fmt.Errorf("tools.%s: %w", b.name, err)
	}
	result.Directory = directory
	if document != nil {
		result.Diagnostics = documentFindings(result.Diagnostics, request, directory)
	}
	if err := b.review(&result, request); err != nil {
		return DecodedCheck{}, err
	}
	var failures error
	for _, message := range result.Errors {
		failures = errors.Join(failures, errors.New(message))
	}
	return detached(result), failures
}

func detached(result DecodedCheck) DecodedCheck {
	diagnostics := make([]scriptengine.Diagnostic, len(result.Diagnostics))
	for i, d := range result.Diagnostics {
		fixes := make([]scriptengine.Fix, len(d.Fixes))
		for j, fix := range d.Fixes {
			fixes[j] = scriptengine.Fix{Applicability: fix.Applicability, Description: fix.Description, Replacements: slices.Clone(fix.Replacements)}
		}
		diagnostics[i] = scriptengine.Diagnostic{
			Filename: d.Filename, Code: d.Code, Name: d.Name, Message: d.Message, Severity: d.Severity,
			Location: d.Location, EndLocation: d.EndLocation, URL: d.URL, TemplateEffect: d.TemplateEffect,
			Source: d.Source, Fixes: fixes,
		}
	}
	return DecodedCheck{Diagnostics: diagnostics, Sources: slices.Clone(result.Sources), Errors: slices.Clone(result.Errors), Directory: result.Directory}
}

func (b *Backend) directory(requested string) (string, error) {
	directory := requested
	if directory == "" {
		directory = b.command.Dir
	}
	if b.command.Runner != nil {
		if directory == "" {
			directory = "/"
		}
		return directory, nil
	}
	if directory == "" {
		directory = "."
	}
	directory, err := filepath.Abs(directory)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(directory)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("tools.%s: working directory is not a directory: %q", b.name, directory)
	}
	// Tools can report paths through getcwd, which resolves directory symlinks.
	return filepath.EvalSymlinks(directory)
}

func (b *Backend) stdin(request scriptengine.Request, config any) (string, error) {
	if b.spec.Stdin != toolmanifest.StdinRequest {
		return request.Source, nil
	}
	payload := scriptengine.Request{Source: request.Source, Filename: request.Filename, Environment: request.Environment}
	if b.spec.Config.Render == toolmanifest.RenderJSONStdin && config != nil {
		encoded, err := json.Marshal(config)
		if err != nil {
			return "", err
		}
		payload.Config = encoded
	}
	encoded, err := json.Marshal(payload)
	return string(encoded), err
}

func pairsOption(args []string) string {
	for _, arg := range args {
		if option, ok := strings.CutPrefix(arg, "{config:toml-pairs:"); ok {
			return strings.TrimSuffix(option, "}")
		}
	}
	return ""
}

func (b *Backend) review(result *DecodedCheck, request scriptengine.Request) error {
	convention := enginediagnostics.ContractCoordinates
	if b.spec.Output.Coordinates != nil {
		convention = *b.spec.Output.Coordinates
	}
	snippet := enginediagnostics.NewSnippet(request.Source, convention)
	for i := range result.Diagnostics {
		d := &result.Diagnostics[i]
		if d.Code == "" || d.Message == "" {
			return fmt.Errorf("tools.%s: invalid diagnostic %+v", b.name, *d)
		}
		if d.Source != "" && !b.capabilities.ReportedSources {
			return fmt.Errorf("tools.%s: tool reported a finding in %q but does not declare reported sources", b.name, d.Source)
		}
		if d.Source == "" {
			if d.Filename == "" {
				d.Filename = request.Filename
			}
			if d.Filename != request.Filename {
				return fmt.Errorf("tools.%s: tool output refers to unexpected file %q", b.name, d.Filename)
			}
		}
		if err := snippet.Normalize(d); err != nil {
			return fmt.Errorf("tools.%s: invalid diagnostic %+v: %w", b.name, *d, err)
		}
		if d.Source != "" {
			continue
		}
		if b.capabilities.TemplateContract == scriptengine.TemplateEffectsContract && d.TemplateEffect == "" {
			d.TemplateEffect = scriptengine.TemplateEffectNone
			if effect, ok := b.spec.TemplateEffects[d.Code]; ok {
				d.TemplateEffect = effect
			}
		}
	}
	for _, source := range result.Sources {
		if source == "" || strings.ContainsRune(source, 0) {
			return fmt.Errorf("tools.%s: invalid reported source %q", b.name, source)
		}
	}
	return nil
}
