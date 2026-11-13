package config

import (
	"fmt"
	"slices"
	"strconv"

	"actionlint.kjanat.dev/v2/internal/input/configuration/binding"
	"actionlint.kjanat.dev/v2/internal/input/references"
)

type NetworkConfig struct {
	// MetadataMirrors selects fallback services for remote action and workflow files.
	MetadataMirrors      MetadataMirrorsConfig `yaml:"metadata-mirrors,omitempty" jsonschema:"nullable"`
	excludedRepositories []string
}

type MetadataMirrorsConfig struct {
	// Services defaults to jsdelivr. An empty array disables metadata mirrors.
	Services *MetadataMirrorServices `yaml:"services,omitempty" jsonschema:"nullable"`
	// AllowUnknownRepositories permits sending repository names, commit hashes and
	// file paths to the selected services without first establishing public visibility.
	// Known private repositories are excluded regardless of this setting.
	AllowUnknownRepositories *bool `yaml:"allow-unknown-repositories,omitempty" jsonschema:"nullable"`
}

type MetadataMirrorServices []string

func (c *MetadataMirrorsConfig) DecodeConfig(value any) error {
	type plain MetadataMirrorsConfig
	var next plain
	if err := binding.Decode(value, &next); err != nil {
		return err
	}
	if next.Services != nil {
		for i, service := range *next.Services {
			if service != "jsdelivr" {
				return binding.At("services", binding.At(strconv.Itoa(i), fmt.Errorf("unknown metadata mirror %q; supported service: jsdelivr", service)))
			}
		}
	}
	*c = MetadataMirrorsConfig(next)
	return nil
}

func (cfg *Config) MetadataMirrorAccess() references.MirrorAccess {
	if cfg == nil {
		return references.PublicMirrorsOnly
	}
	mirrors := cfg.Network.MetadataMirrors
	if mirrors.Services != nil && len(*mirrors.Services) == 0 {
		return references.NoMirrors
	}
	if mirrors.AllowUnknownRepositories != nil && *mirrors.AllowUnknownRepositories {
		return references.AllowUnknownMirrors
	}
	return references.PublicMirrorsOnly
}

func (cfg *Config) MetadataMirrorExclusions() []string {
	if cfg == nil {
		return nil
	}
	return slices.Clone(cfg.Network.excludedRepositories)
}
