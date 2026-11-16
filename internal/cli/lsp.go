package cli

import (
	"context"
	"fmt"
	"os"

	inputmodel "actionlint.kjanat.dev/v2/internal/input"
	"actionlint.kjanat.dev/v2/internal/lsp"
)

type lspRequest struct{ Check checkInvocation }

func (r lspRequest) run(ctx context.Context, streams Command) (int, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return 0, err
	}
	c := r.Check
	for _, warning := range c.Deprecations {
		_, _ = fmt.Fprintln(streams.Stderr, "warning: "+warning)
	}
	plugins, err := c.pluginSelection(streams.Acquisition).Resolve(ctx, cwd, inputmodel.Request{Kind: inputmodel.Repository})
	if err != nil {
		return 0, err
	}
	options, err := c.checkingOptions(cwd, streams.Acquisition, streams.Stderr, false)
	if err != nil {
		return 0, err
	}
	return 0, lsp.LanguageServer(ctx, streams.Stdin, streams.Stdout, options, plugins)
}
