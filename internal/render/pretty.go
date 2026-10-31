package render

import (
	"fmt"
	"io"
	"strings"
	"unicode"

	"github.com/fatih/color"
	"github.com/mattn/go-runewidth"
)

func (f Finding) printPretty(out io.Writer, width int) error {
	var b strings.Builder
	d := f.Diagnostic
	severity := d.Severity
	if severity == "" {
		severity = "error"
	}
	style := new(color.Color).Add(color.Bold, color.FgRed)
	switch severity {
	case "warning", "warn":
		style = new(color.Color).Add(color.Bold, color.FgYellow)
	case "info", "note", "hint":
		style = new(color.Color).Add(color.Bold, color.FgCyan)
	}
	label := severity
	if d.Rule != "" {
		label += "[" + d.Rule + "]"
	}
	writeWrapped(&b, "", label, width, style)
	writeWrapped(&b, "  ", d.Message, width, nil)
	writeWrapped(&b, "  --> ", fmt.Sprintf("%s:%d:%d", d.Path, d.Start.Line, d.Start.Column), width, nil)
	if line, ok := f.line(); ok && d.Start.Line > 0 {
		gutter := fmt.Sprintf("%d | ", d.Start.Line)
		space := strings.Repeat(" ", len(gutter)-2)
		line, start, length := f.prettySource(line, width-len(gutter))
		fmt.Fprintf(&b, "%s|\n%s%s\n", space, gutter, line)
		if start >= 0 {
			fmt.Fprintf(&b, "%s| %s%s\n", space, strings.Repeat(" ", start), style.Sprint(strings.Repeat("^", length)))
		}
		if d.End.Line > d.Start.Line {
			writeWrapped(&b, "  = ", fmt.Sprintf("range continues to %d:%d", d.End.Line, d.End.Column), width, nil)
		}
	}
	for _, item := range []struct{ label, text string }{
		{"note", d.Explanation}, {"help", d.Help}, {"docs", d.DocumentationURL},
	} {
		if item.text != "" {
			writeWrapped(&b, "  = ", item.label+": "+item.text, width, nil)
		}
	}
	for _, related := range d.Related {
		writeWrapped(&b, "  = ", "related: "+related.Message, width, nil)
		writeWrapped(&b, "    --> ", fmt.Sprintf("%s:%d:%d", related.Path, related.Start.Line, related.Start.Column), width, nil)
	}
	for _, fix := range d.Fixes {
		writeWrapped(&b, "  = ", "fix: "+fix.Description, width, nil)
	}
	b.WriteByte('\n')
	_, err := io.WriteString(out, b.String())
	return err
}

// terminalText prevents source and plugin strings from emitting terminal controls.
func terminalText(s string) string {
	return strings.Map(func(r rune) rune {
		if r != '\n' && r != '\t' && unicode.IsControl(r) {
			return '\uFFFD'
		}
		return r
	}, s)
}

func writeWrapped(b *strings.Builder, prefix, text string, width int, style *color.Color) {
	available := max(1, width-runewidth.StringWidth(prefix))
	for paragraph := range strings.SplitSeq(terminalText(text), "\n") {
		line := ""
		flush := func() {
			b.WriteString(prefix)
			if style == nil {
				b.WriteString(line)
			} else {
				b.WriteString(style.Sprint(line))
			}
			b.WriteByte('\n')
			line = ""
		}
		for word := range strings.FieldsSeq(paragraph) {
			if line != "" {
				if runewidth.StringWidth(line)+1+runewidth.StringWidth(word) > available {
					flush()
				} else {
					line += " "
				}
			}
			for _, r := range word {
				if runewidth.StringWidth(line)+runewidth.RuneWidth(r) > available {
					flush()
				}
				line += string(r)
			}
		}
		flush()
	}
}

func (f Finding) prettySource(line string, width int) (string, int, int) {
	var expanded strings.Builder
	start, end, cells := -1, -1, 0
	last := f.end(line)
	for i, r := range []rune(line) {
		if i+1 == f.Diagnostic.Start.Column {
			start = cells
		}
		if r == '\t' {
			n := 4 - cells%4
			expanded.WriteString(strings.Repeat(" ", n))
			cells += n
		} else {
			if unicode.IsControl(r) {
				r = '\uFFFD'
			}
			expanded.WriteRune(r)
			cells += runewidth.RuneWidth(r)
		}
		if i+1 == last {
			end = cells
		}
	}
	if f.Diagnostic.Start.Column == len([]rune(line))+1 {
		start = cells
	}
	if end < start {
		end = start + 1
	}
	text := expanded.String()
	left := max(0, start-width/3)
	skipped, offset := 0, 0
	for i, r := range text {
		if skipped >= left {
			offset = i
			break
		}
		skipped += runewidth.RuneWidth(r)
	}
	if skipped > 0 {
		text = "..." + text[offset:]
		start += 3 - skipped
		end += 3 - skipped
	}
	return runewidth.Truncate(text, width, "..."), start, max(1, min(end-start, width-start))
}
