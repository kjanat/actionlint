package shell

import (
	"regexp"
	"slices"
	"strings"

	"actionlint.kjanat.dev/v2/internal/tools/profile"
	"actionlint.kjanat.dev/v2/internal/tools/profile/mask"
)

var shellDirective = regexp.MustCompile(`^[\t ]*#[\t ]*shellcheck[\t ]+(?:[a-z-]+=(?:"[^"\r\n]*"|'[^'\r\n]*'|[^'"#\t \r\n][^#\t \r\n]*)[\t ]+)*?shell=`)

// HeaderDialect is the dialect the first `# shellcheck shell=` directive of
// header selects; empty when an opened quote is not closed.
func HeaderDialect(header string) string {
	for line := range strings.SplitSeq(header, "\n") {
		selector := shellDirective.FindString(line)
		if selector == "" {
			continue
		}
		value := strings.TrimSuffix(line[len(selector):], "\r")
		if strings.HasPrefix(value, "'") || strings.HasPrefix(value, `"`) {
			dialect, _, closed := strings.Cut(value[1:], value[:1])
			if !closed {
				return ""
			}
			return dialect
		}
		dialect, _, _ := strings.Cut(value, " ")
		return dialect
	}
	return ""
}

// Header splits src after its leading comment and blank lines, where file-wide
// directives live, and reports whether a directive there selects the shell.
func Header(src string) (header, body string, selectsShell bool) {
	end := 0
	for line := range strings.SplitAfterSeq(src, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			break
		}
		selectsShell = selectsShell || shellDirective.MatchString(line)
		end += len(line)
	}
	return src[:end], src[end:], selectsShell
}

// Script is a run block prepared for a tool: generated directive and startup
// lines inserted where the tool honours them.
type Script struct {
	text string
	// sourceLines maps each prepared row to the run block row it holds; zero
	// denotes a generated directive or startup command, never workflow source.
	sourceLines []int
	startupLine int
}

// Prepare inserts directives after a leading shebang and the startup command
// after the leading comments of src. Every inserted row is mapped.
func Prepare(src, directives, startup string) Script {
	header, body, _ := Header(src)
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
	// A real shebang must stay on the first line for ShellCheck to recognize it.
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
		out.WriteString(startup)
		out.WriteByte('\n')
		script.sourceLines = append(script.sourceLines, 0)
		script.startupLine = len(script.sourceLines)
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

// Text is the prepared source.
func (script Script) Text() string { return script.text }

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

// Line maps prepared row line to the run block row it holds, or to the
// generated directive or startup line it is.
func (script Script) Line(line int) profile.Line {
	if original := script.OriginalLine(line); original != 0 {
		return profile.Line{Line: original, Origin: profile.LineScript}
	}
	if script.startupLine != 0 && line == script.startupLine {
		return profile.Line{Origin: profile.LineStartup}
	}
	return profile.Line{Origin: profile.LineDirective}
}

// Input prepares an action input's script, which runs no startup command: it
// masks templates, lets a leading `# shellcheck shell=` directive select the
// dialect over the declared one and inserts directives. It returns the
// selected dialect.
func Input(text, dialect, directives string) (profile.Prepared, string) {
	text = mask.Expressions(text)
	if header, _, selected := Header(text); selected {
		dialect = HeaderDialect(header)
	}
	if directives == "" {
		return profile.Prepared{Source: text}, dialect
	}
	prepared := Prepare(text, directives, "")
	return profile.Prepared{Source: prepared.Text(), LineMap: prepared.Line}, dialect
}
