package cli

import (
	"context"

	actionlint "actionlint.kjanat.dev/internal/engine"
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
	Name string
	JSON bool
}

func (r rulesRequest) run(_ context.Context, streams Command) (int, error) {
	return 0, writeRules(streams.Stdout, r.Name, r.JSON)
}

type doctorRequest struct {
	Plugins           []string
	Ruff              string
	RuffOptions       *actionlint.ExternalCommandOptions
	Config            actionlint.ConfigSelection
	ShellCheck        string
	ShellcheckOptions *actionlint.ExternalCommandOptions
	JSON              bool
	Hyperlinks        hyperlinkMode
}

func (r doctorRequest) run(ctx context.Context, streams Command) (int, error) {
	return 0, writeDoctor(ctx, streams.Stdout, r)
}

type configPathRequest struct {
	Config actionlint.ConfigSelection
	JSON   bool
}

func (r configPathRequest) run(_ context.Context, streams Command) (int, error) {
	return 0, writeConfigPath(streams.Stdout, r)
}

type configShowRequest struct {
	Config       actionlint.ConfigSelection
	Plugins      []string
	JSON, Origin bool
}

func (r configShowRequest) run(ctx context.Context, streams Command) (int, error) {
	return 0, writeConfigShow(ctx, streams.Stdout, r)
}

type configValidateRequest struct {
	Config  actionlint.ConfigSelection
	Plugins []string
	JSON    bool
}

func (r configValidateRequest) run(ctx context.Context, streams Command) (int, error) {
	return 0, writeConfigValidation(ctx, streams.Stdout, r)
}

type configInitRequest struct {
	JSON       bool
	Hyperlinks hyperlinkMode
}

func (r configInitRequest) run(ctx context.Context, streams Command) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return 0, generateCommandConfig(streams.Stdout, r.JSON, true, r.Hyperlinks)
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
	return 0, initLegacyCommandConfig(streams, r)
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
