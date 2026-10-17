package cli

import (
	"context"
	"fmt"

	"actionlint.kjanat.dev/internal/localapp"
)

type pluginInspectRequest struct {
	Source string
	JSON   bool
}

func (r pluginInspectRequest) run(ctx context.Context, streams Command) (int, error) {
	info, err := localapp.InspectPlugin(ctx, r.Source)
	if err != nil {
		return 0, err
	}
	if r.JSON {
		return 0, writeCommandJSON(streams.Stdout, info)
	}
	if _, err := fmt.Fprintf(streams.Stdout, "%s\nKind: %s\nSHA256: %s\nSize: %d bytes (%d decoded)\n", info.Source, info.Kind, info.SHA256, info.Size, info.ModuleSize); err != nil {
		return 0, err
	}
	if info.Metadata == nil {
		_, err := fmt.Fprintln(streams.Stdout, "No actionlint.plugin custom section.")
		return 0, err
	}
	return 0, writeCommandJSON(streams.Stdout, info.Metadata)
}
