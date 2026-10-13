package gitrepo

import (
	"bytes"
	"slices"
)

type patternMatch int

const (
	patternNone patternMatch = iota
	patternPositive
	patternNegative
)

type patternList struct {
	patterns   []ignorePattern
	ignoreCase bool
}

type ignorePattern struct {
	negated bool
	dirOnly bool
	matcher patternMatcher
}

func newPatternList(ignoreCase bool) *patternList {
	return &patternList{ignoreCase: ignoreCase}
}

func (l *patternList) addBuffer(buffer []byte) {
	buffer = bytes.TrimPrefix(buffer, []byte("\xEF\xBB\xBF"))
	for line := range bytes.SplitSeq(buffer, []byte("\n")) {
		l.addLine(line)
	}
}

func (l *patternList) addLine(line []byte) {
	line = bytes.TrimSuffix(line, []byte("\r"))
	if len(line) > 0 && line[0] == '#' {
		return
	}
	line = trimTrailingSpaces(line)
	if len(line) == 0 {
		return
	}
	body, negated := bytes.CutPrefix(line, []byte("!"))
	body, dirOnly := bytes.CutSuffix(body, []byte("/"))
	matcher := parseMatcher(body, l.ignoreCase)
	if matcher == nil {
		matcher = matchNever{}
	}
	l.patterns = append(l.patterns, ignorePattern{negated: negated, dirOnly: dirOnly, matcher: matcher})
}

func (l *patternList) matched(path []byte, isDir bool) patternMatch {
	if len(path) == 0 {
		return patternNone
	}
	name := path
	if index := bytes.LastIndexByte(path, '/'); index >= 0 {
		name = path[index+1:]
	}
	for _, pattern := range slices.Backward(l.patterns) {
		if pattern.dirOnly && !isDir {
			continue
		}
		if pattern.matcher.matches(path, name, l.ignoreCase) {
			if pattern.negated {
				return patternNegative
			}
			return patternPositive
		}
	}
	return patternNone
}

func trimTrailingSpaces(line []byte) []byte {
	end := len(line)
	for end > 0 && line[end-1] == ' ' {
		backslashes := 0
		for i := end - 2; i >= 0 && line[i] == '\\'; i-- {
			backslashes++
		}
		if backslashes%2 == 1 {
			break
		}
		end--
	}
	return line[:end]
}

type patternMatcher interface {
	matches(path, name []byte, fold bool) bool
}

type matchNever struct{}

func (matchNever) matches([]byte, []byte, bool) bool { return false }

type matchName struct{ component component }

func (m matchName) matches(_, name []byte, fold bool) bool { return m.component.matches(name, fold) }

type matchPath struct{ matcher pathMatcher }

func (m matchPath) matches(path, _ []byte, fold bool) bool { return m.matcher.matches(path, fold) }

func parseMatcher(body []byte, fold bool) patternMatcher {
	body, anchored := bytes.CutPrefix(body, []byte("/"))
	if !anchored {
		anchored = bytes.IndexByte(body, '/') >= 0
	}
	if len(body) == 0 {
		return nil
	}
	tokens, ok := tokenize(body, fold)
	if !ok {
		return nil
	}
	if !anchored {
		return matchName{newComponent(tokens)}
	}
	var segments []segment
	for _, part := range splitOnSlash(tokens) {
		switch {
		case len(part) == 1 && part[0].kind == rawDoubleStar:
			segments = append(segments, segment{recursive: true})
		case len(part) == 1 && part[0].kind == rawDoubleStarBeforeEscapedSlash:
			segments = append(segments, segment{component: matchAnything{}}, segment{recursive: true})
		default:
			segments = append(segments, segment{component: newComponent(part)})
		}
	}
	if len(segments) == 2 && segments[0].recursive && !segments[1].recursive {
		return matchName{segments[1].component}
	}
	return matchPath{newPathMatcher(segments)}
}

func splitOnSlash(tokens []rawToken) [][]rawToken {
	var parts [][]rawToken
	start := 0
	for i, token := range tokens {
		if token.kind == rawSlash {
			parts = append(parts, tokens[start:i])
			start = i + 1
		}
	}
	return append(parts, tokens[start:])
}

type segment struct {
	recursive bool
	component component
}

type pathMatcher interface {
	matches(path []byte, fold bool) bool
}

type pathLiteral []byte

func (m pathLiteral) matches(path []byte, fold bool) bool { return eqBytes(m, path, fold) }

type pathUnder []byte

func (m pathUnder) matches(path []byte, fold bool) bool {
	return len(path) > len(m) && eqBytes(m, path[:len(m)], fold)
}

type pathSegments []segment

func (m pathSegments) matches(path []byte, fold bool) bool { return matchSegments(m, path, fold) }

func newPathMatcher(segments []segment) pathMatcher {
	recursiveTail := len(segments) > 0 && segments[len(segments)-1].recursive
	init := segments
	if recursiveTail {
		init = segments[:len(segments)-1]
	}
	var literal []byte
	for i, seg := range init {
		component, ok := seg.component.(matchLiteral)
		if seg.recursive || !ok {
			return pathSegments(segments)
		}
		if i > 0 {
			literal = append(literal, '/')
		}
		literal = append(literal, component...)
	}
	if !recursiveTail {
		return pathLiteral(literal)
	}
	if len(init) > 0 {
		literal = append(literal, '/')
	}
	return pathUnder(literal)
}

func matchSegments(segments []segment, path []byte, fold bool) bool {
	end := len(path)
	index, position := 0, 0
	resumeIndex, resumePosition, resume := 0, 0, false
	for {
		switch {
		case index < len(segments) && segments[index].recursive && index+1 == len(segments):
			return position < end
		case index < len(segments) && segments[index].recursive:
			index++
			resumeIndex, resumePosition, resume = index, position, true
			continue
		case index < len(segments) && position < end:
			text, next := componentAt(path, position)
			if segments[index].component.matches(text, fold) {
				index++
				position = next
				continue
			}
		case index == len(segments) && position >= end:
			return true
		}
		if !resume || resumePosition >= end {
			return false
		}
		_, next := componentAt(path, resumePosition)
		resumePosition = next
		index = resumeIndex
		position = next
	}
}

func componentAt(path []byte, position int) ([]byte, int) {
	rest := path[position:]
	if length := bytes.IndexByte(rest, '/'); length >= 0 {
		return rest[:length], position + length + 1
	}
	return rest, len(path)
}

type component interface {
	matches(text []byte, fold bool) bool
}

type matchLiteral []byte

func (m matchLiteral) matches(text []byte, fold bool) bool { return eqBytes(m, text, fold) }

type matchPrefix []byte

func (m matchPrefix) matches(text []byte, fold bool) bool {
	return len(text) >= len(m) && eqBytes(m, text[:len(m)], fold)
}

type matchSuffix []byte

func (m matchSuffix) matches(text []byte, fold bool) bool {
	return len(text) >= len(m) && eqBytes(m, text[len(text)-len(m):], fold)
}

type matchAnything struct{}

func (matchAnything) matches([]byte, bool) bool { return true }

type matchGlob []token

func (m matchGlob) matches(text []byte, fold bool) bool { return matchGlobTokens(m, text, fold) }

func newComponent(raw []rawToken) component {
	tokens := make([]token, 0, len(raw))
	for _, t := range raw {
		switch t.kind {
		case rawByte:
			tokens = append(tokens, token{kind: tokenByte, b: t.b})
		case rawAnyByte:
			tokens = append(tokens, token{kind: tokenAnyByte})
		case rawStar, rawDoubleStar, rawDoubleStarBeforeEscapedSlash:
			tokens = append(tokens, token{kind: tokenStar})
		case rawClass:
			tokens = append(tokens, token{kind: tokenClass, set: t.set})
		case rawSlash:
			tokens = append(tokens, token{kind: tokenByte, b: '/'})
		}
	}
	literal := func(tokens []token) ([]byte, bool) {
		out := make([]byte, 0, len(tokens))
		for _, t := range tokens {
			if t.kind != tokenByte {
				return nil, false
			}
			out = append(out, t.b)
		}
		return out, true
	}
	switch {
	case len(tokens) == 1 && tokens[0].kind == tokenStar:
		return matchAnything{}
	case len(tokens) > 1 && tokens[0].kind == tokenStar:
		if rest, ok := literal(tokens[1:]); ok {
			return matchSuffix(rest)
		}
	case len(tokens) > 1 && tokens[len(tokens)-1].kind == tokenStar:
		if rest, ok := literal(tokens[:len(tokens)-1]); ok {
			return matchPrefix(rest)
		}
	}
	if bytes, ok := literal(tokens); ok {
		return matchLiteral(bytes)
	}
	return matchGlob(tokens)
}

type tokenKind int

const (
	tokenByte tokenKind = iota
	tokenAnyByte
	tokenStar
	tokenClass
)

type token struct {
	kind tokenKind
	b    byte
	set  *byteSet
}

func (t token) matches(b byte, fold bool) bool {
	switch t.kind {
	case tokenByte:
		return eqByte(t.b, b, fold)
	case tokenAnyByte:
		return true
	case tokenClass:
		return t.set.contains(b)
	}
	return false
}

func matchGlobTokens(tokens []token, text []byte, fold bool) bool {
	index, position := 0, 0
	resumeIndex, resumePosition, resume := 0, 0, false
	for {
		switch {
		case index < len(tokens) && tokens[index].kind == tokenStar && index+1 == len(tokens):
			return true
		case index < len(tokens) && tokens[index].kind == tokenStar:
			index++
			resumeIndex, resumePosition, resume = index, position, true
			continue
		case index < len(tokens) && position < len(text) && tokens[index].matches(text[position], fold):
			index++
			position++
			continue
		case index == len(tokens) && position == len(text):
			return true
		}
		if !resume || resumePosition >= len(text) {
			return false
		}
		resumePosition++
		index = resumeIndex
		position = resumePosition
	}
}

func eqByte(expected, b byte, fold bool) bool {
	if fold {
		b = toLower(b)
	}
	return expected == b
}

func eqBytes(expected, text []byte, fold bool) bool {
	if !fold {
		return bytes.Equal(expected, text)
	}
	if len(expected) != len(text) {
		return false
	}
	for i := range expected {
		if !eqByte(expected[i], text[i], true) {
			return false
		}
	}
	return true
}

func toLower(b byte) byte {
	if 'A' <= b && b <= 'Z' {
		return b + 'a' - 'A'
	}
	return b
}

func toUpper(b byte) byte {
	if 'a' <= b && b <= 'z' {
		return b - ('a' - 'A')
	}
	return b
}

type rawKind int

const (
	rawByte rawKind = iota
	rawAnyByte
	rawStar
	rawDoubleStar
	rawDoubleStarBeforeEscapedSlash
	rawClass
	rawSlash
)

type rawToken struct {
	kind rawKind
	b    byte
	set  *byteSet
}

func tokenize(pattern []byte, fold bool) ([]rawToken, bool) {
	tokens := make([]rawToken, 0, len(pattern))
	index := 0
	for index < len(pattern) {
		b := pattern[index]
		index++
		switch b {
		case '\\':
			if index >= len(pattern) {
				return nil, false
			}
			escaped := pattern[index]
			index++
			if escaped == '/' {
				tokens = append(tokens, rawToken{kind: rawSlash})
			} else {
				tokens = append(tokens, rawToken{kind: rawByte, b: escaped})
			}
		case '*':
			start := index - 1
			for index < len(pattern) && pattern[index] == '*' {
				index++
			}
			afterSlash := start == 0 || pattern[start-1] == '/'
			rest := pattern[index:]
			switch {
			case index-start < 2 || !afterSlash:
				tokens = append(tokens, rawToken{kind: rawStar})
			case len(rest) == 0 || rest[0] == '/':
				tokens = append(tokens, rawToken{kind: rawDoubleStar})
			case bytes.HasPrefix(rest, []byte(`\/`)):
				tokens = append(tokens, rawToken{kind: rawDoubleStarBeforeEscapedSlash})
			default:
				tokens = append(tokens, rawToken{kind: rawStar})
			}
		case '?':
			tokens = append(tokens, rawToken{kind: rawAnyByte})
		case '[':
			set, next, ok := parseClass(pattern, index, fold)
			if !ok {
				return nil, false
			}
			index = next
			tokens = append(tokens, rawToken{kind: rawClass, set: set})
		case '/':
			tokens = append(tokens, rawToken{kind: rawSlash})
		default:
			if fold {
				b = toLower(b)
			}
			tokens = append(tokens, rawToken{kind: rawByte, b: b})
		}
	}
	return tokens, true
}

func parseClass(pattern []byte, start int, fold bool) (*byteSet, int, bool) {
	set := &byteSet{}
	index := start
	negated := index < len(pattern) && (pattern[index] == '!' || pattern[index] == '^')
	if negated {
		index++
	}
	first := index
	hasPrevious := false
	var previous byte
	for {
		if index >= len(pattern) {
			return nil, 0, false
		}
		b := pattern[index]
		switch {
		case b == ']' && index > first:
			index++
			if negated {
				set.complement()
			}
			set.remove('/')
			return set, index, true
		case b == '\\':
			if index+1 >= len(pattern) {
				return nil, 0, false
			}
			escaped := pattern[index+1]
			set.insertSingle(escaped, fold)
			previous, hasPrevious = escaped, true
			index += 2
		case b == '-' && hasPrevious && index+1 < len(pattern) && pattern[index+1] != ']':
			next := pattern[index+1]
			high, after := next, index+2
			if next == '\\' {
				if index+2 >= len(pattern) {
					return nil, 0, false
				}
				high, after = pattern[index+2], index+3
			}
			set.insertRange(previous, high, fold)
			hasPrevious = false
			index = after
		case b == '[' && index+1 < len(pattern) && pattern[index+1] == ':' && classNameAt(pattern, index+2) >= 0:
			end := classNameAt(pattern, index+2)
			if !set.insertNamed(pattern[index+2:end-1], fold) {
				return nil, 0, false
			}
			hasPrevious = false
			index = end + 1
		default:
			set.insertSingle(b, fold)
			previous, hasPrevious = b, true
			index++
		}
	}
}

func classNameAt(pattern []byte, start int) int {
	offset := bytes.IndexByte(pattern[start:], ']')
	if offset < 0 {
		return -1
	}
	end := start + offset
	if end > start && pattern[end-1] == ':' {
		return end
	}
	return -1
}

type byteSet [4]uint64

func (s *byteSet) contains(b byte) bool { return s[b>>6]&(1<<(b&63)) != 0 }

func (s *byteSet) insert(b byte) { s[b>>6] |= 1 << (b & 63) }

func (s *byteSet) remove(b byte) { s[b>>6] &^= 1 << (b & 63) }

func (s *byteSet) insertEitherCase(b byte, fold bool) {
	s.insert(b)
	if fold {
		s.insert(toLower(b))
		s.insert(toUpper(b))
	}
}

func (s *byteSet) insertSingle(b byte, fold bool) {
	if fold && 'A' <= b && b <= 'Z' {
		return
	}
	s.insertEitherCase(b, fold)
}

func (s *byteSet) insertRange(low, high byte, fold bool) {
	for b := int(low); b <= int(high); b++ {
		s.insertEitherCase(byte(b), fold)
	}
}

func (s *byteSet) insertNamed(name []byte, fold bool) bool {
	var predicate func(byte) bool
	switch string(name) {
	case "alnum":
		predicate = func(b byte) bool { return isAlpha(b) || isDigit(b) }
	case "alpha":
		predicate = isAlpha
	case "blank":
		predicate = func(b byte) bool { return b == ' ' || b == '\t' }
	case "cntrl":
		predicate = func(b byte) bool { return b < 0x20 || b == 0x7f }
	case "digit":
		predicate = isDigit
	case "graph":
		predicate = isGraphic
	case "lower":
		predicate = func(b byte) bool { return 'a' <= b && b <= 'z' }
	case "print":
		predicate = func(b byte) bool { return b == ' ' || isGraphic(b) }
	case "punct":
		predicate = func(b byte) bool { return isGraphic(b) && !isAlpha(b) && !isDigit(b) }
	case "space":
		predicate = func(b byte) bool { return b == ' ' || b == '\t' || b == '\n' || b == '\r' }
	case "upper":
		predicate = func(b byte) bool { return 'A' <= b && b <= 'Z' }
	case "xdigit":
		predicate = func(b byte) bool { return isDigit(b) || ('a' <= b && b <= 'f') || ('A' <= b && b <= 'F') }
	default:
		return false
	}
	for b := range 256 {
		if predicate(byte(b)) {
			s.insertEitherCase(byte(b), fold)
		}
	}
	return true
}

func (s *byteSet) complement() {
	for i := range s {
		s[i] = ^s[i]
	}
}

func isAlpha(b byte) bool { return ('a' <= b && b <= 'z') || ('A' <= b && b <= 'Z') }

func isDigit(b byte) bool { return '0' <= b && b <= '9' }

func isGraphic(b byte) bool { return 0x21 <= b && b <= 0x7e }
