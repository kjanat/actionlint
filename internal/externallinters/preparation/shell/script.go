package shell

import (
	"slices"
	"strings"

	"actionlint.kjanat.dev/v2/internal/externallinters/preparation"
)

// Header splits source after its leading comments and blank lines.
func Header(src string) (header, body string) {
	end := 0
	for line := range strings.SplitAfterSeq(src, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			break
		}
		end += len(line)
	}
	return src[:end], src[end:]
}

type Script struct {
	text string
	// sourceLines maps each prepared row to its run block row. Zero
	// denotes a generated directive or startup command. Other values denote workflow source.
	sourceLines               []int
	startupFirst, startupLast int
}

// Prepare inserts directives after a leading shebang and the startup command
// after the leading comments of src. Every inserted row is mapped.
func Prepare(src, directives, startup string) Script {
	header, body := Header(src)
	var out strings.Builder
	var script Script
	original := 0
	writeSource := func(text string) {
		for line := range strings.SplitAfterSeq(text, "\n") {
			if line == "" {
				continue
			}
			original++
			script.sourceLines = append(script.sourceLines, original)
			out.WriteString(line)
			if !strings.HasSuffix(line, "\n") {
				out.WriteByte('\n')
			}
		}
	}
	// The shebang identifies the interpreter only when it is on the first line.
	if strings.HasPrefix(header, "#!") {
		line, rest, newline := strings.Cut(header, "\n")
		if newline {
			line += "\n"
		}
		writeSource(line)
		header = rest
	}
	out.WriteString(directives)
	for range strings.Count(directives, "\n") {
		script.sourceLines = append(script.sourceLines, 0)
	}
	writeSource(header)
	if startup != "" {
		script.startupFirst = len(script.sourceLines) + 1
		for line := range strings.SplitAfterSeq(startup, "\n") {
			if line == "" {
				continue
			}
			out.WriteString(line)
			if !strings.HasSuffix(line, "\n") {
				out.WriteByte('\n')
			}
			script.sourceLines = append(script.sourceLines, 0)
		}
		script.startupLast = len(script.sourceLines)
	}
	writeSource(body)
	// Keep the empty script parseable without attributing the blank line to YAML.
	if out.Len() == 0 {
		out.WriteByte('\n')
		script.sourceLines = append(script.sourceLines, 0)
	}
	script.text = out.String()
	return script
}

func (script Script) Text() string { return script.text }

// Substitute returns the prepared source with each script row taken from
// src, which has the rows of the source given to Prepare.
func (script Script) Substitute(src string) string {
	rows := strings.SplitAfter(src, "\n")
	var out strings.Builder
	for index, line := range strings.SplitAfter(script.text, "\n") {
		if index >= len(script.sourceLines) || script.sourceLines[index] == 0 {
			out.WriteString(line)
			continue
		}
		row := rows[script.sourceLines[index]-1]
		out.WriteString(row)
		if !strings.HasSuffix(row, "\n") {
			out.WriteByte('\n')
		}
	}
	return out.String()
}

// OriginalLine is the run block row that prepared row line holds, or zero for
// a generated row. The row just past the end maps past the last source row,
// where parser errors and exclusive ends may point.
func (script Script) OriginalLine(line int) int {
	if line == len(script.sourceLines)+1 {
		for _, source := range slices.Backward(script.sourceLines) {
			if source != 0 {
				return source + 1
			}
		}
	}
	if line < 1 || line > len(script.sourceLines) {
		return 0
	}
	return script.sourceLines[line-1]
}

func (script Script) Line(line int) preparation.Line {
	if original := script.OriginalLine(line); original != 0 {
		return preparation.Line{Line: original, Origin: preparation.LineScript}
	}
	if script.startupFirst != 0 && line >= script.startupFirst && line <= script.startupLast {
		return preparation.Line{Origin: preparation.LineStartup}
	}
	return preparation.Line{Origin: preparation.LineDirective}
}
