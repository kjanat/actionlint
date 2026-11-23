package output

import "github.com/fatih/color"

type Color int

const (
	// ColorAuto selects color automatically based on the terminal and NO_COLOR.
	ColorAuto Color = iota
	ColorAlways
	ColorNever
)

func colorStyle(enabled bool, attributes ...color.Attribute) *color.Color {
	style := new(color.Color).Add(attributes...)
	if enabled {
		style.EnableColor()
	} else {
		style.DisableColor()
	}
	return style
}
