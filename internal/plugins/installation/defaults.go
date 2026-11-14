package installation

import (
	"fmt"
	"slices"

	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
	config "actionlint.kjanat.dev/v2/internal/input/configuration"
	pluginmanifests "actionlint.kjanat.dev/v2/internal/plugins/manifests"
	pluginref "actionlint.kjanat.dev/v2/internal/plugins/manifests/references"
)

// DefaultPluginOptions selects registry defaults for tools without an explicit
// native command. Configured backends and disabled tools take precedence.
type DefaultPluginOptions struct {
	RuleSet config.RuleSet
	// Tools lists the bundled tool ids that run their default command in
	// this invocation. Each may receive a default plugin declaration; ids
	// the invocation selected or disabled explicitly are left out.
	Tools    []string
	Warning  func(string)
	Overlays []config.Overlay
	// Rules supplies already inspected plugin descriptors. Missing descriptors
	// are read from local or cached provider manifests, never by executing guests.
	Rules       []rule.Descriptor
	Acquisition Acquisition
}

// DefaultReferences supplies the v2 defaults only when plugins was omitted.
// An explicit empty list is a durable opt-out, including through inheritance.
// It declares, in bundled directory order, the registry plugin named after
// each bundled tool in options.Tools that configuration leaves enabled and
// on its default backend.
func DefaultReferences(cfg *config.Config, options DefaultPluginOptions) ([]pluginref.Reference, error) {
	if cfg.Plugins != nil {
		return slices.Clone(cfg.Plugins), nil
	}
	refs := []pluginref.Reference{}
	manifests, err := pluginmanifests.Bundled()
	if err != nil {
		return nil, err
	}
	for _, manifest := range manifests {
		id := manifest.Rule.Name
		tool := cfg.Tools[id]
		if !slices.Contains(options.Tools, id) || tool.Backend != nil || tool.Enabled != nil && !*tool.Enabled || options.RuleSet.DiagnosticLevel(cfg, id) == "off" {
			continue
		}
		ref, err := pluginref.Parse(id)
		if err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	return refs, nil
}

// SourceFollowingNotes warns, for each default registry plugin in refs that
// replaces a bundled tool reporting findings in files its scripts source,
// that the isolated plugin stops following those files.
func SourceFollowingNotes(refs []pluginref.Reference) ([]string, error) {
	manifests, err := pluginmanifests.Bundled()
	if err != nil {
		return nil, err
	}
	var notes []string
	for _, manifest := range manifests {
		id := manifest.Rule.Name
		if !manifest.Capabilities.ReportedSources || !slices.ContainsFunc(refs, func(ref pluginref.Reference) bool {
			return ref.Name() == id && ref.Registry() == pluginref.DefaultRegistry
		}) {
			continue
		}
		notes = append(notes, fmt.Sprintf("%s now uses the isolated Wasm plugin, which does not follow files the script sources. Explicit native configuration in tools.%s keeps the native backend.", id, id))
	}
	return notes, nil
}
