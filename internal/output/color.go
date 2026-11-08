package output

// Color selects whether diagnostic output uses color.
type Color int

const (
	// ColorAuto selects color automatically based on the terminal and NO_COLOR.
	// See the fatih/color documentation for details.
	ColorAuto Color = iota
	// ColorAlways enables color.
	ColorAlways
	// ColorNever disables color.
	ColorNever
)
