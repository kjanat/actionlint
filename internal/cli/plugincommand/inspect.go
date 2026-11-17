package plugincommand

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"actionlint.kjanat.dev/v2/internal/integrations/dependencycache"
	"actionlint.kjanat.dev/v2/internal/model/pluginreference"
	"actionlint.kjanat.dev/v2/internal/output"
	"actionlint.kjanat.dev/v2/internal/plugins/artifacts"
	"actionlint.kjanat.dev/v2/internal/plugins/installation"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginregistry"
)

type InspectOptions struct {
	Source   string
	JSON     bool
	Registry string
}

// Run reads plugin metadata without executing the plugin.
func (r InspectOptions) Run(ctx context.Context, streams output.Streams, acquisition dependencycache.Acquisition) (int, error) {
	info, err := inspectSource(ctx, r, acquisition)
	if err != nil {
		return 0, err
	}
	if r.JSON {
		return 0, output.WriteJSONRecord(streams.Stdout, info)
	}
	if _, err := fmt.Fprintf(streams.Stdout, "%s\nKind: %s\nSHA256: %s\nSize: %d bytes (%d decoded)\n", info.Source, info.Kind, info.SHA256, info.Size, info.ModuleSize); err != nil {
		return 0, err
	}
	if info.Manifest != "" {
		if _, err := fmt.Fprintf(streams.Stdout, "Platform: %s\nImage manifest: sha256:%s\n", info.Platform, info.Manifest); err != nil {
			return 0, err
		}
	}
	if info.Kind == artifacts.ProviderManifest {
		if _, err := fmt.Fprintln(streams.Stdout, "Tool provider manifest:"); err != nil {
			return 0, err
		}
		return 0, output.WriteJSONRecord(streams.Stdout, info.Metadata)
	}
	if info.Metadata == nil {
		_, err := fmt.Fprintln(streams.Stdout, "No actionlint.plugin custom section.")
		return 0, err
	}
	return 0, output.WriteJSONRecord(streams.Stdout, info.Metadata)
}

func inspectSource(ctx context.Context, r InspectOptions, acquisition dependencycache.Acquisition) (artifacts.ArtifactProvenance, error) {
	_, statErr := os.Stat(r.Source)
	local := statErr == nil || filepath.IsAbs(r.Source) || strings.HasPrefix(r.Source, ".") || strings.Contains(r.Source, `\`)
	if local || strings.Contains(r.Source, "://") {
		return installation.Inspect(ctx, r.Source, acquisition)
	}
	_, parseErr := pluginreference.ParsePluginReference(r.Source)
	if parseErr != nil && (strings.HasSuffix(r.Source, ".wasm") || strings.HasSuffix(r.Source, ".json") || strings.HasSuffix(r.Source, ".gz")) {
		return installation.Inspect(ctx, r.Source, acquisition)
	}
	ref, err := RequestedPlugin(r.Source, RegistryOptions{Registry: r.Registry}, nil)
	if err != nil {
		return artifacts.ArtifactProvenance{}, err
	}
	return pluginregistry.InspectRelease(ctx, ref, acquisition)
}
