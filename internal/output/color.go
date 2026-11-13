package output

import "github.com/fatih/color"

// Color selects whether diagnostic output uses color.
type Color int

const (
	// ColorAuto selects color automatically based on the terminal and NO_COLOR.
	ColorAuto Color = iota
	// ColorAlways enables color.
	ColorAlways
	// ColorNever disables color.
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
