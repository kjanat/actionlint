package api

import "slices"

// Labels only searches and compares Names with slices.Contains and
// slices.Equal, so its fields stay a plain value record.
type Labels struct {
	Names    []string
	Expected []string
}

func (l *Labels) Has(name string) bool { return slices.Contains(l.Names, name) }
func (l *Labels) Matches() bool        { return slices.Equal(l.Names, l.Expected) }

// slices.ContainsFunc runs its callback and has no model.
type Matcher struct {
	Names []string // want "ARC004: type Matcher exposes writable field Names"
}

func (m *Matcher) Empty() bool {
	return slices.ContainsFunc(m.Names, func(name string) bool { return name == "" })
}
