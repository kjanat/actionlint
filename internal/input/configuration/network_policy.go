package config

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"actionlint.kjanat.dev/v2/internal/input/configuration/binding"
	"actionlint.kjanat.dev/v2/internal/input/configuration/document"
	configsource "actionlint.kjanat.dev/v2/internal/input/configuration/source"
	"actionlint.kjanat.dev/v2/internal/input/configuration/tree"
	acquisitionhttp "actionlint.kjanat.dev/v2/internal/integrations/http"
	"github.com/bmatcuk/doublestar/v4"
)

// NetworkPolicyFile supplies defaults and restrictions independently of project configuration.
type NetworkPolicyFile struct {
	// Extends loads local or HTTPS policy files. Restrictions from every file apply.
	Extends      []string                  `yaml:"extends,omitempty"`
	Defaults     NetworkPolicyDefaults     `yaml:"defaults,omitempty" jsonschema:"nullable"`
	Restrictions NetworkPolicyRestrictions `yaml:"restrictions,omitempty" jsonschema:"nullable"`
}

type NetworkPolicyDefaults struct {
	// Network supplies settings that projects may override.
	Network NetworkConfig `yaml:"network,omitempty" jsonschema:"nullable"`
}

type NetworkPolicyRestrictions struct {
	Network NetworkRestrictions `yaml:"network,omitempty" jsonschema:"nullable"`
}

type NetworkRestrictions struct {
	MetadataMirrors MetadataMirrorRestrictions `yaml:"metadata-mirrors,omitempty" jsonschema:"nullable"`
}

type MetadataMirrorRestrictions struct {
	// ExcludeServices prevents requests to these metadata mirrors.
	ExcludeServices MetadataMirrorServices `yaml:"exclude-services,omitempty" jsonschema:"nullable"`
	// ExcludeRepositories matches host/owner/repository, such as github.com/company/private-*.
	ExcludeRepositories []string `yaml:"exclude-repositories,omitempty" jsonschema:"nullable"`
	// RequirePublic prevents requests to mirrors when repository visibility is unknown.
	RequirePublic bool `yaml:"require-public,omitempty" jsonschema:"nullable"`
}

type mirrorRestriction struct {
	settings MetadataMirrorRestrictions
	source   string
}

// NetworkPolicy combines all loaded policy files. Its restrictions cannot be replaced by a project.
type NetworkPolicy struct {
	defaults       NetworkConfig
	defaultOrigins map[string]Input
	restrictions   []mirrorRestriction
	files          []string
}

func (p *NetworkPolicy) Files() []string {
	if p == nil {
		return nil
	}
	return slices.Clone(p.files)
}

// LoadNetworkPolicy reads machine, user and ACTIONLINT_NETWORK_POLICY settings in that order.
func LoadNetworkPolicy(ctx context.Context, connections acquisitionhttp.Connections) (*NetworkPolicy, error) {
	var paths []string
	for _, path := range networkPolicyFiles() {
		if _, err := os.Lstat(path); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return nil, fmt.Errorf("read network policy %q: %w", path, err)
		}
		paths = append(paths, path)
	}
	if path := os.Getenv("ACTIONLINT_NETWORK_POLICY"); path != "" {
		paths = append(paths, path)
	}
	return ReadNetworkPolicy(paths, configsource.New(ctx, nil, connections).ReadConfig)
}

func networkPolicyFiles() []string {
	if runtime.GOOS == "js" {
		return nil
	}
	var paths []string
	machine := "/etc/actionlint/network-policy.toml"
	if runtime.GOOS == "windows" {
		machine = ""
		if directory := os.Getenv("ProgramData"); directory != "" {
			machine = filepath.Join(directory, "actionlint", "network-policy.toml")
		}
	}
	if machine != "" {
		paths = append(paths, machine)
	}
	if user, err := os.UserConfigDir(); err == nil {
		paths = append(paths, filepath.Join(user, "actionlint", "network-policy.toml"))
	}
	return paths
}

func ReadNetworkPolicy(paths []string, read func(string) ([]byte, error)) (*NetworkPolicy, error) {
	p := &NetworkPolicy{defaultOrigins: map[string]Input{}}
	var defaults any
	for _, path := range paths {
		loaded, err := tree.Load(path, read, func(origin string, doc document.Document) (document.Document, error) {
			object, ok := doc.Value.(map[string]any)
			if !ok {
				return doc, errors.New("network policy must be an object")
			}
			object = maps.Clone(object)
			if value, exists := object["$schema"]; exists {
				if _, ok := value.(string); !ok {
					return doc, errors.New("$schema must be a string")
				}
			}
			delete(object, "$schema")
			doc.Value = object
			var file NetworkPolicyFile
			if err := binding.Decode(doc.Value, &file); err != nil {
				return doc, sourceError(err, documentInputs(doc))
			}
			restriction := file.Restrictions.Network.MetadataMirrors
			for i, service := range restriction.ExcludeServices {
				if service != "jsdelivr" {
					return doc, fmt.Errorf("restrictions.network.metadata-mirrors.exclude-services[%d]: unknown mirror %q", i, service)
				}
			}
			for i, pattern := range restriction.ExcludeRepositories {
				parts := strings.Split(pattern, "/")
				if len(parts) != 3 || slices.Contains(parts, "") || !doublestar.ValidatePattern(pattern) {
					return doc, fmt.Errorf("restrictions.network.metadata-mirrors.exclude-repositories[%d]: expected a host/owner/repository glob, got %q", i, pattern)
				}
			}
			p.restrictions = append(p.restrictions, mirrorRestriction{restriction, origin})
			return doc, nil
		})
		if err != nil {
			return nil, err
		}
		root, _ := loaded.Value.(map[string]any)
		if value, present := root["defaults"]; present {
			origins := map[string]Input{}
			for key, origin := range loaded.Origins {
				if key == "/defaults" || strings.HasPrefix(key, "/defaults/") {
					origins[strings.TrimPrefix(key, "/defaults")] = origin
				}
			}
			tree.MergeOrigins(p.defaultOrigins, origins, defaults, value)
			defaults = tree.Merge(defaults, value)
		}
		p.files = append(p.files, loaded.Files...)
	}
	var selected NetworkPolicyDefaults
	if err := binding.Decode(defaults, &selected); err != nil {
		return nil, err
	}
	p.defaults = selected.Network
	p.files = slices.Compact(slices.Sorted(slices.Values(p.files)))
	return p, nil
}

func (p *NetworkPolicy) Resolve(rules RuleSet, resolved Resolved) (Resolved, error) {
	if p == nil || len(p.files) == 0 {
		return resolved, nil
	}
	requested := MetadataMirrorsConfig{}
	if resolved.Config != nil {
		requested = resolved.Config.Network.MetadataMirrors
	}
	cfg, err := p.Apply(resolved.Config)
	if err != nil {
		return Resolved{}, err
	}
	resolved.Config = cfg
	resolved.Values, err = rules.EffectiveValues(cfg)
	if err != nil {
		return Resolved{}, err
	}
	resolved.Origins = maps.Clone(resolved.Origins)
	if resolved.Origins == nil {
		resolved.Origins = map[string]Origin{}
	}
	setOrigin := func(key string, input Input) {
		resolved.Origins["/network/metadata-mirrors/"+key] = Origin{Source: "policy", State: "value", File: input.File, Line: input.Line, Column: input.Column}
	}
	for key, absent := range map[string]bool{"services": requested.Services == nil, "allow-unknown-repositories": requested.AllowUnknownRepositories == nil} {
		if input, ok := p.defaultOrigins["/network/metadata-mirrors/"+key]; absent && ok {
			setOrigin(key, input)
		}
	}
	for _, restriction := range p.restrictions {
		if len(restriction.settings.ExcludeServices) != 0 {
			setOrigin("services", Input{File: restriction.source})
		}
		if restriction.settings.RequirePublic {
			setOrigin("allow-unknown-repositories", Input{File: restriction.source})
		}
	}
	return resolved, nil
}

func (p *NetworkPolicy) Apply(cfg *Config) (*Config, error) {
	if p == nil || len(p.files) == 0 {
		return cfg, nil
	}
	var next Config
	if cfg != nil {
		next = *cfg
	}
	requested := next.Network.MetadataMirrors
	settings := p.defaults.MetadataMirrors
	if requested.Services != nil {
		settings.Services = requested.Services
	}
	if requested.AllowUnknownRepositories != nil {
		settings.AllowUnknownRepositories = requested.AllowUnknownRepositories
	}
	services := MetadataMirrorServices{"jsdelivr"}
	if settings.Services != nil {
		services = slices.Clone(*settings.Services)
	}
	var excluded []string
	for _, restriction := range p.restrictions {
		for _, service := range restriction.settings.ExcludeServices {
			if requested.Services != nil && slices.Contains(*requested.Services, service) {
				return nil, fmt.Errorf("network.metadata-mirrors.services enables %q, forbidden by network policy %q", service, restriction.source)
			}
			services = slices.DeleteFunc(services, func(item string) bool { return item == service })
		}
		if restriction.settings.RequirePublic {
			if requested.AllowUnknownRepositories != nil && *requested.AllowUnknownRepositories {
				return nil, fmt.Errorf("network.metadata-mirrors.allow-unknown-repositories is true, forbidden by require-public in network policy %q", restriction.source)
			}
			settings.AllowUnknownRepositories = new(false)
		}
		excluded = append(excluded, restriction.settings.ExcludeRepositories...)
	}
	settings.Services = &services
	if settings.AllowUnknownRepositories != nil {
		settings.AllowUnknownRepositories = new(*settings.AllowUnknownRepositories)
	}
	next.Network = NetworkConfig{MetadataMirrors: settings, excludedRepositories: slices.Compact(slices.Sorted(slices.Values(excluded)))}
	return &next, nil
}
