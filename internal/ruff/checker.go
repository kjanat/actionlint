package ruff

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/mattn/go-shellwords"
	"golang.org/x/text/unicode/norm"
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
	workingDirectory        string
	beforeCheck             func() (bool, error)
	workflowShell, jobShell *string
	mu                      sync.Mutex
}

// New creates a checker. The host must accept exit statuses 0 and 1, but not 2.
func New(run Run, wait func() error, expressionEnd ExpressionEnd, flags ...string) *Checker {
	return &Checker{run: run, wait: wait, expressionEnd: expressionEnd, flags: append([]string(nil), flags...)}
}

// Fork shares command scheduling but not workflow defaults or diagnostic state.
func (c *Checker) Fork() *Checker {
	child := New(c.run, c.wait, c.expressionEnd, c.flags...)
	child.workingDirectory = c.workingDirectory
	child.beforeCheck = c.beforeCheck
	return child
}

// WorkingDirectory sets the child process directory used for its stdin filename.
func (c *Checker) WorkingDirectory(directory string) { c.workingDirectory = directory }

// OptionalVersion skips incompatible automatically discovered executables.
// Explicit commands retain their configured invocation contract.
func (c *Checker) OptionalVersion(check *Compatibility, ctx context.Context, warning func(error)) {
	c.beforeCheck = func() (bool, error) { return check.available(ctx, c.run, c.wait, warning) }
}

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
	for _, flag := range c.flags {
		if strings.HasPrefix(flag, "@") {
			return fmt.Errorf("ruff argument files are not supported for script at %s: %q", location, flag)
		}
		if flag == "--show-files" || flag == "--show-settings" {
			return fmt.Errorf("ruff inspection output is not supported for script at %s: %q", location, flag)
		}
		if flag == "--diff" || flag == "--fix-only" {
			return fmt.Errorf("ruff editing output is not supported for script at %s: %q", location, flag)
		}
		if flag == "--statistics" {
			return fmt.Errorf("ruff statistics output is not supported for script at %s", location)
		}
		if flag == "--output-file" || strings.HasPrefix(flag, "--output-file=") ||
			strings.HasPrefix(flag, "-") && !strings.HasPrefix(flag, "--") && strings.ContainsRune(flag[1:], 'o') {
			return fmt.Errorf("ruff output redirection is not supported for script at %s: %q", location, flag)
		}
		if flag == "--silent" || strings.HasPrefix(flag, "-") && !strings.HasPrefix(flag, "--") && strings.ContainsRune(flag[1:], 's') {
			return fmt.Errorf("ruff silent output is not supported for script at %s", location)
		}
		option, _, _ := strings.Cut(flag, "=")
		if option == "--help" || option == "--watch" || option == "--add-noqa" || option == "--add-ignore" ||
			strings.HasPrefix(option, "-") && !strings.HasPrefix(option, "--") && strings.ContainsAny(option[1:], "hw") {
			return fmt.Errorf("ruff non-diagnostic mode %q is not supported for script at %s", option, location)
		}
		if strings.HasPrefix(option, "-") && !strings.HasPrefix(option, "--") && strings.ContainsRune(option[1:], 'n') {
			option = "--no-cache"
		}
		switch option {
		case "--isolated", "--ignore-noqa", "--no-fix", "--no-cache", "--target-version", "--stdin-filename", "--output-format":
			return fmt.Errorf("ruff integration-owned option %q must be removed from extra arguments for script at %s", option, location)
		}
	}
	placeholders := &templateMasks{identifiers: make(map[Position]int), parentheses: make(map[Position]Position), quoted: make(map[Position]string)}
	source, valid, err := sanitize(script, c.expressionEnd, placeholders)
	if err != nil {
		return fmt.Errorf("ruff could not check Python script at %s: %w", location, err)
	}
	if !valid {
		return nil
	}
	if c.beforeCheck != nil {
		if available, err := c.beforeCheck(); err != nil || !available {
			return err
		}
	}
	directory := c.workingDirectory
	if directory == "" {
		directory = "."
	}
	directoryInfo, err := os.Stat(directory)
	if err != nil {
		return fmt.Errorf("ruff stdin directory for script at %s: %w", location, err)
	}
	if !directoryInfo.IsDir() {
		return fmt.Errorf("ruff stdin directory for script at %s is not a directory: %q", location, directory)
	}
	// Let Ruff resolve its own cwd spelling, including Windows short paths.
	const filename = "actionlint.py"
	if config.TargetVersion == "" {
		config.TargetVersion = pythonShellTarget(*shell)
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
			parent, err := os.Stat(filepath.Dir(diagnostic.Filename))
			if !filepath.IsAbs(diagnostic.Filename) || filepath.Base(diagnostic.Filename) != filename || err != nil || !os.SameFile(directoryInfo, parent) {
				return fmt.Errorf("ruff output for script at %s refers to unexpected file %q; expected %q in %q", location, diagnostic.Filename, filename, directory)
			}
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		for _, d := range diagnostics {
			// Opaque values, quoted annotations and exports may hide references.
			// Comment-only templates add no masks; unrelated checks still apply.
			if len(placeholders.identifiers) > 0 && requiresCompleteReferences(d.Code) {
				continue
			}
			if (d.Code == "F822" || d.Code == "F541") && placeholders.dynamicQuotedValue(d) {
				continue
			}
			// A mask can manufacture undefined-name and useless-expression
			// findings. Only its exact identifier range is excluded.
			if width, ok := placeholders.identifiers[d.Location]; ok && (d.Code == "F821" || d.Code == "B018") &&
				d.EndLocation == (Position{Row: d.Location.Row, Column: d.Location.Column + width}) {
				continue
			}
			// UP034 can identify the wrapper added around an opaque value.
			// Source parentheses extend outside this exact generated range.
			if end, ok := placeholders.parentheses[d.Location]; ok && d.Code == "UP034" && d.EndLocation == end {
				continue
			}
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
	return pythonAdapterArguments(shell, command) != nil
}

func pythonAdapterArguments(shell, command string) []string {
	name := strings.ToLower(command[strings.LastIndexAny(command, `/\`)+1:])
	name = strings.TrimSuffix(name, ".cmd")
	if name != "actions-shell" && name != "actions-shells" {
		return nil
	}
	// The documented Python adapters take the runtime first and script last.
	parser := shellwords.NewParser()
	parser.ParseEnv, parser.ParseBacktick = false, false
	args, err := parser.Parse(shell[len(command):])
	if err != nil || parser.Position >= 0 || len(args) < 2 ||
		(args[0] != "python" && args[0] != "py") || args[len(args)-1] != "{0}" {
		return nil
	}
	return args
}

func isPythonCommand(command string) bool {
	command = command[strings.LastIndexAny(command, `/\`)+1:]
	command = strings.TrimSuffix(strings.ToLower(command), ".exe")
	if command == "python" || command == "python3" || command == "py" {
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

func pythonShellTarget(shell string) string {
	shell = strings.TrimSpace(shell)
	words := strings.Fields(shell)
	if len(words) == 0 {
		return ""
	}
	if args := pythonAdapterArguments(shell, words[0]); args != nil {
		words = args
	}
	command := words[0]
	command = command[strings.LastIndexAny(command, `/\`)+1:]
	command = strings.TrimSuffix(strings.ToLower(command), ".exe")
	if command == "py" && len(words) >= 3 && slices.ContainsFunc(words[2:], func(word string) bool {
		return word == "{0}" || word == `"{0}"` || word == "'{0}'"
	}) {
		selector := words[1]
		if len(selector) >= 2 && (selector[0] == '"' || selector[0] == '\'') && selector[len(selector)-1] == selector[0] {
			selector = selector[1 : len(selector)-1]
		}
		version, ok := strings.CutPrefix(selector, "-V:")
		if !ok {
			version, ok = strings.CutPrefix(selector, "-")
		}
		if !ok {
			return ""
		}
		if company, tag, hasCompany := strings.Cut(version, "/"); hasCompany {
			if !strings.HasPrefix(selector, "-V:") || !strings.EqualFold(company, "PythonCore") {
				return ""
			}
			version = tag
		}
		version, architecture, hasArchitecture := strings.Cut(version, "-")
		if hasArchitecture && architecture != "32" && architecture != "64" && architecture != "arm64" {
			return ""
		}
		command = "python" + version
	}
	if minor, ok := strings.CutPrefix(command, "python3."); ok {
		if target := "py3" + minor; slices.Contains(SupportedTargetVersions(), target) {
			return target
		}
	}
	return ""
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

// Sanitize masks templates with independent opaque values, preserving Unicode
// columns and line breaks. Check filters undefined-name findings for the masks.
// Scripts whose templates cannot preserve Python syntax and positions are skipped.
func Sanitize(src string, expressionEnd ExpressionEnd) (string, bool, error) {
	return sanitize(src, expressionEnd, nil)
}

type templateMasks struct {
	identifiers map[Position]int
	parentheses map[Position]Position
	quoted      map[Position]string
}

func requiresCompleteReferences(code string) bool {
	switch code {
	case "F401", "F811", "F841", "F842", "ARG001", "ARG002", "ARG003", "ARG004", "ARG005", "B007":
		return true
	default:
		return false
	}
}

func (m *templateMasks) dynamicQuotedValue(d Diagnostic) bool {
	for start, name := range m.quoted {
		end := Position{Row: start.Row, Column: start.Column + len(name)}
		if (d.Location.Row < start.Row || d.Location.Row == start.Row && d.Location.Column <= start.Column) &&
			(d.EndLocation.Row > end.Row || d.EndLocation.Row == end.Row && d.EndLocation.Column >= end.Column) &&
			(d.Code == "F541" || strings.Contains(d.Message, name)) {
			return true
		}
	}
	return false
}

func sanitize(src string, expressionEnd ExpressionEnd, placeholders *templateMasks) (string, bool, error) {
	var out strings.Builder
	var state pythonLexicalState
	// Python normalizes identifiers, so Unicode aliases also reserve names.
	original := norm.NFKC.String(src)
	nextName := uint64(0)
	position := Position{Row: 1, Column: 1}
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
		if state.inFieldExpression() && !state.fieldComment && state.quotePrevious == '!' {
			return "", false, nil
		}
		if state.patternCapture {
			return "", false, nil
		}
		if state.quote == 0 && !state.comment && (templateTouchesPythonToken(src, start, end) || templateFollowsPythonValue(state.lastToken) || state.subscriptDepth == 0 && (state.nameRequired || templateIsAssignmentTarget(src[:start], src[end:], state.depth))) {
			return "", false, nil
		}
		if state.quote == 0 && !state.comment && state.casePattern {
			state.pendingCapture = true
		}
		runes := []rune(src[start:end])
		for i, r := range runes {
			if r != '\n' && r != '\r' {
				runes[i] = ' '
			}
		}
		if state.comment || state.fieldComment {
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
			runes[0], runes[len(runes)-1] = '(', ')'
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
				}
				finish := position
				advancePosition(&finish, src[start:end])
				placeholders.parentheses[position] = finish
			}
		}
		state.escaped = false
		if state.quote == 0 && !state.comment {
			state.lastToken = ")"
		}
		out.WriteString(string(runes))
		advancePosition(&position, src[start:end])
		src = src[end:]
	}
}

func advancePosition(position *Position, text string) {
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
	case "raise", "except", "with", "async", "elif", "match", "case":
		return false
	}
	return pythonTokenCanBeSubscripted(token)
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
	formatted     bool
	fields        []pythonFormatField
	quotePrevious byte
	fieldWord     string
	fieldComment  bool
}

type pythonFormatField struct {
	brackets int
	format   bool
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

func (s *pythonStringState) consumeField(c byte) {
	if len(s.fields) == 0 || s.fields[len(s.fields)-1].format {
		if c == '{' {
			s.fields = append(s.fields, pythonFormatField{})
		} else if c == '}' && len(s.fields) > 0 {
			s.fields = s.fields[:len(s.fields)-1]
		}
		return
	}
	field := &s.fields[len(s.fields)-1]
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
		s.lastToken = s.word
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
			if s.inFieldExpression() && c == '#' {
				s.fieldComment = true
				continue
			}
			if s.inFieldExpression() && (c == '\'' || c == '"') {
				prefix := strings.ToLower(s.fieldWord)
				s.fieldWord = ""
				s.stringParents = append(s.stringParents, s.pythonStringState)
				s.pythonStringState = pythonStringState{quote: c, formatted: pythonFormattedPrefix(prefix)}
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
			s.formatted = pythonFormattedPrefix(prefix)
			s.fields, s.quotePrevious = nil, 0
			s.triple = i+2 < len(text) && text[i+1] == c && text[i+2] == c
			if s.triple {
				i += 2
			}
		}
	}
}
