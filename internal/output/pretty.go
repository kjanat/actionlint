package output

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode"

	"actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/results"

	"github.com/fatih/color"
	"github.com/mattn/go-runewidth"
)

func printPretty(out io.Writer, f results.PublishedDiagnostic, width int, hyperlinks, colored bool, configPath string) error {
	var b strings.Builder
	d := f.Diagnostic
	severity := d.Severity
	if severity == "" {
		severity = "error"
	}
	style := colorStyle(colored, color.Bold, color.FgRed)
	switch severity {
	case "warning", "warn":
		style = colorStyle(colored, color.Bold, color.FgYellow)
	case "info", "note", "hint":
		style = colorStyle(colored, color.Bold, color.FgCyan)
	}
	label := severity
	if d.Rule != "" {
		label += "[" + d.Rule + "]"
	}
	if d.Code != "" && d.Code != d.Rule {
		label += " " + d.Code
	}
	writeWrapped(&b, "", label, width, style, hyperlinks)
	writeWrapped(&b, "  ", d.Message, width, nil, hyperlinks)
	if d.NonBlocking {
		writeWrapped(&b, "  = ", "This dependency finding does not affect the exit code. Use lint.dependencies to make it count.", width, nil, hyperlinks)
	}
	writePrettyLocation(&b, "  --> ", d.Path, d.Start, width, hyperlinks)
	if f.Excerpt.Kind != results.ExcerptAbsent {
		gutter := fmt.Sprintf("%d | ", d.Start.Line)
		space := strings.Repeat(" ", len(gutter)-2)
		line, start, length := prettySource(f, width-len(gutter))
		fmt.Fprintf(&b, "%s|\n%s%s\n", space, gutter, line)
		if start >= 0 {
			fmt.Fprintf(&b, "%s| %s%s\n", space, strings.Repeat(" ", start), style.Sprint(strings.Repeat("^", length)))
		}
		if d.End.Line > d.Start.Line {
			writeWrapped(&b, "  = ", fmt.Sprintf("range continues to %d:%d", d.End.Line, d.End.Column), width, nil, hyperlinks)
		}
	}
	for _, item := range []struct{ label, text string }{
		{"note", d.Explanation}, {"help", d.Help}, {"config", configPath}, {"docs", d.DocumentationURL},
	} {
		if item.text != "" {
			writeWrapped(&b, "  = ", item.label+": "+item.text, width, nil, hyperlinks)
		}
	}
	if lifecycle := d.Lifecycle; lifecycle != nil {
		event := "lifecycle: " + lifecycle.Event
		if lifecycle.EffectiveOn != "" {
			event += " on " + lifecycle.EffectiveOn
		}
		if lifecycle.Source != "" {
			event += " (" + lifecycle.Source + ")"
		}
		writeWrapped(&b, "  = ", event, width, nil, hyperlinks)
		for _, policy := range lifecycle.Policies {
			status := "optional"
			if policy.Enforced {
				status = "required"
			}
			text := "policy: " + policy.ID + " (" + status + ")"
			if policy.StartsOn != "" {
				text += ", starts " + policy.StartsOn
			}
			if policy.Origin != "" {
				text += ", from " + policy.Origin
			}
			writeWrapped(&b, "  = ", text, width, nil, hyperlinks)
		}
	}
	for _, related := range d.Related {
		writeWrapped(&b, "  = ", "related: "+related.Message, width, nil, hyperlinks)
		writePrettyLocation(&b, "    --> ", related.Path, related.Start, width, hyperlinks)
	}
	for _, fix := range d.Fixes {
		writeWrapped(&b, "  = ", "fix: "+fix.Description, width, nil, hyperlinks)
	}
	b.WriteByte('\n')
	_, err := io.WriteString(out, b.String())
	return err
}

func writePrettyLocation(b *strings.Builder, prefix, path string, position source.Position, width int, hyperlinks bool) {
	path = terminalText(path)
	if links := terminalURLs(path); len(links) == 1 && links[0].start == 0 && links[0].end == len(path) {
		writeRemoteLocation(b, prefix, path, position, hyperlinks)
		return
	}
	writeWrapped(b, prefix, fmt.Sprintf("%s:%d:%d", path, position.Line, position.Column), width, nil, hyperlinks)
}

// writeRemoteLocation writes a URL location on one line so the URL is never
// split. The visible text names the line and column; the link target adds a
// #L<line> anchor, which GitHub blob pages scroll to.
func writeRemoteLocation(b *strings.Builder, prefix, address string, position source.Position, hyperlinks bool) {
	b.WriteString(prefix)
	if hyperlinks {
		writeTerminalLinkStart(b, remoteLocationTarget(address, position.Line))
		b.WriteString(address)
		writeTerminalLinkEnd(b)
	} else {
		b.WriteString(address)
	}
	fmt.Fprintf(b, " (line %d, column %d)\n", position.Line, position.Column)
}

// remoteLocationTarget replaces any fragment of address with a #L<line> anchor.
// Locations without a line keep address unchanged.
func remoteLocationTarget(address string, line int) string {
	if line <= 0 {
		return address
	}
	base, _, _ := strings.Cut(address, "#")
	return base + "#L" + strconv.Itoa(line)
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

func writeWrapped(b *strings.Builder, prefix, text string, width int, style *color.Color, hyperlinks bool) {
	available := max(1, width-runewidth.StringWidth(prefix))
	for paragraph := range strings.SplitSeq(terminalText(text), "\n") {
		line := ""
		var rendered strings.Builder
		activeURL := ""
		selectURL := func(target string) {
			if target == activeURL {
				return
			}
			if activeURL != "" {
				writeTerminalLinkEnd(&rendered)
			}
			if target != "" {
				writeTerminalLinkStart(&rendered, target)
			}
			activeURL = target
		}
		flush := func() {
			selectURL("")
			b.WriteString(prefix)
			if style == nil {
				b.WriteString(rendered.String())
			} else {
				b.WriteString(style.Sprint(rendered.String()))
			}
			b.WriteByte('\n')
			line = ""
			rendered.Reset()
		}
		for word := range strings.FieldsSeq(paragraph) {
			if line != "" {
				if runewidth.StringWidth(line)+1+runewidth.StringWidth(word) > available {
					flush()
				} else {
					selectURL("")
					line += " "
					rendered.WriteByte(' ')
				}
			}
			links := terminalURLs(word)
			for offset, r := range word {
				if len(links) == 0 && runewidth.StringWidth(line)+runewidth.RuneWidth(r) > available {
					flush()
				}
				target := ""
				for _, link := range links {
					if offset >= link.start && offset < link.end {
						target = link.target
						break
					}
				}
				if hyperlinks {
					selectURL(target)
				}
				line += string(r)
				rendered.WriteRune(r)
			}
		}
		flush()
	}
}

// prettySource expands the excerpt line for display within width cells and
// returns it with the cell offset and length of the indicated range.
func prettySource(f results.PublishedDiagnostic, width int) (string, int, int) {
	var expanded strings.Builder
	start, end, cells := -1, -1, 0
	line, last := f.Excerpt.Line, f.Excerpt.EndColumn
	for i, r := range []rune(line) {
		if i+1 == f.Start.Column {
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
	if f.Start.Column == len([]rune(line))+1 {
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
