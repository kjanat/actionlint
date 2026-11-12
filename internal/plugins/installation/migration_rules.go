package installation

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
	inputmodel "actionlint.kjanat.dev/v2/internal/input"
	config "actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/files"
	"actionlint.kjanat.dev/v2/internal/plugins/artifacts"
	"actionlint.kjanat.dev/v2/internal/plugins/artifacts/oci"
	"actionlint.kjanat.dev/v2/internal/plugins/manifests"
)

// cachedMigrationRules reads declarative metadata without running a guest or
// acquiring an artifact. Registry selection can consult release metadata; a
// missing cached manifest requires an explicit installation before migration.
func cachedMigrationRules(ctx context.Context, rules config.RuleSet, acquisition Acquisition, path string) ([]rule.Descriptor, error) {
	directory := filepath.Dir(path)
	sources, err := (Selection{RuleSet: rules, ConfigFile: path, Acquisition: acquisition}).SelectForCache(ctx, directory, inputmodel.Request{Kind: inputmodel.Repository})
	if err != nil {
		return nil, err
	}
	descriptors := []rule.Descriptor{}
	for _, source := range sources.Paths {
		if manifests.IsLocalManifest(source) {
			manifest, _, err := manifests.ReadLocal(files.New(ctx, nil, nil), source)
			if err != nil {
				return nil, err
			}
			descriptors = append(descriptors, manifest.Rule)
			continue
		}
		options := sources.ArtifactOptions(source)
		options.PullOCI = oci.Cached
		cachedSource, local, err := artifacts.NormalizeSource(source, directory)
		if err != nil {
			return nil, err
		}
		if local == "" && !artifacts.IsOCI(cachedSource) {
			location, err := url.Parse(cachedSource)
			if err != nil {
				return nil, err
			}
			if location.Scheme != "sha256" {
				if err := artifacts.RequireRemotePin(cachedSource); err != nil {
					return nil, err
				}
				cachedSource = "sha256://" + strings.TrimPrefix(location.Fragment, "sha256=")
			}
		}
		cached, err := artifacts.Resolve(ctx, cachedSource, options)
		if err != nil {
			return nil, fmt.Errorf("cached plugin manifest: %w", err)
		}
		if cached.Info.Kind != artifacts.ProviderManifest {
			return nil, fmt.Errorf("plugin %q has no declarative rule descriptor: %w", source, config.ErrRuleMetadataRequired)
		}
		manifest, err := manifests.Parse(cached.Bytes)
		if err != nil {
			return nil, err
		}
		descriptors = append(descriptors, manifest.Rule)
	}
	return descriptors, nil
}
