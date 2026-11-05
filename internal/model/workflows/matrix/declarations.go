package matrix

import (
	"slices"
	"strings"

	value "actionlint.kjanat.dev/v2/internal/model/contexts/values"
)

// Axis describes one declared row. Values do not enumerate the full domain
// when Complete is false.
type Axis struct {
	Name     string
	Values   []value.Value
	Complete bool
	Origin   value.Origin
}

func axisDeclaration(axis matrixAxis) Axis {
	return Axis{Name: axis.Name, Values: slices.Clone(axis.Values), Complete: axis.Complete, Origin: axis.Origin}
}

// Axes returns detached declarations in the resolution's axis order.
func (r Resolution) Axes() []Axis {
	axes := make([]Axis, len(r.axes))
	for i, axis := range r.axes {
		axes[i] = axisDeclaration(axis)
	}
	return axes
}

// Axis looks up a base axis without treating include-only fields as axes.
func (r Resolution) Axis(name string) (Axis, bool) {
	index := axisIndex(r.axes, strings.ToLower(name))
	if index < 0 {
		return Axis{}, false
	}
	return axisDeclaration(r.axes[index]), true
}

// Inclusions returns the declared additions, before matching them to base rows.
func (r Resolution) Inclusions() []Combination { return slices.Clone(r.include) }

// Exclusions returns the declared filters, including invalid or unmatched filters.
func (r Resolution) Exclusions() []Combination { return slices.Clone(r.exclude) }
