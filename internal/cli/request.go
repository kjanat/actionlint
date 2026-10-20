package cli

import (
	"context"

	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/localapp"
	"actionlint.kjanat.dev/v2/internal/processconfig"
)

// commandRequest is the execution boundary: each operation carries only its own inputs.
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
	Plugins   []string
	Selection localapp.PluginSelection
	Name      string
	JSON      bool
}

func (r rulesRequest) run(ctx context.Context, streams Command) (int, error) {
	selection := r.Selection
	if len(r.Plugins) > 0 {
		selection.Paths = r.Plugins
	}
	sources, err := selectedRulePlugins(ctx, selection)
	if err != nil {
		return 0, commandUsageError{err}
	}
	rules, err := localapp.RulesSources(ctx, sources)
	if err != nil {
		return 0, commandUsageError{err}
	}
	return 0, writeRules(streams.Stdout, r.Name, r.JSON, rules)
}

type doctorRequest struct {
	Ruff              string
	RuffOptions       *processconfig.Options
	Config            repoconfig.Selection
	ShellCheck        string
	ShellcheckOptions *processconfig.Options
	JSON              bool
	Hyperlinks        hyperlinkMode
}

func (r doctorRequest) run(ctx context.Context, streams Command) (int, error) {
	return 0, writeDoctor(ctx, streams.Stdout, r)
}

type configPathRequest struct {
	Config repoconfig.Selection
	JSON   bool
}

func (r configPathRequest) run(_ context.Context, streams Command) (int, error) {
	return 0, writeConfigPath(streams.Stdout, r)
}

type configShowRequest struct {
	Config       repoconfig.Selection
	JSON, Origin bool
}

func (r configShowRequest) run(_ context.Context, streams Command) (int, error) {
	return 0, writeConfigShow(streams.Stdout, r)
}

type configValidateRequest struct {
	Config repoconfig.Selection
	JSON   bool
}

func (r configValidateRequest) run(_ context.Context, streams Command) (int, error) {
	return 0, writeConfigValidation(streams.Stdout, r)
}

type configInitRequest struct {
	JSON       bool
	Hyperlinks hyperlinkMode
}

type configMigrateRequest struct {
	Config             repoconfig.Selection
	JSON, Write, Check bool
}

func (r configMigrateRequest) run(ctx context.Context, streams Command) (int, error) {
	return migrateCommandConfig(ctx, streams, r)
}

func (r configInitRequest) run(ctx context.Context, streams Command) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return 0, generateCommandConfig(ctx, streams.Stdout, r.JSON, r.Hyperlinks)
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
