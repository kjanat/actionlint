// Package scriptcoords maps engine universal-newline rows to decoded source
// rows and checks the file names sandboxed engines report findings in.
package scriptcoords

import checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"

// Starts maps each LF, CRLF or CR-delimited engine line to LF-delimited source.
func Starts(source string) []checkinputs.ScriptPosition {
	position := checkinputs.ScriptPosition{Row: 1, Column: 1}
	starts := []checkinputs.ScriptPosition{position}
	for i, character := range source {
		if character == '\n' {
			position.Row++
			position.Column = 1
			starts = append(starts, position)
		} else {
			position.Column++
			if character == '\r' && (i+1 == len(source) || source[i+1] != '\n') {
				starts = append(starts, position)
			}
		}
	}
	return starts
}

// SourcePosition accepts a position already validated against the engine input.
func SourcePosition(starts []checkinputs.ScriptPosition, position checkinputs.ScriptPosition) checkinputs.ScriptPosition {
	start := starts[position.Row-1]
	return checkinputs.ScriptPosition{Row: start.Row, Column: start.Column + position.Column - 1}
}
