package matrix

import (
	"slices"
	"strings"

	"actionlint.kjanat.dev/v2/internal/model/contexts/value"
)

// Axis values do not enumerate the full domain when Complete is false.
type Axis struct {
	Name     string
	Values   []value.Value
	Complete bool
	Origin   value.Origin
}

func axisDeclaration(axis matrixAxis) Axis {
	return Axis{Name: axis.Name, Values: slices.Clone(axis.Values), Complete: axis.Complete, Origin: axis.Origin}
}

func (r Resolution) Axes() []Axis {
	axes := make([]Axis, len(r.axes))
	for i, axis := range r.axes {
		axes[i] = axisDeclaration(axis)
	}
	return axes
}

func (r Resolution) Axis(name string) (Axis, bool) {
	index := axisIndex(r.axes, strings.ToLower(name))
	if index < 0 {
		return Axis{}, false
	}
	return axisDeclaration(r.axes[index]), true
}

func (r Resolution) Inclusions() []Combination { return slices.Clone(r.include) }

func (r Resolution) Exclusions() []Combination { return slices.Clone(r.exclude) }
