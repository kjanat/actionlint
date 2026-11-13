package plugincmd

import (
	"context"
	"fmt"

	commandstreams "actionlint.kjanat.dev/v2/internal/cli/streams"
	pluginartifact "actionlint.kjanat.dev/v2/internal/plugins/artifacts"
	plugininstall "actionlint.kjanat.dev/v2/internal/plugins/installation"
)

type InspectOptions struct {
	Source string
	JSON   bool
}

func Inspect(ctx context.Context, streams commandstreams.Streams, r InspectOptions, acquisition plugininstall.Acquisition) (int, error) {
	info, err := plugininstall.Inspect(ctx, r.Source, acquisition)
	if err != nil {
		return 0, err
	}
	if r.JSON {
		return 0, commandstreams.WriteJSON(streams.Stdout, info)
	}
	if _, err := fmt.Fprintf(streams.Stdout, "%s\nKind: %s\nSHA256: %s\nSize: %d bytes (%d decoded)\n", info.Source, info.Kind, info.SHA256, info.Size, info.ModuleSize); err != nil {
		return 0, err
	}
	if info.Manifest != "" {
		if _, err := fmt.Fprintf(streams.Stdout, "Platform: %s\nImage manifest: sha256:%s\n", info.Platform, info.Manifest); err != nil {
			return 0, err
		}
	}
	if info.Kind == pluginartifact.ProviderManifest {
		if _, err := fmt.Fprintln(streams.Stdout, "Tool provider manifest:"); err != nil {
			return 0, err
		}
		return 0, commandstreams.WriteJSON(streams.Stdout, info.Metadata)
	}
	if info.Metadata == nil {
		_, err := fmt.Fprintln(streams.Stdout, "No actionlint.plugin custom section.")
		return 0, err
	}
	return 0, commandstreams.WriteJSON(streams.Stdout, info.Metadata)
}
