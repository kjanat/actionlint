package python

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"actionlint.kjanat.dev/v2/analyzer"

	"golang.org/x/text/unicode/norm"
)

func sanitize(src string, expressionEnd ExpressionEnd, placeholders *Masks) (string, bool, error) {
	var out strings.Builder
	var state pythonLexicalState
	// Python normalizes identifiers, so Unicode aliases also reserve names.
	original := norm.NFKC.String(src)
	if placeholders != nil {
		placeholders.sourceLines = strings.Split(src, "\n")
		placeholders.percentConversions = make(map[analyzer.ScriptPosition]bool)
	}
	nextName := uint64(0)
	position := analyzer.ScriptPosition{Row: 1, Column: 1}
	for {
		start := strings.Index(src, "${{")
		if start < 0 {
			state.consume(src)
			if state.patternCapture {
				return "", false, nil
			}
			out.WriteString(src)
			return out.String(), true, nil
		}
		length, valid := expressionEnd(src[start+3:])
		if !valid || length < 2 || length > len(src)-start-3 {
			return "", false, nil
		}
		end := start + 3 + length
		out.WriteString(src[:start])
		advancePosition(&position, src[:start])
		state.consume(src[:start])
		if state.quote != 0 && !state.raw && !state.inFieldExpression() &&
			(state.escaped || state.escapeDigits > 0 || state.escapeName || state.escapeBrace) {
			return "", false, nil
		}
		if state.inFieldExpression() && !state.fieldComment && state.quotePrevious == '!' {
			return "", false, nil
		}
		if state.patternCapture {
			return "", false, nil
		}
		code := &state
		if state.inFieldExpression() {
			code = state.fields[len(state.fields)-1].code
			code.consumeCode(' ')
		}
		inCode := (state.quote == 0 || state.inFieldExpression()) && !state.comment && !state.fieldComment
		// Whole-statement templates can declare names that later code uses.
		// A value mask would lose those bindings, so skip the script.
		if inCode && !state.inFieldExpression() && code.depth == 0 && templateIsPythonStatement(src[:start], src[end:], code) {
			return "", false, nil
		}
		assignment := templateIsAssignmentTarget(src[:start], src[end:], code.depth)
		if state.inFieldExpression() && templateDebugFieldSuffix(src[end:]) {
			assignment = false
		}
		if inCode && (code.asyncKeyword || templateTouchesPythonToken(src, start, end) || templateFollowsPythonValue(code.lastToken) || code.subscriptDepth == 0 && (code.nameRequired || assignment)) {
			return "", false, nil
		}
		if state.quote == 0 && !state.comment && state.casePattern {
			state.pendingCapture = true
		}
		if placeholders != nil {
			if placeholders.lines == nil {
				placeholders.lines = make(map[int]bool)
			}
			lastLine := position.Row + strings.Count(src[start:end], "\n")
			for line := position.Row; line <= lastLine; line++ {
				placeholders.lines[line] = true
			}
		}
		runes := []rune(src[start:end])
		for i, r := range runes {
			if r != '\n' && r != '\r' {
				runes[i] = ' '
			}
		}
		if state.comment || state.fieldComment {
			first := true
			for i, r := range runes {
				if r == '\n' {
					first = true
				} else if r != '\r' && first {
					runes[i], first = '#', false
				}
			}
		} else {
			// Ordinary quoted strings require explicit continuations. Outside
			// strings, parentheses allow multiline expressions and empty lines.
			if state.quote != 0 && !state.triple {
				for i, r := range runes {
					if r != '\n' {
						continue
					}
					previous := i - 1
					if previous >= 0 && runes[previous] == '\r' {
						previous--
					}
					if previous < 0 || runes[previous] == '\n' {
						return "", false, nil
					}
					runes[previous] = '\\'
				}
			}
			runes[0], runes[len(runes)-1] = '(', ')'
			// Use a valid percent conversion to avoid a synthetic F509.
			if !inCode && state.quote != 0 && templateProvidesPercentConversion(src[:start]) {
				runes[0] = 's'
			}
			var name string
			for {
				name = "_" + strconv.FormatUint(nextName, 36)
				nextName++
				if !strings.Contains(original, name) {
					break
				}
			}
			slot, width := -1, 0
			for i, r := range runes {
				if r == ' ' {
					width++
					if width == len(name) {
						slot = i - width + 1
						break
					}
				} else {
					width = 0
				}
			}
			if slot < 0 {
				return "", false, nil
			}
			copy(runes[slot:], []rune(name))
			if placeholders != nil {
				identifierStart := position
				advancePosition(&identifierStart, string(runes[:slot]))
				placeholders.identifiers[identifierStart] = len(name)
				if state.quote != 0 {
					placeholders.quoted[identifierStart] = name
					placeholders.percentConversions[identifierStart] = templateProvidesPercentConversion(src[:start])
				}
				finish := position
				advancePosition(&finish, src[start:end])
				placeholders.parentheses[position] = finish
			}
		}
		state.escaped = false
		if inCode {
			code.lastToken = ")"
		}
		out.WriteString(string(runes))
		advancePosition(&position, src[start:end])
		src = src[end:]
	}
}

func templateDebugFieldSuffix(suffix string) bool {
	rest, ok := strings.CutPrefix(strings.TrimSpace(suffix), "=")
	if !ok {
		return false
	}
	rest = strings.TrimSpace(rest)
	return rest != "" && strings.ContainsRune("}!:", rune(rest[0]))
}

func advancePosition(position *analyzer.ScriptPosition, text string) {
	for _, r := range text {
		if r == '\n' {
			position.Row++
			position.Column = 1
		} else {
			position.Column++
		}
	}
}

func templateFollowsPythonValue(token string) bool {
	// A template after a complete operand supplies syntax. Statement keywords
	// can introduce a value expression.
	switch token {
	case "raise", "except", "with", "elif", "match", "case":
		return false
	}
	return pythonTokenCanBeSubscripted(token)
}

func templateIsPythonStatement(prefix, suffix string, state *pythonLexicalState) bool {
	rest := strings.TrimLeft(suffix, " \t\r")
	if rest != "" && !strings.ContainsRune("\n;#", rune(rest[0])) {
		return false
	}
	// Explicit continuations keep the same statement boundary.
	prefix = strings.ReplaceAll(strings.ReplaceAll(prefix, "\\\r\n", ""), "\\\n", "")
	part := strings.TrimSpace(prefix[strings.LastIndexAny(prefix, "\n;")+1:])
	switch state.lastToken {
	case "", "\n", ";", "\\":
		return part == ""
	case ":":
		return state.suiteColon
	}
	return false
}

func templateTouchesPythonToken(source string, start, end int) bool {
	left, _ := utf8.DecodeLastRuneInString(source[:start])
	right, _ := utf8.DecodeRuneInString(source[end:])
	isTokenPart := func(r rune) bool {
		return r == '_' || r == '.' || r == '\'' || r == '"' || unicode.IsLetter(r) || unicode.IsNumber(r) || unicode.IsMark(r)
	}
	return isTokenPart(left) || isTokenPart(right) || strings.HasPrefix(source[end:], "${{")
}

func templateIsAssignmentTarget(prefix, suffix string, depth int) bool {
	suffix = strings.TrimLeft(suffix, " \t")
	if depth == 0 && strings.HasPrefix(suffix, ":") && strings.TrimSpace(prefix[strings.LastIndexAny(prefix, "\n;")+1:]) == "" {
		return true
	}
	if strings.HasPrefix(suffix, "=") && !strings.HasPrefix(suffix, "==") {
		return true
	}
	for _, operator := range []string{":=", "+=", "-=", "*=", "/=", "//=", "%=", "**=", "&=", "|=", "^=", ">>=", "<<=", "@="} {
		if strings.HasPrefix(suffix, operator) {
			return true
		}
	}
	if strings.HasPrefix(suffix, ",") || strings.HasPrefix(suffix, ")") || strings.HasPrefix(suffix, "]") {
		for offset, token := range suffix {
			switch {
			case token == '=':
				return depth == 0 && !strings.HasPrefix(suffix[offset:], "==")
			case token == '(' || token == '[':
				depth++
			case (token == ')' || token == ']') && depth > 0:
				depth--
			case token == '\n' && depth == 0:
				return false
			case unicode.IsSpace(token), unicode.IsLetter(token), unicode.IsNumber(token), token == '_', token == ',', token == '.', token == '*':
			default:
				return false
			}
		}
	}
	return false
}

// pythonStringState tracks one quoted string and its formatted replacement fields.
type pythonStringState struct {
	quote         byte
	triple        bool
	raw           bool
	bytes         bool
	escapeDigits  int
	escapeName    bool
	escapeBrace   bool
	formatted     bool
	fields        []pythonFormatField
	quotePrevious byte
	fieldWord     string
	fieldComment  bool
}

type pythonFormatField struct {
	brackets int
	format   bool
	code     *pythonLexicalState
}

func (s *pythonStringState) inFieldExpression() bool {
	return s.formatted && len(s.fields) > 0 && !s.fields[len(s.fields)-1].format
}

func pythonFormattedPrefix(prefix string) bool {
	switch prefix {
	case "f", "rf", "fr", "t", "rt", "tr":
		return true
	default:
		return false
	}
}

func pythonRawPrefix(prefix string) bool {
	switch prefix {
	case "r", "rf", "fr", "rb", "br", "rt", "tr":
		return true
	default:
		return false
	}
}

func (s *pythonStringState) consumeField(c byte) {
	if len(s.fields) == 0 || s.fields[len(s.fields)-1].format {
		if c == '{' {
			s.fields = append(s.fields, pythonFormatField{code: &pythonLexicalState{depth: 1}})
		} else if c == '}' && len(s.fields) > 0 {
			s.fields = s.fields[:len(s.fields)-1]
		}
		return
	}
	field := &s.fields[len(s.fields)-1]
	field.code.consumeCode(c)
	switch c {
	case '(', '[', '{':
		field.brackets++
	case ')', ']', '}':
		if field.brackets > 0 {
			field.brackets--
		} else if c == '}' {
			s.fields = s.fields[:len(s.fields)-1]
		}
	case ':':
		if field.brackets == 0 {
			field.format = true
		}
	}
}

// pythonLexicalState distinguishes comment and string template placement while
// leaving Python syntax validation to Ruff.
type pythonLexicalState struct {
	pythonStringState
	stringParents   []pythonStringState
	comment         bool
	escaped         bool
	word            string
	depth           int
	nameRequired    bool
	nameList        bool
	className       bool
	lastToken       string
	asyncKeyword    bool
	suiteColon      bool
	statementHead   string
	subscriptDepth  int
	nameListDepth   int
	functionName    bool
	parameterDepth  int
	casePattern     bool
	pendingCapture  bool
	patternCapture  bool
	valueFromDepths []int
}

// consumeCode tracks name-only positions without interpreting Python values.
// Names in strings and comments never reach this method.
func (s *pythonLexicalState) consumeCode(c byte) {
	if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_' || c >= '0' && c <= '9' || c >= utf8.RuneSelf {
		if len(s.word) < 9 {
			s.word += string(c)
		}
		return
	}
	previous := s.lastToken
	if s.word != "" {
		if s.statementHead == "" && s.depth == 0 {
			s.statementHead = s.word
		}
		s.lastToken = s.word
		s.asyncKeyword = s.word == "async"
	}
	switch s.word {
	case "case":
		if s.depth == 0 && (previous == "" || previous == "\n") {
			s.casePattern = true
		}
	case "if":
		if s.casePattern && s.depth == 0 {
			s.patternCapture = s.patternCapture || s.pendingCapture
			s.casePattern, s.pendingCapture = false, false
		}
	case "yield", "raise":
		if n := len(s.valueFromDepths); n == 0 || s.valueFromDepths[n-1] != s.depth {
			s.valueFromDepths = append(s.valueFromDepths, s.depth)
		}
	case "from":
		// Yield delegation and exception causes introduce value expressions.
		n := len(s.valueFromDepths)
		valueFrom := n > 0 && s.valueFromDepths[n-1] == s.depth
		s.nameRequired, s.nameList = !valueFrom, !valueFrom
		s.nameListDepth = s.depth
		if valueFrom {
			s.valueFromDepths = s.valueFromDepths[:n-1]
		}
	case "import", "global", "nonlocal", "del", "for", "lambda":
		s.nameRequired, s.nameList = true, true
		s.nameListDepth = s.depth
	case "def":
		s.nameRequired, s.nameList, s.functionName = true, true, true
	case "class":
		s.nameRequired, s.className = true, true
	case "as":
		s.nameRequired = true
	case "in":
		s.nameRequired, s.nameList = false, false
	}
	s.word = ""
	if c != ' ' && c != '\t' && c != '\r' && c != '\\' {
		s.asyncKeyword = false
	}
	switch c {
	case '(', '[', '{':
		s.depth++
		if c == '[' && s.subscriptDepth == 0 && !s.className && !s.functionName && pythonTokenCanBeSubscripted(s.lastToken) {
			s.subscriptDepth = s.depth
		}
		if c == '(' && s.functionName {
			s.parameterDepth, s.functionName = s.depth, false
		}
		if s.nameRequired && s.nameList {
			s.nameListDepth = s.depth
		}
		if c == '(' && s.className {
			s.nameRequired, s.className = false, false
		}
	case ')', ']', '}':
		if n := len(s.valueFromDepths); n > 0 && s.valueFromDepths[n-1] == s.depth {
			s.valueFromDepths = s.valueFromDepths[:n-1]
		}
		if s.depth == s.subscriptDepth {
			s.subscriptDepth = 0
		}
		if s.depth == s.parameterDepth {
			s.parameterDepth = 0
			s.nameRequired, s.nameList = false, false
		}
		s.depth--
		s.nameListDepth = min(s.nameListDepth, s.depth)
	case ',':
		if s.depth == s.nameListDepth {
			s.nameRequired = s.nameList
		}
	case '=':
		s.nameRequired = false
		if s.depth == 0 {
			s.casePattern, s.pendingCapture = false, false
		}
	case ':', ';':
		if s.depth == 0 {
			s.suiteColon = false
			if c == ':' {
				switch s.statementHead {
				case "if", "elif", "else", "while", "for", "with", "try", "except", "finally", "match", "case", "def", "class", "async":
					s.suiteColon = true
				}
			}
			s.statementHead = ""
			s.patternCapture = s.patternCapture || c == ':' && s.casePattern && s.pendingCapture
			s.casePattern, s.pendingCapture = false, false
			s.valueFromDepths = s.valueFromDepths[:0]
		}
		s.nameRequired = false
		if c != ':' || s.parameterDepth == 0 {
			s.nameList = false
		}
		s.className = false
	case '\n':
		if s.depth == 0 {
			s.statementHead = ""
			s.casePattern, s.pendingCapture = false, false
			s.nameRequired, s.nameList = false, false
			s.className = false
			s.valueFromDepths = s.valueFromDepths[:0]
		}
	}
	if c != ' ' && c != '\t' && c != '\r' && (c != '\n' || s.depth <= 0) {
		s.lastToken = string(c)
	}
}

func pythonTokenCanBeSubscripted(token string) bool {
	switch token {
	case "", "import", "from", "global", "nonlocal", "def", "class", "del", "for", "in", "lambda", "return", "yield", "assert", "await", "if", "else", "while", "not", "and", "or", "is":
		return false
	case ")", "]", "}", "'", "\"":
		return true
	}
	first, _ := utf8.DecodeRuneInString(token)
	return first == '_' || unicode.IsLetter(first) || unicode.IsNumber(first)
}

func (s *pythonLexicalState) consume(text string) {
	for i := 0; i < len(text); i++ {
		c := text[i]
		if s.fieldComment {
			if c == '\n' {
				s.fieldComment = false
			}
			continue
		}
		if s.comment {
			if c == '\n' {
				s.comment = false
				s.consumeCode(c)
			}
			continue
		}
		if s.escaped {
			s.escaped = false
			if s.quote != 0 && !s.raw && !s.inFieldExpression() {
				switch c {
				case 'x':
					s.escapeDigits = 2
				case 'u':
					if !s.bytes {
						s.escapeDigits = 4
					}
				case 'U':
					if !s.bytes {
						s.escapeDigits = 8
					}
				case 'N':
					if !s.bytes {
						s.escapeBrace = true
					}
				}
			}
			if c == '\r' && i+1 < len(text) && text[i+1] == '\n' {
				i++
			}
			continue
		}
		if s.escapeDigits > 0 {
			if c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F' {
				s.escapeDigits--
				continue
			}
			s.escapeDigits = 0
		}
		if s.escapeBrace {
			s.escapeBrace = false
			if c == '{' {
				s.escapeName = true
				continue
			}
		}
		if s.escapeName {
			if c == '}' {
				s.escapeName = false
				continue
			}
			if c != s.quote && c != '\n' && c != '\r' {
				continue
			}
			s.escapeName = false
		}
		if c == '\\' {
			if s.quote == 0 {
				s.consumeCode(c)
			}
			s.escaped = true
			continue
		}
		if s.quote != 0 {
			if s.inFieldExpression() && c == '#' {
				s.fieldComment = true
				continue
			}
			if s.inFieldExpression() && (c == '\'' || c == '"') {
				s.fields[len(s.fields)-1].code.consumeCode(c)
				prefix := strings.ToLower(s.fieldWord)
				s.fieldWord = ""
				s.stringParents = append(s.stringParents, s.pythonStringState)
				s.pythonStringState = pythonStringState{quote: c, formatted: pythonFormattedPrefix(prefix), raw: pythonRawPrefix(prefix), bytes: prefix == "b" || prefix == "br" || prefix == "rb"}
				s.triple = i+2 < len(text) && text[i+1] == c && text[i+2] == c
				if s.triple {
					i += 2
				}
				continue
			}
			if s.formatted {
				if s.inFieldExpression() && (c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z') {
					if len(s.fieldWord) < 3 {
						s.fieldWord += string(c)
					}
				} else {
					s.fieldWord = ""
				}
				s.quotePrevious = c
				if (c == '{' || c == '}') && len(s.fields) == 0 && i+1 < len(text) && text[i+1] == c {
					i++
					continue
				}
				s.consumeField(c)
			}
			if c == s.quote {
				if !s.triple {
					s.quote = 0
				} else if i+2 < len(text) && text[i+1] == c && text[i+2] == c {
					s.quote, s.triple = 0, false
					i += 2
				}
				if s.quote == 0 {
					s.pythonStringState = pythonStringState{}
					if n := len(s.stringParents); n > 0 {
						s.pythonStringState = s.stringParents[n-1]
						s.stringParents = s.stringParents[:n-1]
						s.quotePrevious = c
					}
				}
			}
			continue
		}
		prefix := strings.ToLower(s.word)
		s.consumeCode(c)
		switch c {
		case '#':
			s.comment = true
		case '\'', '"':
			s.quote = c
			s.raw = pythonRawPrefix(prefix)
			s.bytes = prefix == "b" || prefix == "br" || prefix == "rb"
			s.formatted = pythonFormattedPrefix(prefix)
			s.fields, s.quotePrevious = nil, 0
			s.triple = i+2 < len(text) && text[i+1] == c && text[i+2] == c
			if s.triple {
				i += 2
			}
		}
	}
}
