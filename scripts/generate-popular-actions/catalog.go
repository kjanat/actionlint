package main

import (
	"encoding/json"
	"io"
	"maps"
	"slices"
	"strings"

	"actionlint.kjanat.dev"
	"actionlint.kjanat.dev/internal/actioncatalog"
)

func (g *gen) writeCatalog(out io.Writer, actions map[string]*actionlint.ActionMetadata) error {
	c := actioncatalog.Catalog{Schema: "popular_actions.schema.json", Version: 1, References: map[string]string{}, Definitions: map[string]actioncatalog.Definition{}, Retired: []string{}}
	identities := map[string]string{}
	for _, spec := range slices.Sorted(maps.Keys(actions)) {
		m := actions[spec]
		if isOutdated(spec, m.Runs.Using) {
			c.Retired = append(c.Retired, spec)
			continue
		}
		d := actioncatalog.Definition{Name: m.Name, SkipInputs: m.SkipInputs, SkipOutputs: m.SkipOutputs}
		if strings.HasPrefix(strings.ToLower(m.Runs.Using), "node") {
			d.Runtime = m.Runs.Using
		}
		if !m.SkipInputs {
			for _, key := range slices.Sorted(maps.Keys(m.Inputs)) {
				i := m.Inputs[key]
				d.Inputs = append(d.Inputs, i.Name)
				if i.Required {
					d.Required = append(d.Required, key)
				}
				if i.Deprecated {
					if d.Deprecated == nil {
						d.Deprecated = map[string]string{}
					}
					d.Deprecated[key] = i.DeprecationMessage
				}
			}
		}
		if !m.SkipOutputs {
			for _, key := range slices.Sorted(maps.Keys(m.Outputs)) {
				d.Outputs = append(d.Outputs, m.Outputs[key].Name)
			}
		}
		encoded, err := json.Marshal(d)
		if err != nil {
			return err
		}
		id, exists := identities[string(encoded)]
		if !exists {
			id = spec
			identities[string(encoded)] = id
			c.Definitions[id] = d
		}
		c.References[spec] = id
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(c)
}
