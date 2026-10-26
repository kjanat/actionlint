package actioncatalog

import (
	"strings"

	actionmeta "actionlint.kjanat.dev/v2/internal/action"
)

// PopularActions contains bundled metadata indexed by exact action reference.
// Each entry owns its maps and input/output objects for compatibility with callers
// which customize this exported map before analysis.
// OutdatedPopularActionSpecs contains exact references with retired runtimes or services.
var PopularActions, OutdatedPopularActionSpecs = materializeMetadata(Default())

// Load replaces the metadata catalogue after validating all of its data.
// Call it before analysis starts; like direct changes to PopularActions, it must
// not run concurrently with linting. Invalid data leaves the current maps intact.
func Load(data []byte) error {
	catalog, err := Decode(data)
	if err != nil {
		return err
	}
	PopularActions, OutdatedPopularActionSpecs = materializeMetadata(catalog)
	return nil
}

func materializeMetadata(catalog *Catalog) (map[string]*actionmeta.Metadata, map[string]struct{}) {
	actions := make(map[string]*actionmeta.Metadata, len(catalog.References))
	for ref, id := range catalog.References {
		d := catalog.Definitions[id]
		m := &actionmeta.Metadata{Name: d.Name, Runs: actionmeta.Runs{Using: d.Runtime}, SkipInputs: d.SkipInputs, SkipOutputs: d.SkipOutputs}
		if len(d.Inputs) > 0 {
			m.Inputs = make(actionmeta.Inputs, len(d.Inputs))
			for _, name := range d.Inputs {
				key := strings.ToLower(name)
				message, deprecated := d.Deprecated[key]
				m.Inputs[key] = &actionmeta.Input{Name: name, Deprecated: deprecated, DeprecationMessage: message}
			}
			for _, key := range d.Required {
				m.Inputs[key].Required = true
			}
		}
		if len(d.Outputs) > 0 {
			m.Outputs = make(actionmeta.Outputs, len(d.Outputs))
			for _, name := range d.Outputs {
				m.Outputs[strings.ToLower(name)] = &actionmeta.Output{Name: name}
			}
		}
		actions[ref] = m
	}
	retired := make(map[string]struct{}, len(catalog.Retired))
	for _, ref := range catalog.Retired {
		retired[ref] = struct{}{}
	}
	return actions, retired
}

// Lookup reads PopularActions and OutdatedPopularActionSpecs directly so input
// validation and output typing use caller overrides to those maps.
func Lookup(ref string) (*actionmeta.Metadata, bool) {
	meta := PopularActions[ref]
	_, retired := OutdatedPopularActionSpecs[ref]
	return meta, meta == nil && retired
}
