// Package scriptcoords maps engine universal-newline rows to decoded source
// rows and checks the file names sandboxed engines report findings in.
package scriptcoords

import "actionlint.kjanat.dev/v2/analyzer"

// Starts maps each LF, CRLF or CR-delimited engine line to LF-delimited source.
func Starts(source string) []analyzer.ScriptPosition {
	position := analyzer.ScriptPosition{Row: 1, Column: 1}
	starts := []analyzer.ScriptPosition{position}
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
func SourcePosition(starts []analyzer.ScriptPosition, position analyzer.ScriptPosition) analyzer.ScriptPosition {
	start := starts[position.Row-1]
	return analyzer.ScriptPosition{Row: start.Row, Column: start.Column + position.Column - 1}
}
