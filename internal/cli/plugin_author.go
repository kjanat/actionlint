package cli

import (
	"context"
	"errors"

	plugincmd "actionlint.kjanat.dev/v2/internal/cli/plugin"
	"github.com/spf13/cobra"
)

type (
	pluginValidateRequest plugincmd.ValidateOptions
	pluginTestRequest     plugincmd.TestOptions
)

func (a *commandApp) pluginAuthoringCommands() []*cobra.Command {
	validate := &cobra.Command{
		Use: "validate [manifest]", Short: "Validate a process provider manifest without running it",
		Long: "Validate a process provider manifest and compile its jq transforms without running the declared tool.\n\nExamples:\n  actionlint plugin validate\n  actionlint plugin validate ./actionlint-provider.json --json\n  actionlint plugin validate - < manifest.json",
		Args: cobra.MaximumNArgs(1), RunE: a.capture(operationPluginValidate),
	}
	validate.Flags().BoolVar(&a.inv.JSON, "json", false, "Write a JSON report")
	test := &cobra.Command{
		Use: "test [manifest fixtures]", Short: "Test process provider transforms against fixtures without running the tool",
		Long: "Run manifest jq transforms over fixture stdout and configuration. The declared tool is never run.\n\nExamples:\n  actionlint plugin test ./actionlint-provider.json ./fixtures.json --json\n  actionlint plugin test --json < test-input.json",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 0 && len(args) != 2 {
				return errors.New("accepts either no arguments or exactly two paths")
			}
			return nil
		}, RunE: a.capture(operationPluginTest),
	}
	test.Flags().BoolVar(&a.inv.JSON, "json", false, "Write a JSON report")
	return []*cobra.Command{validate, test}
}

func (r pluginValidateRequest) run(ctx context.Context, streams Command) (int, error) {
	return plugincmd.Validate(ctx, streams.commandStreams(), plugincmd.ValidateOptions(r))
}

func (r pluginTestRequest) run(ctx context.Context, streams Command) (int, error) {
	return plugincmd.Test(ctx, streams.commandStreams(), plugincmd.TestOptions(r))
}
