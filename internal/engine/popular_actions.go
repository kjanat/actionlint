package engine

import (
	"strings"

	"actionlint.kjanat.dev/internal/actioncatalog"
)

// PopularActions contains bundled metadata indexed by exact action reference.
// Each entry owns its maps and input/output objects for compatibility with callers
// which customize this exported map before analysis.
// OutdatedPopularActionSpecs contains exact references with retired runtimes or services.
var PopularActions, OutdatedPopularActionSpecs = materializePopularActions(actioncatalog.Default())

// LoadPopularActions replaces the catalogue after validating all of its data.
// Call it before analysis starts; like direct changes to PopularActions, it must
// not run concurrently with linting. Invalid data leaves the current maps intact.
func LoadPopularActions(data []byte) error {
	catalog, err := actioncatalog.Decode(data)
	if err != nil {
		return err
	}
	PopularActions, OutdatedPopularActionSpecs = materializePopularActions(catalog)
	return nil
}

func materializePopularActions(catalog *actioncatalog.Catalog) (map[string]*ActionMetadata, map[string]struct{}) {
	actions := make(map[string]*ActionMetadata, len(catalog.References))
	for ref, id := range catalog.References {
		d := catalog.Definitions[id]
		m := &ActionMetadata{Name: d.Name, Runs: ActionMetadataRuns{Using: d.Runtime}, SkipInputs: d.SkipInputs, SkipOutputs: d.SkipOutputs}
		if len(d.Inputs) > 0 {
			m.Inputs = make(ActionMetadataInputs, len(d.Inputs))
			for _, name := range d.Inputs {
				key := strings.ToLower(name)
				message, deprecated := d.Deprecated[key]
				m.Inputs[key] = &ActionMetadataInput{Name: name, Deprecated: deprecated, DeprecationMessage: message}
			}
			for _, key := range d.Required {
				m.Inputs[key].Required = true
			}
		}
		if len(d.Outputs) > 0 {
			m.Outputs = make(ActionMetadataOutputs, len(d.Outputs))
			for _, name := range d.Outputs {
				m.Outputs[strings.ToLower(name)] = &ActionMetadataOutput{Name: name}
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

// lookupPopularAction uses the compatibility maps so caller overrides remain
// visible to both input validation and output typing. Unknown refs stay unknown.
func lookupPopularAction(ref string) (*ActionMetadata, bool) {
	meta := PopularActions[ref]
	_, retired := OutdatedPopularActionSpecs[ref]
	return meta, meta == nil && retired
}
