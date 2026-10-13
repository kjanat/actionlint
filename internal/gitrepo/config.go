package gitrepo

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
)

type config map[string]string

func parseConfig(data []byte, visit func(section, name, value string) error) error {
	data = bytes.TrimPrefix(data, []byte("\xEF\xBB\xBF"))
	p := configParser{data: data}
	section := ""
	for !p.done() {
		p.skipSpace()
		if p.done() {
			break
		}
		switch b := p.peek(); {
		case b == '\n':
			p.pos++
		case b == '#' || b == ';':
			p.skipLine()
		case b == '[':
			name, ok := p.section()
			if ok {
				section = name
			}
			p.skipLine()
		case isAlpha(b):
			name, value, ok := p.variable()
			if !ok {
				p.skipLine()
				continue
			}
			if section == "" {
				continue
			}
			if err := visit(section, name, value); err != nil {
				return err
			}
		default:
			p.skipLine()
		}
	}
	return nil
}

func (c config) boolean(key string) (bool, error) {
	value, ok := c[key]
	if !ok {
		return false, nil
	}
	return parseBool(value)
}

func parseBool(value string) (bool, error) {
	switch strings.ToLower(value) {
	case "true", "yes", "on":
		return true, nil
	case "false", "no", "off", "":
		return false, nil
	}
	number, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return false, fmt.Errorf("bad boolean config value %q", value)
	}
	return number != 0, nil
}

type configParser struct {
	data []byte
	pos  int
}

func (p *configParser) done() bool { return p.pos >= len(p.data) }

func (p *configParser) peek() byte { return p.data[p.pos] }

func (p *configParser) skipSpace() {
	for !p.done() && (p.peek() == ' ' || p.peek() == '\t' || p.peek() == '\r') {
		p.pos++
	}
}

func (p *configParser) skipToLineEnd() {
	for !p.done() && p.peek() != '\n' {
		p.pos++
	}
}

func (p *configParser) skipLine() {
	p.skipToLineEnd()
	if !p.done() {
		p.pos++
	}
}

func (p *configParser) section() (string, bool) {
	p.pos++
	var name []byte
	for !p.done() {
		b := p.peek()
		if !isAlpha(b) && !isDigit(b) && b != '-' && b != '.' {
			break
		}
		name = append(name, toLower(b))
		p.pos++
	}
	if len(name) == 0 {
		return "", false
	}
	p.skipSpace()
	if p.done() {
		return "", false
	}
	switch p.peek() {
	case ']':
		p.pos++
		return string(name), true
	case '"':
		p.pos++
		var sub []byte
		for {
			if p.done() || p.peek() == '\n' {
				return "", false
			}
			b := p.peek()
			p.pos++
			switch b {
			case '"':
				p.skipSpace()
				if p.done() || p.peek() != ']' {
					return "", false
				}
				p.pos++
				return string(name) + "." + string(sub), true
			case '\\':
				if p.done() {
					return "", false
				}
				sub = append(sub, p.peek())
				p.pos++
			default:
				sub = append(sub, b)
			}
		}
	}
	return "", false
}

func (p *configParser) variable() (string, string, bool) {
	var name []byte
	for !p.done() {
		b := p.peek()
		if !isAlpha(b) && !isDigit(b) && b != '-' {
			break
		}
		name = append(name, toLower(b))
		p.pos++
	}
	p.skipSpace()
	if p.done() || p.peek() == '\n' || p.peek() == '#' || p.peek() == ';' {
		p.skipToLineEnd()
		return string(name), "true", true
	}
	if p.peek() != '=' {
		return "", "", false
	}
	p.pos++
	p.skipSpace()
	var value, pending []byte
	quoted := false
	for !p.done() && p.peek() != '\n' {
		b := p.peek()
		p.pos++
		switch {
		case b == '"':
			quoted = !quoted
			value = append(value, pending...)
			pending = pending[:0]
		case b == '\\':
			if p.done() {
				return "", "", false
			}
			escaped := p.peek()
			p.pos++
			value = append(value, pending...)
			pending = pending[:0]
			switch escaped {
			case 'n':
				value = append(value, '\n')
			case 't':
				value = append(value, '\t')
			case 'b':
				value = append(value, '\b')
			case '\n':
			case '\r':
				if !p.done() && p.peek() == '\n' {
					p.pos++
				}
			default:
				value = append(value, escaped)
			}
		case !quoted && (b == '#' || b == ';'):
			p.skipToLineEnd()
			return string(name), string(value), true
		case !quoted && (b == ' ' || b == '\t' || b == '\r'):
			pending = append(pending, b)
		default:
			value = append(value, pending...)
			pending = pending[:0]
			value = append(value, b)
		}
	}
	if quoted {
		return "", "", false
	}
	return string(name), string(value), true
}
