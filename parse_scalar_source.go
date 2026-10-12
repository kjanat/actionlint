package actionlint

import (
	"strings"
	"unicode/utf8"

	"go.yaml.in/yaml/v4"
)

// scalarSourceBuilder only publishes mappings when the decoded bytes match the
// parser's value. YAML folding introduces bytes with no corresponding source token.
type scalarSourceBuilder struct {
	source *scriptSource
	offset int
}

func (b *scalarSourceBuilder) append(value string, line, column, width int) bool {
	if !strings.HasPrefix(b.source.value[b.offset:], value) {
		return false
	}
	if line != 0 && !strings.ContainsAny(value, "\r\n") && value != "" {
		previous := len(b.source.spans) - 1
		if !b.source.mapBytes(b.offset, len(value), line, column) {
			return false
		}
		if width != 0 {
			b.source.spans[len(b.source.spans)-1].yamlEndColumn = column + width
		} else if previous >= 0 {
			prior := &b.source.spans[previous]
			if prior.yamlEndColumn == 0 && prior.end == b.offset && prior.yamlLine == line {
				prior.end += len(value)
				b.source.spans = b.source.spans[:previous+1]
			}
		}
	}
	b.offset += len(value)
	return true
}

func (p *parser) quotedScriptSource(n *yaml.Node) *scriptSource {
	if n.Line <= 0 || n.Line > len(p.sourceLines) {
		return nil
	}
	start, ok := byteOffsetAtColumn(p.sourceLines[n.Line-1], n.Column)
	if !ok || start >= len(p.sourceLines[n.Line-1]) {
		return nil
	}
	quote := byte('\'')
	if n.Style == yaml.DoubleQuotedStyle {
		quote = '"'
	}
	if p.sourceLines[n.Line-1][start] != quote {
		return nil
	}
	b := scalarSourceBuilder{source: newScriptSource(n.Value)}
	start++
	for row := n.Line - 1; row < len(p.sourceLines); row++ {
		raw := p.sourceLines[row]
		end := len(strings.TrimRight(raw, " \t"))
		if quote == '"' && end < len(raw) {
			backslashes := 0
			for i := end - 1; i >= start && raw[i] == '\\'; i-- {
				backslashes++
			}
			if backslashes%2 != 0 {
				end++ // The first space is escaped; subsequent trailing spaces fold.
			}
		}
		continued := false
		column := utf8.RuneCountInString(raw[:start]) + 1
		for i := start; i < end; {
			if raw[i] == quote {
				if quote == '\'' && i+1 < end && raw[i+1] == quote {
					if !b.append("'", row+1, column, 2) {
						return nil
					}
					i += 2
					column += 2
					continue
				}
				if b.offset == len(n.Value) {
					return b.source
				}
				return nil
			}
			if quote == '"' && raw[i] == '\\' {
				if i+1 == end {
					continued = true
					break
				}
				size := 2
				switch raw[i+1] {
				case 'x':
					size = 4
				case 'u':
					size = 6
				case 'U':
					size = 10
				}
				if i+size > end {
					return nil
				}
				var decoded string
				if err := yaml.Unmarshal([]byte("\""+raw[i:i+size]+"\""), &decoded); err != nil || !b.append(decoded, row+1, column, size) {
					return nil
				}
				i += size
				column += size
				continue
			}
			_, size := utf8.DecodeRuneInString(raw[i:])
			if !b.append(raw[i:i+size], row+1, column, 0) {
				return nil
			}
			i += size
			column++
		}
		next := row + 1
		for next < len(p.sourceLines) && strings.Trim(p.sourceLines[next], " \t") == "" {
			next++
		}
		if next == len(p.sourceLines) {
			return nil
		}
		breaks := next - row - 1
		fold := strings.Repeat("\n", breaks)
		if breaks == 0 && !continued {
			fold = " "
		}
		if !b.append(fold, 0, 0, 0) {
			return nil
		}
		row = next - 1
		start = len(p.sourceLines[next]) - len(strings.TrimLeft(p.sourceLines[next], " \t"))
	}
	return nil
}

func (p *parser) foldedScriptSource(n *yaml.Node) *scriptSource {
	if n.Line <= 0 || n.Line >= len(p.sourceLines) {
		return nil
	}
	first := n.Line
	for first < len(p.sourceLines) && strings.Trim(p.sourceLines[first], " ") == "" {
		first++
	}
	if first == len(p.sourceLines) {
		return nil
	}
	decoded := strings.TrimLeft(n.Value, "\n")
	indent := len(p.sourceLines[first]) - len(strings.TrimLeft(p.sourceLines[first], " "))
	indent -= len(decoded) - len(strings.TrimLeft(decoded, " "))
	if indent <= 0 {
		return nil
	}
	b := scalarSourceBuilder{source: newScriptSource(n.Value)}
	if !b.append(strings.Repeat("\n", first-n.Line), 0, 0, 0) {
		return nil
	}
	for row := first; row < len(p.sourceLines); row++ {
		raw := p.sourceLines[row]
		if len(raw) < indent || strings.Trim(raw[:indent], " ") != "" {
			break
		}
		content := raw[indent:]
		if !b.append(content, row+1, indent+1, 0) {
			return nil
		}
		next := row + 1
		for next < len(p.sourceLines) && strings.Trim(p.sourceLines[next], " ") == "" {
			next++
		}
		if next == len(p.sourceLines) || len(p.sourceLines[next]) < indent || strings.Trim(p.sourceLines[next][:indent], " ") != "" {
			break
		}
		breaks := next - row - 1
		fold := strings.Repeat("\n", breaks)
		if strings.HasPrefix(content, " ") || strings.HasPrefix(content, "\t") || strings.HasPrefix(p.sourceLines[next][indent:], " ") || strings.HasPrefix(p.sourceLines[next][indent:], "\t") {
			fold += "\n"
		} else if breaks == 0 {
			fold = " "
		}
		if !b.append(fold, 0, 0, 0) {
			return nil
		}
		row = next - 1
	}
	if strings.Trim(b.source.value[b.offset:], "\n") != "" {
		return nil
	}
	return b.source
}
