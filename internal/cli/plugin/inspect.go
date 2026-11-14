package plugincmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	commandstreams "actionlint.kjanat.dev/v2/internal/cli/streams"
	pluginartifact "actionlint.kjanat.dev/v2/internal/plugins/artifacts"
	plugininstall "actionlint.kjanat.dev/v2/internal/plugins/installation"
	pluginref "actionlint.kjanat.dev/v2/internal/plugins/manifests/references"
)

type InspectOptions struct {
	Source   string
	JSON     bool
	Registry string
}

func Inspect(ctx context.Context, streams commandstreams.Streams, r InspectOptions, acquisition plugininstall.Acquisition) (int, error) {
	info, err := inspectSource(ctx, r, acquisition)
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

func inspectSource(ctx context.Context, r InspectOptions, acquisition plugininstall.Acquisition) (pluginartifact.Info, error) {
	_, statErr := os.Stat(r.Source)
	local := statErr == nil || filepath.IsAbs(r.Source) || strings.HasPrefix(r.Source, ".") || strings.Contains(r.Source, `\`)
	if local || strings.Contains(r.Source, "://") {
		return plugininstall.Inspect(ctx, r.Source, acquisition)
	}
	_, parseErr := pluginref.Parse(r.Source)
	if parseErr != nil && (strings.HasSuffix(r.Source, ".wasm") || strings.HasSuffix(r.Source, ".json") || strings.HasSuffix(r.Source, ".gz")) {
		return plugininstall.Inspect(ctx, r.Source, acquisition)
	}
	ref, err := RequestedPlugin(r.Source, RegistryOptions{Registry: r.Registry}, nil)
	if err != nil {
		return pluginartifact.Info{}, err
	}
	return plugininstall.InspectRelease(ctx, ref, acquisition)
}
