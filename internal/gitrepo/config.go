package gitrepo

import (
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
)

type configValue struct {
	text string
	bare bool
}

func valueOf(text string) configValue { return configValue{text: text} }

type config map[string][]configValue

func (c config) add(section, name string, value configValue) {
	key := section + "." + name
	c[key] = append(c[key], value)
}

func (c config) last(key string) (configValue, bool) {
	values := c[key]
	if len(values) == 0 {
		return configValue{}, false
	}
	return values[len(values)-1], true
}

func (c config) boolean(key string) (bool, error) {
	result := false
	for _, value := range c[key] {
		parsed, err := value.boolean()
		if err != nil {
			return false, fmt.Errorf("bad boolean config value %q for %q", value.text, key)
		}
		result = parsed
	}
	return result, nil
}

func (v configValue) boolean() (bool, error) {
	if v.bare {
		return true, nil
	}
	return parseBool(v.text)
}

func parseBool(value string) (bool, error) {
	switch asciiLower(value) {
	case "true", "yes", "on":
		return true, nil
	case "false", "no", "off", "":
		return false, nil
	}
	number, ok := parseInt(value)
	if !ok {
		return false, fmt.Errorf("bad boolean value %q", value)
	}
	return number != 0, nil
}

func parseInt(value string) (int64, bool) {
	number, rest, ok := strtoimax(value)
	if !ok {
		return 0, false
	}
	var factor int64
	switch asciiLower(rest) {
	case "":
		factor = 1
	case "k":
		factor = 1 << 10
	case "m":
		factor = 1 << 20
	case "g":
		factor = 1 << 30
	default:
		return 0, false
	}
	const limit = math.MaxInt32
	if (number < 0 && (-limit-1)/factor > number) || (number > 0 && limit/factor < number) {
		return 0, false
	}
	return number * factor, true
}

func strtoimax(value string) (int64, string, bool) {
	i := 0
	for i < len(value) && isCSpace(value[i]) {
		i++
	}
	negative := false
	if i < len(value) && (value[i] == '+' || value[i] == '-') {
		negative = value[i] == '-'
		i++
	}
	base := int64(10)
	switch {
	case i+2 < len(value) && value[i] == '0' && toLower(value[i+1]) == 'x' && digitValue(value[i+2]) < 16:
		base = 16
		i += 2
	case i < len(value) && value[i] == '0':
		base = 8
	}
	start := i
	var number int64
	for ; i < len(value) && digitValue(value[i]) < base; i++ {
		number = min(number*base+digitValue(value[i]), 1<<40)
	}
	if i == start {
		return 0, value, false
	}
	if negative {
		number = -number
	}
	return number, value[i:], true
}

func digitValue(b byte) int64 {
	switch {
	case isDigit(b):
		return int64(b - '0')
	case 'a' <= toLower(b) && toLower(b) <= 'f':
		return int64(toLower(b)-'a') + 10
	}
	return 16
}

func isCSpace(b byte) bool { return b == ' ' || ('\t' <= b && b <= '\r') }

func isSpace(b byte) bool { return b == ' ' || b == '\t' || b == '\n' || b == '\r' }

func isKeyChar(b byte) bool { return isAlpha(b) || isDigit(b) || b == '-' }

func asciiLower(s string) string {
	lower := []byte(s)
	for i, b := range lower {
		lower[i] = toLower(b)
	}
	return string(lower)
}

func parseConfig(file string, data []byte, visit func(section, name string, value configValue) error) error {
	p := configParser{data: data}
	if !p.skipBOM() {
		return p.fail(file)
	}
	var name []byte
	baselen := 0
	comment := false
	for {
		c := p.next()
		switch {
		case c == '\n':
			if p.eof {
				return nil
			}
			comment = false
			continue
		case comment || isSpace(c):
			continue
		case c == '#' || c == ';':
			comment = true
			continue
		case c == '[':
			var ok bool
			if name, ok = p.baseVar(name[:0]); !ok || len(name) == 0 {
				return p.fail(file)
			}
			name = append(name, '.')
			baselen = len(name)
			continue
		case !isAlpha(c):
			return p.fail(file)
		}
		name = append(name[:baselen], toLower(c))
		for {
			c = p.next()
			if p.eof || !isKeyChar(c) {
				break
			}
			name = append(name, toLower(c))
		}
		for c == ' ' || c == '\t' {
			c = p.next()
		}
		value := configValue{bare: true}
		if c != '\n' {
			if c != '=' {
				return p.fail(file)
			}
			text, ok := p.value()
			if !ok {
				return p.fail(file)
			}
			value = valueOf(text)
		}
		key, _, _ := strings.Cut(string(name), "\x00")
		value.text, _, _ = strings.Cut(value.text, "\x00")
		dot := strings.LastIndexByte(key, '.')
		if dot < 0 {
			continue
		}
		if err := visit(key[:dot], key[dot+1:], value); err != nil {
			return err
		}
	}
}

type configParser struct {
	data []byte
	pos  int
	eof  bool
}

func (p *configParser) skipBOM() bool {
	const bom = "\xEF\xBB\xBF"
	matched := 0
	for matched < len(bom) && matched < len(p.data) && p.data[matched] == bom[matched] {
		matched++
	}
	p.pos = matched
	return matched == 0 || matched == len(bom)
}

func (p *configParser) next() byte {
	if p.pos >= len(p.data) {
		p.eof = true
		return '\n'
	}
	c := p.data[p.pos]
	p.pos++
	if c == '\r' && p.pos < len(p.data) && p.data[p.pos] == '\n' {
		p.pos++
		c = '\n'
	}
	return c
}

func (p *configParser) fail(file string) error {
	line := strings.Count(string(p.data[:max(p.pos-1, 0)]), "\n") + 1
	return fmt.Errorf("bad config line %d in file %s", line, file)
}

func (p *configParser) baseVar(name []byte) ([]byte, bool) {
	for {
		c := p.next()
		switch {
		case p.eof:
			return name, false
		case c == ']':
			return name, true
		case isSpace(c):
			return p.extendedBaseVar(name, c)
		case !isKeyChar(c) && c != '.':
			return name, false
		}
		name = append(name, toLower(c))
	}
}

func (p *configParser) extendedBaseVar(name []byte, c byte) ([]byte, bool) {
	for isSpace(c) {
		if c == '\n' {
			return name, false
		}
		c = p.next()
	}
	if c != '"' {
		return name, false
	}
	name = append(name, '.')
	for {
		c = p.next()
		switch c {
		case '\\':
			c = p.next()
		case '"':
			return name, p.next() == ']'
		}
		if c == '\n' {
			return name, false
		}
		name = append(name, c)
	}
}

func (p *configParser) value() (string, bool) {
	var value []byte
	quote, comment := false, false
	trim := 0
	for {
		c := p.next()
		switch {
		case c == '\n':
			if quote {
				return "", false
			}
			if trim > 0 {
				value = value[:trim]
			}
			return string(value), true
		case comment:
			continue
		case isSpace(c) && !quote:
			if trim == 0 {
				trim = len(value)
			}
			if len(value) > 0 {
				value = append(value, c)
			}
			continue
		case !quote && (c == ';' || c == '#'):
			comment = true
			continue
		}
		trim = 0
		switch c {
		case '\\':
			switch escaped := p.next(); escaped {
			case '\n':
			case 't':
				value = append(value, '\t')
			case 'b':
				value = append(value, '\b')
			case 'n':
				value = append(value, '\n')
			case '\\', '"':
				value = append(value, escaped)
			default:
				return "", false
			}
		case '"':
			quote = !quote
		default:
			value = append(value, c)
		}
	}
}

type repositoryFormat struct {
	worktreeConfig bool
	sha256         bool
}

func readRepositoryFormat(path string) (repositoryFormat, error) {
	data, err := readOptional(path)
	if err != nil {
		return repositoryFormat{}, err
	}
	settings := config{}
	err = parseConfig(path, data, func(section, name string, value configValue) error {
		settings.add(section, name, value)
		return nil
	})
	if err != nil {
		return repositoryFormat{}, err
	}
	version := int64(-1)
	for _, value := range settings["core.repositoryformatversion"] {
		var ok bool
		if version, ok = parseInt(value.text); value.bare || !ok {
			return repositoryFormat{}, fmt.Errorf("bad numeric config value %q for 'core.repositoryformatversion' in file %s", value.text, path)
		}
	}
	var format repositoryFormat
	if format.worktreeConfig, err = settings.boolean("extensions.worktreeconfig"); err != nil {
		return repositoryFormat{}, err
	}
	for _, value := range settings["extensions.objectformat"] {
		switch {
		case value.bare:
			return repositoryFormat{}, errors.New("missing value for 'extensions.objectformat'")
		case value.text == "sha1":
			format.sha256 = false
		case value.text == "sha256":
			format.sha256 = true
		default:
			return repositoryFormat{}, fmt.Errorf("invalid value for 'extensions.objectformat': %q", value.text)
		}
	}
	if version == -1 {
		return repositoryFormat{}, nil
	}
	return format, nil
}

type configParam struct {
	section, name string
	value         configValue
}

func commandConfig() ([]configParam, error) {
	params, err := countedConfig()
	if err != nil {
		return nil, err
	}
	if env, ok := os.LookupEnv("GIT_CONFIG_PARAMETERS"); ok {
		more, err := parseConfigParameters(env)
		if err != nil {
			return nil, err
		}
		params = append(params, more...)
	}
	return params, nil
}

func countedConfig() ([]configParam, error) {
	env, ok := os.LookupEnv("GIT_CONFIG_COUNT")
	if !ok {
		return nil, nil
	}
	count, err := parseCount(env)
	if err != nil {
		return nil, err
	}
	params := make([]configParam, 0, min(count, 64))
	for i := range count {
		keyName := "GIT_CONFIG_KEY_" + strconv.Itoa(i)
		key, ok := os.LookupEnv(keyName)
		if !ok {
			return nil, fmt.Errorf("missing config key %s", keyName)
		}
		valueName := "GIT_CONFIG_VALUE_" + strconv.Itoa(i)
		value, ok := os.LookupEnv(valueName)
		if !ok {
			return nil, fmt.Errorf("missing config value %s", valueName)
		}
		param, err := newConfigParam(key, valueOf(value))
		if err != nil {
			return nil, err
		}
		params = append(params, param)
	}
	return params, nil
}

func parseCount(env string) (int, error) {
	i := 0
	for i < len(env) && isCSpace(env[i]) {
		i++
	}
	negative := false
	if i < len(env) && (env[i] == '+' || env[i] == '-') {
		negative = env[i] == '-'
		i++
	}
	start := i
	var count uint64
	for ; i < len(env) && isDigit(env[i]); i++ {
		count = min(count*10+uint64(env[i]-'0'), math.MaxUint32)
	}
	if i == start {
		i = 0
	}
	if i < len(env) {
		return 0, errors.New("bogus count in GIT_CONFIG_COUNT")
	}
	if count > math.MaxInt32 || (negative && count != 0) {
		return 0, errors.New("too many entries in GIT_CONFIG_COUNT")
	}
	return int(count), nil
}

func newConfigParam(key string, value configValue) (configParam, error) {
	if key == "" {
		return configParam{}, errors.New("empty config key")
	}
	dot := strings.LastIndexByte(key, '.')
	if dot <= 0 {
		return configParam{}, fmt.Errorf("key does not contain a section: %s", key)
	}
	if dot == len(key)-1 {
		return configParam{}, fmt.Errorf("key does not contain variable name: %s", key)
	}
	section, subsection, nested := strings.Cut(key[:dot], ".")
	name := key[dot+1:]
	if !isAlpha(name[0]) || strings.IndexFunc(section+name, func(r rune) bool { return r > 0x7F || !isKeyChar(byte(r)) }) >= 0 {
		return configParam{}, fmt.Errorf("invalid key: %s", key)
	}
	if strings.Contains(subsection, "\n") {
		return configParam{}, fmt.Errorf("invalid key (newline): %s", key)
	}
	section = asciiLower(section)
	if nested {
		section += "." + subsection
	}
	return configParam{section: section, name: asciiLower(name), value: value}, nil
}

func parseConfigParameters(env string) ([]configParam, error) {
	bogus := errors.New("bogus format in GIT_CONFIG_PARAMETERS")
	var params []configParam
	rest, more := env, env != ""
	for more {
		var key string
		var ok bool
		if key, rest, more, ok = sqDequoteStep(rest); !ok {
			return nil, bogus
		}
		var param configParam
		var err error
		switch {
		case !more || isSpace(rest[0]):
			text, value, hasValue := strings.Cut(key, "=")
			if text == "" {
				return nil, fmt.Errorf("bogus config parameter: %s", key)
			}
			param, err = newConfigParam(text, configValue{text: value, bare: !hasValue})
		case rest[0] == '=':
			rest = rest[1:]
			value := configValue{bare: true}
			switch {
			case rest != "" && rest[0] == '\'':
				if value.text, rest, more, ok = sqDequoteStep(rest); !ok || (more && !isSpace(rest[0])) {
					return nil, bogus
				}
				value.bare = false
			case rest != "" && !isSpace(rest[0]):
				return nil, bogus
			default:
				more = rest != ""
			}
			param, err = newConfigParam(key, value)
		default:
			return nil, bogus
		}
		if err != nil {
			return nil, err
		}
		params = append(params, param)
		if more {
			rest = strings.TrimLeftFunc(rest, func(r rune) bool { return r < 0x80 && isSpace(byte(r)) })
			more = rest != ""
		}
	}
	return params, nil
}

func sqDequoteStep(arg string) (string, string, bool, bool) {
	if arg == "" || arg[0] != '\'' {
		return "", "", false, false
	}
	var out []byte
	for i := 1; ; {
		if i >= len(arg) {
			return "", "", false, false
		}
		if arg[i] != '\'' {
			out = append(out, arg[i])
			i++
			continue
		}
		i++
		switch {
		case i == len(arg):
			return string(out), "", false, true
		case arg[i] == '\\' && i+2 < len(arg) && (arg[i+1] == '\'' || arg[i+1] == '!') && arg[i+2] == '\'':
			out = append(out, arg[i+1])
			i += 3
		default:
			return string(out), arg[i:], true, true
		}
	}
}
