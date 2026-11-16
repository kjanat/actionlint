package cli

import (
	"context"
	"errors"
	"fmt"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/cli/arguments"
	"actionlint.kjanat.dev/v2/internal/cli/commandio"
	"actionlint.kjanat.dev/v2/internal/cli/configcommand"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginacquisition"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginexecution"

	"actionlint.kjanat.dev/v2/internal/checking/scriptanalysis/registry"
	"actionlint.kjanat.dev/v2/internal/input/configuration"
)

// commandRequest runs a command using the inputs stored in its request type.
type commandRequest interface {
	run(context.Context, Command) (int, error)
}

// streamCommand is a command whose package runs it over command streams.
type streamCommand interface {
	Run(context.Context, commandio.Streams, pluginacquisition.Options) (int, error)
}

// streamRequest runs a streamCommand with the streams and plugin acquisition of a Command.
type streamRequest[C streamCommand] struct{ command C }

func newStreamRequest[C streamCommand](command C) commandRequest { return streamRequest[C]{command} }

func (r streamRequest[C]) run(ctx context.Context, streams Command) (int, error) {
	return r.command.Run(ctx, streams.commandStreams(), streams.Acquisition)
}

type checkRequest struct {
	Check        checkInvocation
	Render       renderOptions
	Yes          bool
	JSON, Legacy bool
}

func (r checkRequest) run(ctx context.Context, streams Command) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return executeCheck(ctx, streams, r)
}

type versionRequest struct{ JSON, Legacy bool }

func (r versionRequest) run(_ context.Context, streams Command) (int, error) {
	return 0, writeVersion(streams.Stdout, r.JSON, r.Legacy)
}

type rulesRequest struct {
	Check checkInvocation
	Name  string
	JSON  bool
}

func (r rulesRequest) run(ctx context.Context, streams Command) (int, error) {
	selection := r.Check.pluginSelection(streams.Acquisition)
	sources, err := selectedRulePlugins(ctx, selection)
	if err != nil {
		return 0, err
	}
	rules, err := checking.RulesWithPlugins(ctx, sources)
	if err != nil {
		if _, loadFailure := errors.AsType[*pluginexecution.LoadError](err); loadFailure && len(selection.Paths) > 0 {
			return 0, arguments.UsageError{Cause: err}
		}
		return 0, err
	}
	return 0, writeRules(streams.Stdout, r.Name, r.JSON, rules)
}

type doctorRequest struct {
	Plugins           []string
	ResolverCommand   string
	ResolverArguments []string
	// Tools selects every bundled tool, as checks would run it.
	Tools registry.Selections
	// NamedTools lists the tool ids --tool and --no-tool named.
	NamedTools []string
	// Deprecations holds a warning for each deprecated flag used; doctor
	// prints them to stderr.
	Deprecations []string
	Config       configuration.Selection
	JSON         bool
	Hyperlinks   hyperlinkMode
}

func (r doctorRequest) run(ctx context.Context, streams Command) (int, error) {
	for _, warning := range r.Deprecations {
		_, _ = fmt.Fprintln(streams.Stderr, "warning: "+warning)
	}
	return 0, writeDoctor(ctx, streams.Stdout, r, streams.Acquisition)
}

type configPathRequest configcommand.PathOptions

func (r configPathRequest) run(_ context.Context, streams Command) (int, error) {
	return 0, configcommand.WritePath(streams.Stdout, configcommand.PathOptions(r))
}

type configShowRequest configcommand.ShowOptions

func (r configShowRequest) run(ctx context.Context, streams Command) (int, error) {
	return 0, configcommand.WriteShow(ctx, streams.Stdout, configcommand.ShowOptions(r), streams.Acquisition)
}

type configValidateRequest configcommand.ValidateOptions

func (r configValidateRequest) run(ctx context.Context, streams Command) (int, error) {
	return 0, configcommand.WriteValidation(ctx, streams.Stdout, configcommand.ValidateOptions(r), streams.Acquisition)
}

// configSchemaRequest prints the configuration JSON schema with the tool
// providers that the --plugin flags or the selected configuration install.
type configSchemaRequest configcommand.SchemaOptions

func (r configSchemaRequest) run(ctx context.Context, streams Command) (int, error) {
	return 0, configcommand.WriteSchema(ctx, streams.Stdout, configcommand.SchemaOptions(r), streams.Acquisition)
}

type configInitRequest struct {
	JSON       bool
	Hyperlinks hyperlinkMode
	Format     configuration.FileFormat
}

func (r configInitRequest) run(ctx context.Context, streams Command) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return 0, generateCommandConfig(ctx, streams.Stdout, r.JSON, r.Hyperlinks, r.Format, streams.Acquisition)
}

type legacyConfigInitRequest struct {
	ConfigPath             string
	IgnoreRegex            []string
	Template, TemplateFile string
	Log, JSON              bool
	Hyperlinks             hyperlinkMode
}

func (r legacyConfigInitRequest) run(ctx context.Context, streams Command) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return 0, initLegacyCommandConfig(ctx, streams, r)
}

func executeInvocation(ctx context.Context, streams Command, inv invocation) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	request, err := inv.request()
	if err != nil {
		return 0, err
	}
	if inv.JSON && inv.Render.PrettyJSON && inv.Operation != operationCheck {
		streams.Stdout = &prettyJSONOutput{Writer: streams.Stdout, ctx: ctx, color: inv.Render.Color, explicit: inv.Render.PrettyJSONExplicit}
	}
	return request.run(ctx, streams)
}
