package actionlint

import (
	"regexp"
	"slices"
	"strings"
)

// Only detect selection here. ShellCheck validates the directive and its value.
var shellcheckShellDirective = regexp.MustCompile(`^[\t ]*#[\t ]*shellcheck[\t ]+(?:[a-z-]+=(?:"[^"\r\n]*"|'[^'\r\n]*'|[^'"#\t \r\n][^#\t \r\n]*)[\t ]+)*shell=`)

func shellcheckHeader(src string) (header, body string, selectsShell bool) {
	end := 0
	for line := range strings.SplitAfterSeq(src, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			break
		}
		selectsShell = selectsShell || shellcheckShellDirective.MatchString(line)
		end += len(line)
	}
	return src[:end], src[end:], selectsShell
}

// sourceLines maps ShellCheck's generated input back to the unchanged run block.
// Zero denotes a generated directive or startup command, never workflow source.
type shellcheckScript struct {
	text        string
	sourceLines []int
	startupLine int
}

func prepareShellcheckScript(src, directives, setup string) shellcheckScript {
	header, body, _ := shellcheckHeader(src)
	var out strings.Builder
	var script shellcheckScript
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
	if setup != "" {
		out.WriteString(setup + "\n")
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

func (script shellcheckScript) originalLine(line int) int {
	if line == len(script.sourceLines)+1 {
		// Parser errors and exclusive end positions may point just past EOF.
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
