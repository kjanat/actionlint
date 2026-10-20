package render

// Color is kind of colorful output behavior.
type Color int

const (
	// ColorAuto is kind to determine to colorize errors output automatically. It is
	// determined based on pty and $NO_COLOR environment variable. See document of fatih/color
	// for more details.
	ColorAuto Color = iota
	// ColorAlways is kind to always colorize errors output.
	ColorAlways
	// ColorNever is kind never to colorize errors output.
	ColorNever
)
