package cli

import (
	"context"
	"errors"
	"fmt"

	checking "actionlint.kjanat.dev/v2/internal/checking"
	commandargs "actionlint.kjanat.dev/v2/internal/cli/arguments"
	configcmd "actionlint.kjanat.dev/v2/internal/cli/config"
	pluginexec "actionlint.kjanat.dev/v2/internal/plugins/execution"

	"actionlint.kjanat.dev/v2/internal/checking/scripts/registry"
	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"
)

// commandRequest runs a command using the inputs stored in its request type.
type commandRequest interface {
	run(context.Context, Command) (int, error)
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
		if _, loadFailure := errors.AsType[*pluginexec.LoadError](err); loadFailure && len(selection.Paths) > 0 {
			return 0, commandargs.UsageError{Cause: err}
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
	Config       repoconfig.Selection
	JSON         bool
	Hyperlinks   hyperlinkMode
}

func (r doctorRequest) run(ctx context.Context, streams Command) (int, error) {
	for _, warning := range r.Deprecations {
		_, _ = fmt.Fprintln(streams.Stderr, "warning: "+warning)
	}
	return 0, writeDoctor(ctx, streams.Stdout, r, streams.Acquisition)
}

type configPathRequest configcmd.PathOptions

func (r configPathRequest) run(_ context.Context, streams Command) (int, error) {
	return 0, configcmd.WritePath(streams.Stdout, configcmd.PathOptions(r))
}

type configShowRequest configcmd.ShowOptions

func (r configShowRequest) run(ctx context.Context, streams Command) (int, error) {
	return 0, configcmd.WriteShow(ctx, streams.Stdout, configcmd.ShowOptions(r), streams.Acquisition)
}

type configValidateRequest configcmd.ValidateOptions

func (r configValidateRequest) run(ctx context.Context, streams Command) (int, error) {
	return 0, configcmd.WriteValidation(ctx, streams.Stdout, configcmd.ValidateOptions(r), streams.Acquisition)
}

// configSchemaRequest prints the configuration JSON schema with the tool
// providers that the --plugin flags or the selected configuration install.
type configSchemaRequest configcmd.SchemaOptions

func (r configSchemaRequest) run(ctx context.Context, streams Command) (int, error) {
	return 0, configcmd.WriteSchema(ctx, streams.Stdout, configcmd.SchemaOptions(r), streams.Acquisition)
}

type configInitRequest struct {
	JSON       bool
	Hyperlinks hyperlinkMode
	Format     repoconfig.FileFormat
}

type configMigrateRequest configcmd.MigrateOptions

func (r configMigrateRequest) run(ctx context.Context, streams Command) (int, error) {
	return configcmd.Migrate(ctx, streams.commandStreams(), configcmd.MigrateOptions(r), streams.Acquisition)
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
