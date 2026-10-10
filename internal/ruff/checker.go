package ruff

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/mattn/go-shellwords"
)

// Run schedules a command through the host's bounded, cancellable process pool.
type Run func(args []string, stdin string, callback func([]byte, error) error)

// ExpressionEnd returns the byte length of a valid expression, including its
// closing delimiter. The input starts immediately after the opening ${{.
type ExpressionEnd func(string) (int, bool)

// Checker owns Python selection, invocation and diagnostic delivery. It knows
// nothing about the host AST: its callbacks bridge expressions and source maps.
type Checker struct {
	run                     Run
	wait                    func() error
	expressionEnd           ExpressionEnd
	flags                   []string
	workflowShell, jobShell *string
	mu                      sync.Mutex
}

// New creates a checker. The host must accept exit statuses 0 and 1, but not 2.
func New(run Run, wait func() error, expressionEnd ExpressionEnd, flags ...string) *Checker {
	return &Checker{run: run, wait: wait, expressionEnd: expressionEnd, flags: append([]string(nil), flags...)}
}

// Fork shares command scheduling but not workflow defaults or diagnostic state.
func (c *Checker) Fork() *Checker { return New(c.run, c.wait, c.expressionEnd, c.flags...) }

// UnsetEnvironment identifies ambient settings that bypass the stdout protocol.
// They must be removed after applying explicit child environment overrides too.
func UnsetEnvironment() []string { return []string{"RUFF_OUTPUT_FILE"} }

// WorkflowShell sets the workflow-level default shell.
func (c *Checker) WorkflowShell(shell *string) { c.workflowShell = shell }

// JobShell sets (or clears) the job-level default shell.
func (c *Checker) JobShell(shell *string) { c.jobShell = shell }

// Wait drains scheduled checks before diagnostics are read.
func (c *Checker) Wait() error { return c.wait() }

// Check schedules a Python script and serializes delivery to report.
func (c *Checker) Check(script string, shell *string, location string, config Config, report func(Diagnostic)) error {
	if config.Enabled != nil && !*config.Enabled {
		return nil
	}
	if shell == nil {
		shell = c.jobShell
	}
	if shell == nil {
		shell = c.workflowShell
	}
	if shell == nil || strings.Contains(*shell, "${{") {
		return nil
	}
	if !isPythonShell(*shell) {
		return nil
	}
	source, valid, err := Sanitize(script, c.expressionEnd)
	if err != nil {
		return fmt.Errorf("ruff could not check Python script at %s: %w", location, err)
	}
	if !valid {
		return nil
	}
	filename, err := filepath.Abs("actionlint.py")
	if err != nil {
		return fmt.Errorf("ruff stdin filename for script at %s: %w", location, err)
	}
	defaults := arguments(config, filename)
	args := make([]string, 1, len(defaults)+len(c.flags))
	args[0] = "check"
	args = append(args, c.flags...)
	args = append(args, defaults[1:]...)
	c.run(args, source, func(stdout []byte, err error) error {
		if err != nil {
			return fmt.Errorf("ruff failed while checking Python script at %s: %w", location, err)
		}
		diagnostics, err := Decode(stdout)
		if err != nil {
			return fmt.Errorf("ruff output for script at %s: %w", location, err)
		}
		for _, diagnostic := range diagnostics {
			if filepath.Clean(diagnostic.Filename) != filename {
				return fmt.Errorf("ruff output for script at %s refers to unexpected file %q; expected %q", location, diagnostic.Filename, filename)
			}
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		for _, d := range diagnostics {
			report(d)
		}
		return nil
	})
	return nil
}

func isPythonShell(shell string) bool {
	shell = strings.TrimSpace(shell)
	words := strings.Fields(shell)
	if len(words) == 0 {
		return false
	}
	command := words[0]
	if isPythonCommand(command) {
		return true
	}
	name := strings.ToLower(command[strings.LastIndexAny(command, `/\`)+1:])
	name = strings.TrimSuffix(name, ".cmd")
	if name != "actions-shell" && name != "actions-shells" {
		return false
	}
	// The documented Python adapters take the runtime first and script last.
	parser := shellwords.NewParser()
	parser.ParseEnv, parser.ParseBacktick = false, false
	args, err := parser.Parse(shell[len(command):])
	return err == nil && parser.Position < 0 && len(args) >= 2 &&
		(args[0] == "python" || args[0] == "py") && args[len(args)-1] == "{0}"
}

func isPythonCommand(command string) bool {
	command = command[strings.LastIndexAny(command, `/\`)+1:]
	command = strings.TrimSuffix(strings.ToLower(command), ".exe")
	if command == "python" || command == "python3" {
		return true
	}
	minor, ok := strings.CutPrefix(command, "python3.")
	if !ok || minor == "" {
		return false
	}
	for _, digit := range minor {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
}

func arguments(config Config, filename string) []string {
	target := config.TargetVersion
	if target == "" {
		target = "py314"
	}
	selectRules, ignoreRules := config.Select, config.Ignore
	if selectRules != nil && len(selectRules) == 0 {
		// An empty --select is invalid. Equal-specificity ALL selectors disable
		// lint codes without suppressing Python syntax errors.
		selectRules, ignoreRules = []string{"ALL"}, []string{"ALL"}
	}
	if selectRules == nil {
		selectRules = []string{"F"}
	}
	args := []string{"check", "--isolated", "--target-version", target, "--select", strings.Join(selectRules, ","), "--ignore-noqa", "--no-fix", "--no-cache", "--output-format", "json", "--stdin-filename", filename}
	if len(ignoreRules) > 0 {
		args = append(args, "--ignore", strings.Join(ignoreRules, ","))
	}
	return append(args, "-")
}

// Sanitize masks templates with a neutral Python value, preserving Unicode
// columns and line breaks without introducing undefined Python identifiers.
// Scripts whose templates cannot preserve Python syntax and positions are skipped.
func Sanitize(src string, expressionEnd ExpressionEnd) (string, bool, error) {
	var out strings.Builder
	var state pythonLexicalState
	for {
		start := strings.Index(src, "${{")
		if start < 0 {
			out.WriteString(src)
			return out.String(), true, nil
		}
		length, valid := expressionEnd(src[start+3:])
		if !valid || length < 2 || length > len(src)-start-3 {
			return "", false, nil
		}
		end := start + 3 + length
		out.WriteString(src[:start])
		state.consume(src[:start])
		if state.quote == 0 && !state.comment && (templateTouchesPythonToken(src, start, end) || state.subscriptDepth == 0 && (state.nameRequired || templateIsAssignmentTarget(src[:start], src[end:], state.depth))) {
			return "", false, nil
		}
		runes := []rune(src[start:end])
		for i, r := range runes {
			if r != '\n' && r != '\r' {
				runes[i] = ' '
			}
		}
		if state.comment {
			// Each physical line remains comment text, including the suffix
			// after the template. Empty lines can remain empty.
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
			runes[0], runes[len(runes)-2], runes[len(runes)-1] = '(', '0', ')'
		}
		state.escaped = false
		out.WriteString(string(runes))
		src = src[end:]
	}
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

// pythonLexicalState distinguishes comment and string template placement while
// leaving Python syntax validation to Ruff.
type pythonLexicalState struct {
	quote          byte
	triple         bool
	comment        bool
	escaped        bool
	word           string
	depth          int
	nameRequired   bool
	nameList       bool
	className      bool
	lastToken      string
	subscriptDepth int
	nameListDepth  int
	functionName   bool
	parameterDepth int
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
	if s.word != "" {
		s.lastToken = s.word
	}
	switch s.word {
	case "import", "from", "global", "nonlocal", "del", "for", "lambda":
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
	case ':', ';':
		s.nameRequired = false
		if c != ':' || s.parameterDepth == 0 {
			s.nameList = false
		}
		s.className = false
	case '\n':
		if s.depth == 0 {
			s.nameRequired, s.nameList = false, false
			s.className = false
		}
	}
	if c != ' ' && c != '\t' && c != '\r' {
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
		if s.comment {
			if c == '\n' {
				s.comment = false
				s.consumeCode(c)
			}
			continue
		}
		if s.escaped {
			s.escaped = false
			if c == '\r' && i+1 < len(text) && text[i+1] == '\n' {
				i++
			}
			continue
		}
		if c == '\\' {
			if s.quote == 0 {
				s.consumeCode(c)
			}
			s.escaped = true
			continue
		}
		if s.quote != 0 {
			if c == s.quote {
				if !s.triple {
					s.quote = 0
				} else if i+2 < len(text) && text[i+1] == c && text[i+2] == c {
					s.quote, s.triple = 0, false
					i += 2
				}
			}
			continue
		}
		s.consumeCode(c)
		switch c {
		case '#':
			s.comment = true
		case '\'', '"':
			s.quote = c
			s.triple = i+2 < len(text) && text[i+1] == c && text[i+2] == c
			if s.triple {
				i += 2
			}
		}
	}
}
