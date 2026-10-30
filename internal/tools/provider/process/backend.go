package process

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/internal/analyzerhost"
	"actionlint.kjanat.dev/v2/internal/tools/provider"
	"actionlint.kjanat.dev/v2/internal/tools/provider/configschema"
)

// Options configure a Backend beyond its manifest.
type Options struct {
	// Programs resolves transform digests to program text. Inline programs
	// need none.
	Programs func(digest string) ([]byte, bool)
	// Required makes a failed version probe an error. Without it a provider
	// whose version cannot be established is skipped with one warning.
	Required bool
	Warn     func(error)
	Debug    func(string, ...any)
}

// Backend runs one native process per check as a process manifest describes:
// it substitutes argument placeholders, renders configuration, runs the
// command with a scrubbed environment and decodes stdout into snippet
// diagnostics in contract coordinates. It implements analyzer.ScriptBackend,
// analyzer.ScriptConfigValidator and analyzer.ScriptTemplateValidator.
type Backend struct {
	name         string
	spec         provider.ProcessSpec
	capabilities provider.Capabilities
	command      *analyzerhost.Command
	options      Options
	decoder      Decoder
	transform    *configTransform
	schemas      []*configschema.Schema
	version      *regexp.Regexp
	env          []string
	// launcher holds the command's launcher arguments and the launcher
	// options that start the tool; user holds the tool's own user arguments.
	launcher, user []string
	// commandProblem rejects every check of a command the manifest cannot
	// run; each check reports it as its own error.
	commandProblem string
	probe          probe
	templates      sync.Once
}

// NewBackend binds a process manifest to a resolved command. The command's
// arguments are user arguments: launcher options first, when the
// executable is a launcher the manifest names.
func NewBackend(manifest provider.Manifest, command *analyzerhost.Command, options Options) (*Backend, error) {
	if manifest.Process == nil {
		return nil, fmt.Errorf("tool provider %s has no process transport", manifest.Rule.Name)
	}
	if command == nil {
		return nil, fmt.Errorf("tool provider %s has no command", manifest.Rule.Name)
	}
	spec := *manifest.Process
	b := &Backend{name: manifest.Rule.Name, spec: spec, capabilities: manifest.Capabilities, command: command, options: options}
	var err error
	if b.decoder, err = newDecoder(spec.Output, options.Programs); err != nil {
		return nil, err
	}
	if spec.Config.Transform != nil {
		source, err := program(spec.Config.Transform, options.Programs)
		if err != nil {
			return nil, err
		}
		compiled, err := compileJQ(source)
		if err != nil {
			return nil, fmt.Errorf("configuration %w", err)
		}
		b.transform = &configTransform{program: compiled}
	}
	for _, document := range []json.RawMessage{manifest.ConfigSchema, spec.ConfigSchema} {
		if len(document) == 0 {
			continue
		}
		schema, err := configschema.Compile(document)
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

// newDecoder selects the decoder of an output format.
func newDecoder(output provider.Output, programs func(string) ([]byte, bool)) (Decoder, error) {
	switch output.Format {
	case provider.FormatEnvelope:
		return Envelope{}, nil
	case provider.FormatSARIF:
		return SARIF{}, nil
	case provider.FormatJSON, provider.FormatText:
		if output.Transform == nil {
			return nil, fmt.Errorf("output format %q needs a transform", output.Format)
		}
		source, err := program(output.Transform, programs)
		if err != nil {
			return nil, err
		}
		input := JQJSON
		if output.Format == provider.FormatText {
			input = JQText
		}
		return NewJQ(source, input)
	default:
		return nil, fmt.Errorf("unknown output format %q", output.Format)
	}
}

// program returns a transform's text, verifying a digest-named program
// against its digest.
func program(transform *provider.Transform, programs func(string) ([]byte, bool)) (string, error) {
	if transform.Language != provider.LanguageJQ {
		return "", fmt.Errorf("unsupported transform language %q", transform.Language)
	}
	if transform.Inline != "" {
		return transform.Inline, nil
	}
	if programs == nil {
		return "", fmt.Errorf("transform program %s is not available", transform.Digest)
	}
	source, ok := programs(transform.Digest)
	if !ok {
		return "", fmt.Errorf("transform program %s is not available", transform.Digest)
	}
	sum := sha256.Sum256(source)
	if "sha256:"+hex.EncodeToString(sum[:]) != transform.Digest {
		return "", fmt.Errorf("transform program does not match digest %s", transform.Digest)
	}
	return string(source), nil
}

// Concurrent reports whether checks may run at once, each holding one slot
// of the shared process pool.
func (b *Backend) Concurrent() bool {
	return b.spec.Concurrency != provider.ConcurrencySerial
}

// Check implements analyzer.ScriptBackend.
func (b *Backend) Check(ctx context.Context, request analyzer.ScriptRequest) ([]analyzer.ScriptDiagnostic, error) {
	result, err := b.Run(ctx, request, nil)
	return result.Diagnostics, err
}

// ValidateConfig implements analyzer.ScriptConfigValidator with the
// provider's configuration schema and the process transport's own.
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

// ValidateTemplates implements analyzer.ScriptTemplateValidator. Template
// effects are trusted only from the exact engine version the manifest's
// table was reviewed against.
func (b *Backend) ValidateTemplates(ctx context.Context) (bool, error) {
	if available, err := b.available(ctx); err != nil || !available {
		return false, owned(ctx, err)
	}
	if b.spec.Version == nil || b.spec.Version.TemplateEffects == "" {
		return true, nil
	}
	// The probe completed in available; run returns its shared result.
	version, err := b.probe.run(ctx, func() (string, error) { return b.identify(ctx) }, nil)
	if err != nil {
		return false, owned(ctx, err)
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

// available probes the engine version once. A failed probe is an error for
// a required provider and a skipped check, after one warning, otherwise.
func (b *Backend) available(ctx context.Context) (bool, error) {
	if b.version == nil {
		return true, nil
	}
	if b.commandProblem != "" {
		return false, errors.New(b.commandProblem)
	}
	_, err := b.probe.run(ctx, func() (string, error) { return b.identify(ctx) }, func(err error) {
		if !b.options.Required {
			b.warn(err)
		}
	})
	switch {
	case err == nil:
		return true, nil
	case canceled(err) || b.options.Required:
		return false, err
	default:
		return false, nil
	}
}

// identify runs the version probe.
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

// execution describes one run. It shares no storage with the backend, so
// nothing a run returns can alias the backend's configuration.
func (b *Backend) execution(args []string, stdin, directory string) analyzerhost.Execution {
	exitCodes := slices.Clone(b.spec.ExitCodes)
	if len(exitCodes) == 0 {
		exitCodes = []int{0}
	}
	return analyzerhost.Execution{
		Args: append(slices.Clone(b.launcher), args...), Stdin: stdin, Dir: directory,
		Env: slices.Clone(b.env), UnsetEnv: append(slices.Clone(b.command.UnsetEnv), b.spec.EnvUnset...), ExitCodes: exitCodes,
	}
}

// Run checks one request and returns the decoded result, including the
// files the engine read. Vars are preparation variables for argument
// placeholders and transforms; "filename", "filename_abs" and "exit_code"
// are the host's own. An optional provider whose version cannot be
// established returns no result and no error.
func (b *Backend) Run(ctx context.Context, request analyzer.ScriptRequest, vars map[string]string) (Result, error) {
	result, err := b.run(ctx, request, vars)
	return result, owned(ctx, err)
}

// owned reports err without retaining backend or engine state: a
// cancellation as ctx's error and any other failure by its message.
func owned(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if cause := ctx.Err(); cause != nil {
		return cause
	}
	return errors.New(err.Error())
}

func (b *Backend) run(ctx context.Context, request analyzer.ScriptRequest, vars map[string]string) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if b.commandProblem != "" {
		return Result{}, errors.New(b.commandProblem)
	}
	if available, err := b.available(ctx); err != nil || !available {
		return Result{}, err
	}
	directory, err := b.directory(request.Directory)
	if err != nil {
		return Result{}, err
	}
	if err := checkUserArgs(b.name, b.spec.UserArgs, b.user, directory); err != nil {
		return Result{}, err
	}
	values := maps.Clone(vars)
	if values == nil {
		values = map[string]string{}
	}
	values["filename"] = request.Filename
	values["filename_abs"] = filepath.Join(directory, request.Filename)
	config, err := decodeConfig(request.Config)
	if err != nil {
		return Result{}, fmt.Errorf("tools.%s.config: %w", b.name, err)
	}
	engine := engineConfig{value: config}
	if b.transform != nil {
		if engine, err = b.transform.apply(ctx, config, values); err != nil {
			return Result{}, fmt.Errorf("tools.%s.config: %w", b.name, err)
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
		return Result{}, err
	}
	in := argInputs{vars: values, user: b.user}
	switch b.spec.Config.Render {
	case provider.RenderTOMLPairs:
		if in.tomlPairs, err = tomlPairs(pairsOption(b.spec.Args), engine.value); err != nil {
			return Result{}, fmt.Errorf("tools.%s.%w", b.name, err)
		}
	case provider.RenderTOMLFile, provider.RenderJSONFile:
		path, remove, err := writeConfigFile(b.spec.Config.Render, engine.value)
		if err != nil {
			return Result{}, fmt.Errorf("tools.%s.config: %w", b.name, err)
		}
		defer remove()
		in.configFile = path
	case provider.RenderNone, provider.RenderJSONStdin:
	}
	args, err := renderArgs(b.spec.Args, in)
	if err != nil {
		return Result{}, err
	}
	stdin, err := b.stdin(request, engine.value)
	if err != nil {
		return Result{}, err
	}
	execution := b.execution(args, stdin, directory)
	b.debug("%s: Running %q with arguments %q in %q", b.name, b.command.Exe, execution.Args, directory)
	stdout, code, err := b.command.Invoke(ctx, execution)
	if err != nil {
		return Result{}, err
	}
	values["exit_code"] = strconv.Itoa(code)
	result, err := b.decoder.Decode(ctx, stdout, values)
	if err != nil {
		return Result{}, fmt.Errorf("tools.%s: %w", b.name, err)
	}
	result.Directory = directory
	if err := b.review(&result, request); err != nil {
		return Result{}, err
	}
	var failures error
	for _, message := range result.Errors {
		failures = errors.Join(failures, errors.New(message))
	}
	return detached(result), failures
}

// detached copies a decoded result into storage no decoder retains.
func detached(result Result) Result {
	diagnostics := make([]analyzer.ScriptDiagnostic, len(result.Diagnostics))
	for i, d := range result.Diagnostics {
		fixes := make([]analyzer.ScriptFix, len(d.Fixes))
		for j, fix := range d.Fixes {
			fixes[j] = analyzer.ScriptFix{Description: fix.Description, Replacements: slices.Clone(fix.Replacements)}
		}
		diagnostics[i] = analyzer.ScriptDiagnostic{
			Filename: d.Filename, Code: d.Code, Name: d.Name, Message: d.Message, Severity: d.Severity,
			Location: d.Location, EndLocation: d.EndLocation, URL: d.URL, TemplateEffect: d.TemplateEffect,
			Source: d.Source, Fixes: fixes,
		}
	}
	return Result{Diagnostics: diagnostics, Sources: slices.Clone(result.Sources), Errors: slices.Clone(result.Errors), Directory: result.Directory}
}

// directory returns the absolute working directory of a check.
func (b *Backend) directory(requested string) (string, error) {
	directory := requested
	if directory == "" {
		directory = b.command.Dir
	}
	if b.command.Runner != nil {
		// An embedded runner may have no filesystem to check the directory
		// in, such as in a browser.
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
	return directory, nil
}

// stdin is the snippet, or the JSON request carrying rendered configuration
// when the configuration travels on stdin.
func (b *Backend) stdin(request analyzer.ScriptRequest, config any) (string, error) {
	if b.spec.Stdin != provider.StdinRequest {
		return request.Source, nil
	}
	payload := analyzer.ScriptRequest{Source: request.Source, Filename: request.Filename}
	if b.spec.Config.Render == provider.RenderJSONStdin && config != nil {
		encoded, err := json.Marshal(config)
		if err != nil {
			return "", err
		}
		payload.Config = encoded
	}
	encoded, err := json.Marshal(payload)
	return string(encoded), err
}

// pairsOption is the option of the {config:toml-pairs:OPTION} placeholder.
func pairsOption(args []string) string {
	for _, arg := range args {
		if option, ok := strings.CutPrefix(arg, "{config:toml-pairs:"); ok {
			return strings.TrimSuffix(option, "}")
		}
	}
	return ""
}

// review admits decoded diagnostics. A snippet diagnostic must name the
// request's snippet, a reported one must come from a provider that declares
// reported sources; snippet positions are converted to contract
// coordinates and template effects come from the reviewed table.
func (b *Backend) review(result *Result, request analyzer.ScriptRequest) error {
	coordinates := newCoordinates(request.Source, b.spec.Output.Coordinates)
	for i := range result.Diagnostics {
		d := &result.Diagnostics[i]
		if d.Code == "" || d.Message == "" || d.Location.Row < 1 || d.Location.Column < 1 {
			return fmt.Errorf("tools.%s: invalid diagnostic %+v", b.name, *d)
		}
		if d.Source != "" {
			if !b.capabilities.ReportedSources {
				return fmt.Errorf("tools.%s: tool reported a finding in %q but does not declare reported sources", b.name, d.Source)
			}
			continue
		}
		if d.Filename == "" {
			d.Filename = request.Filename
		}
		if d.Filename != request.Filename {
			return fmt.Errorf("tools.%s: tool output refers to unexpected file %q", b.name, d.Filename)
		}
		coordinates.diagnostic(d)
		if b.capabilities.TemplateContract == analyzer.TemplateEffectsContract && d.TemplateEffect == "" {
			d.TemplateEffect = analyzer.TemplateEffectNone
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
