package installation

import (
	"fmt"
	"slices"

	"actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/integrations/dependencycache"
	"actionlint.kjanat.dev/v2/internal/model/pluginreference"
	"actionlint.kjanat.dev/v2/internal/model/rule"
	"actionlint.kjanat.dev/v2/internal/model/toolmanifest"
)

type DefaultPluginOptions struct {
	RuleSet configuration.RuleSet
	// Tools leaves out ids the invocation selects or disables explicitly.
	Tools    []string
	Warning  func(string)
	Overlays []configuration.Overlay
	// Missing Rules descriptors are read from local or cached provider manifests without executing guests.
	Rules       []rule.Descriptor
	Acquisition dependencycache.Acquisition
}

// DefaultReferences supplies defaults only when plugins was omitted, and an explicit empty list opts out through inheritance too.
func DefaultReferences(cfg *configuration.Config, options DefaultPluginOptions) ([]pluginreference.Reference, error) {
	if cfg.Plugins != nil {
		return slices.Clone(cfg.Plugins), nil
	}
	refs := []pluginreference.Reference{}
	manifests, err := toolmanifest.Bundled()
	if err != nil {
		return nil, err
	}
	for _, manifest := range manifests {
		id := manifest.Rule.Name
		if !slices.Contains(options.Tools, id) {
			continue
		}
		candidates, err := configuration.ConfigCandidates(id, cfg)
		if err != nil {
			return nil, err
		}
		if !slices.ContainsFunc(candidates, func(candidate *configuration.Config) bool {
			tool := candidate.Tools[id]
			return tool.Backend == nil && (tool.Enabled == nil || *tool.Enabled) && options.RuleSet.DiagnosticLevel(candidate, id) != "off"
		}) {
			continue
		}
		ref, err := pluginreference.ParsePluginReference(id)
		if err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	return refs, nil
}

func SourceFollowingNotes(refs []pluginreference.Reference) ([]string, error) {
	manifests, err := toolmanifest.Bundled()
	if err != nil {
		return nil, err
	}
	var notes []string
	for _, manifest := range manifests {
		id := manifest.Rule.Name
		if !manifest.Capabilities.ReportedSources || !slices.ContainsFunc(refs, func(ref pluginreference.Reference) bool {
			return ref.Name() == id && ref.Registry() == pluginreference.DefaultRegistry
		}) {
			continue
		}
		notes = append(notes, fmt.Sprintf("%s now uses the isolated Wasm plugin, which does not follow files the script sources. Explicit native configuration in tools.%s keeps the native backend.", id, id))
	}
	return notes, nil
}
