package localapp

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/fsread"
	"actionlint.kjanat.dev/v2/internal/plugins/artifact"
	"actionlint.kjanat.dev/v2/internal/plugins/artifact/oci"
	"actionlint.kjanat.dev/v2/internal/tools/provider"
	"actionlint.kjanat.dev/v2/rule"
)

// cachedMigrationRules reads declarative metadata without running a guest or
// acquiring an artifact. Registry selection can consult release metadata; a
// missing cached manifest requires an explicit installation before migration.
func cachedMigrationRules(ctx context.Context, path string) ([]rule.Descriptor, error) {
	directory := filepath.Dir(path)
	sources, err := (PluginSelection{ConfigFile: path}).SelectForCache(ctx, directory, application.Input{Kind: application.Repository})
	if err != nil {
		return nil, err
	}
	descriptors := []rule.Descriptor{}
	for _, source := range sources.Paths {
		if provider.IsLocalManifest(source) {
			manifest, _, err := provider.ReadLocal(fsread.New(ctx, nil, nil), source)
			if err != nil {
				return nil, err
			}
			descriptors = append(descriptors, manifest.Rule)
			continue
		}
		options := sources.artifactOptions(source)
		options.PullOCI = oci.Cached
		cachedSource, local, err := artifact.NormalizeSource(source, directory)
		if err != nil {
			return nil, err
		}
		if local == "" && !artifact.IsOCI(cachedSource) {
			location, err := url.Parse(cachedSource)
			if err != nil {
				return nil, err
			}
			if location.Scheme != "sha256" {
				if err := artifact.RequireRemotePin(cachedSource); err != nil {
					return nil, err
				}
				cachedSource = "sha256://" + strings.TrimPrefix(location.Fragment, "sha256=")
			}
		}
		cached, err := artifact.Resolve(ctx, cachedSource, options)
		if err != nil {
			return nil, fmt.Errorf("cached plugin manifest: %w", err)
		}
		if cached.Info.Kind != artifact.ProviderManifest {
			return nil, fmt.Errorf("plugin %q has no declarative rule descriptor: %w", source, config.ErrRuleMetadataRequired)
		}
		manifest, err := provider.Parse(cached.Bytes)
		if err != nil {
			return nil, err
		}
		descriptors = append(descriptors, manifest.Rule)
	}
	return descriptors, nil
}
