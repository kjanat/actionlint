// Package mapping resolves ordered, state-aware workflow mapping entries.
package mapping

import (
	"iter"
	"maps"
	"slices"
	"strings"

	"actionlint.kjanat.dev/v2/internal/workflow/value"
)

// Input is one declared mapping entry. A missing Name uses its map key.
type Input struct {
	Name      value.Value
	Value     value.Value
	Source    string
	Evaluated bool
}

// Inputs is the source declaration consumed at the syntax ownership boundary.
type Inputs struct {
	Values     map[string]Input
	Expression value.Value
	Insertions []*Inputs
}

type mappingEntry struct {
	name  value.Value
	value value.Value
}

// Declaration preserves source-facing key data independently of lookup entries.
type Declaration struct {
	Key       value.Value
	Name      string
	State     KeyState
	Source    string
	Evaluated bool
	Origin    value.Origin
}

// Mapping is an immutable resolved mapping.
type Mapping struct {
	entries  []mappingEntry
	decls    []Declaration
	byName   map[string][]int
	complete bool
}

// Resolve applies key coercion, expression classification, and insertions once.
func Resolve(inputs *Inputs) Mapping {
	result := Mapping{byName: map[string][]int{}, complete: true}
	if inputs == nil {
		return result
	}
	add := func(name, item value.Value, source string, evaluated bool) {
		text, state := ResolveKey(name)
		result.decls = append(result.decls, Declaration{
			Key: name, Name: text, State: state, Source: source, Evaluated: evaluated, Origin: name.Origin(),
		})
		if state != KnownKey {
			result.complete = false
			return
		}
		key := strings.ToLower(text)
		entry := mappingEntry{name: name, value: item}
		result.byName[key] = append(result.byName[key], len(result.entries))
		result.entries = append(result.entries, entry)
	}
	for _, sourceName := range slices.Sorted(maps.Keys(inputs.Values)) {
		input := inputs.Values[sourceName]
		name := input.Name
		if name.State() == value.Missing {
			name = value.YAMLScalar(sourceName, "!!str", value.Origin{})
		}
		add(name, input.Value, input.Source, input.Evaluated)
	}
	if inputs.Expression.State() != value.Missing {
		expression := inputs.Expression
		if expression.Known() && expression.Kind() == value.Object {
			for name, item := range expression.Props() {
				add(value.FromJSON(name, expression.Origin()), item, name, true)
			}
		} else {
			result.complete = false
		}
	}
	for _, insertion := range inputs.Insertions {
		inserted := Resolve(insertion)
		result.complete = result.complete && inserted.complete
		result.decls = append(result.decls, inserted.decls...)
		for _, item := range inserted.entries {
			key, state := ResolveKey(item.name)
			if state != KnownKey {
				result.complete = false
				continue
			}
			key = strings.ToLower(key)
			result.byName[key] = append(result.byName[key], len(result.entries))
			result.entries = append(result.entries, item)
		}
	}
	return result
}

// Declarations yields source-facing declaration records in source traversal order.
func (m Mapping) Declarations() iter.Seq[Declaration] {
	decls := slices.Clone(m.decls)
	return slices.Values(decls)
}

// Lookup returns Missing only when the mapping is complete and the key absent.
func (m Mapping) Lookup(name string) value.Value {
	indices := m.byName[strings.ToLower(name)]
	if !m.Complete() {
		if len(indices) != 0 {
			return value.DynamicValue("", value.KindUnknown, m.entries[indices[0]].name.Origin())
		}
		return value.DynamicValue("", value.KindUnknown, value.Origin{})
	}
	if len(indices) == 0 {
		return value.Value{}
	}
	if len(indices) != 1 {
		return value.DynamicValue("", value.KindUnknown, m.entries[indices[0]].name.Origin())
	}
	entry := m.entries[indices[0]]
	if entry.value.State() == value.Missing {
		return value.DynamicValue("", value.KindUnknown, entry.name.Origin())
	}
	return entry.value
}

// Entries yields canonical names and values in stable order.
func (m Mapping) Entries() iter.Seq2[string, value.Value] {
	entries := m.KeyedEntries()
	return func(yield func(string, value.Value) bool) {
		for name, item := range entries {
			text, ok := name.ScalarString()
			if !ok || !yield(text, item) {
				return
			}
		}
	}
}

// KeyedEntries yields canonical key values with their declaration origins.
func (m Mapping) KeyedEntries() iter.Seq2[value.Value, value.Value] {
	return func(yield func(value.Value, value.Value) bool) {
		for _, entry := range m.entries {
			if !yield(entry.name, entry.value) {
				return
			}
		}
	}
}

// Complete reports whether all declared keys and inserted mappings are known.
func (m Mapping) Complete() bool { return m.byName == nil || m.complete }
