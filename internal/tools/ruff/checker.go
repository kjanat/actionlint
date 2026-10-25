package ruff

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/internal/analyzerhost"

	"github.com/mattn/go-shellwords"
	"golang.org/x/text/unicode/norm"
)

// Let Ruff resolve its own cwd spelling, including Windows short paths.
const stdinFilename = "actionlint.py"

// Run schedules a command through the host's bounded, cancellable process pool.
type Run func(args []string, stdin string, callback func([]byte, error) error)

// ExpressionEnd returns the byte length of a valid expression, including its
// closing delimiter. The input starts immediately after the opening ${{.
type ExpressionEnd func(string) (int, bool)

// Checker owns Python selection, invocation and diagnostic delivery. It knows
// nothing about the host AST: its callbacks bridge expressions and source maps.
type Checker struct {
	commandPolicy
	backend       analyzer.ScriptBackend
	ctx           context.Context
	pending       []analyzerhost.Task[Diagnostic]
	run           Run
	wait          func() error
	expressionEnd ExpressionEnd
	beforeCheck   func() (bool, error)
	mu            sync.Mutex
}

// New creates a checker. The host must accept exit statuses 0 and 1, but not 2.
func New(run Run, wait func() error, expressionEnd ExpressionEnd, flags ...string) *Checker {
	return &Checker{run: run, wait: wait, expressionEnd: expressionEnd, flags: append([]string(nil), flags...)}
}

// NewBackend binds a transport-independent engine to actionlint's Python
// selection, sanitization, and result filtering. No process is resolved.
func NewBackend(ctx context.Context, backend analyzer.ScriptBackend, expressionEnd ExpressionEnd) *Checker {
	return &Checker{backend: backend, ctx: ctx, expressionEnd: expressionEnd}
}

// Fork shares command scheduling and copies checker configuration.
func (c *Checker) Fork() *Checker {
	child := New(c.run, c.wait, c.expressionEnd, c.flags...)
	if c.backend != nil {
		child = NewBackend(c.ctx, c.backend, c.expressionEnd)
	}
	child.workingDirectory = c.workingDirectory
	child.beforeCheck = c.beforeCheck
	child.commandError = c.commandError
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

// Wait drains scheduled checks before diagnostics are read.
func (c *Checker) Wait() error {
	if c.backend == nil {
		return c.wait()
	}
	tasks := c.pending
	c.pending = nil
	_, err := analyzerhost.RunTasks(c.ctx, tasks)
	return err
}

// Check schedules a Python script and serializes delivery to report.
func (c *Checker) Check(script string, shell *string, location string, config Config, report func(Diagnostic)) error {
	if config.Enabled != nil && !*config.Enabled {
		return nil
	}
	if shell == nil || strings.Contains(*shell, "${{") {
		return nil
	}
	if !isPythonShell(*shell) {
		return nil
	}
	if c.backend == nil {
		if err := c.validateCommand(location); err != nil {
			return err
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
	templates := strings.Contains(script, "${{")
	if c.backend != nil && templates && backendTemplateContract(c.backend) != analyzer.TemplateEffectsContract {
		return fmt.Errorf("ruff expression masking requires template contract %q", analyzer.TemplateEffectsContract)
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
	var directoryInfo os.FileInfo
	if c.backend == nil {
		directoryInfo, err = os.Stat(directory)
		if err != nil {
			return fmt.Errorf("ruff stdin directory for script at %s: %w", location, err)
		}
		if !directoryInfo.IsDir() {
			return fmt.Errorf("ruff stdin directory for script at %s is not a directory: %q", location, directory)
		}
	}
	if config.TargetVersion == "" {
		config.TargetVersion = pythonShellTarget(*shell)
	}
	deliver := func(diagnostics []Diagnostic, err error) error {
		if err != nil && len(diagnostics) == 0 {
			return fmt.Errorf("ruff failed while checking Python script at %s: %w", location, err)
		}

		for i := range diagnostics {
			diagnostic := &diagnostics[i]
			if diagnostic.Code == "" {
				diagnostic.Code = diagnostic.Name
			}
			if c.backend != nil {
				contract := backendTemplateContract(c.backend)
				if contract == analyzer.TemplateEffectsContract && !diagnostic.TemplateEffect.Valid() ||
					contract != analyzer.TemplateEffectsContract && diagnostic.TemplateEffect != "" {
					return fmt.Errorf("ruff backend returned undeclared template effect %q", diagnostic.TemplateEffect)
				}
				if diagnostic.Filename != stdinFilename || diagnostic.Code == "" || diagnostic.Message == "" || diagnostic.Location.Row < 1 || diagnostic.Location.Column < 1 {
					return fmt.Errorf("invalid backend diagnostic for %s: %+v", stdinFilename, diagnostic)
				}
				continue
			}
			parent, err := os.Stat(filepath.Dir(diagnostic.Filename))
			if !filepath.IsAbs(diagnostic.Filename) || filepath.Base(diagnostic.Filename) != stdinFilename || err != nil || !os.SameFile(directoryInfo, parent) {
				return fmt.Errorf("ruff output for script at %s refers to unexpected file %q; expected %q in %q", location, diagnostic.Filename, stdinFilename, directory)
			}
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		for _, d := range diagnostics {
			if !placeholders.accept(d) {
				continue
			}
			report(d)
		}
		return err
	}
	if c.backend != nil {
		options, err := engineOptions(config)
		if err != nil {
			return err
		}
		encoded, err := json.Marshal(options)
		if err != nil {
			return err
		}
		request := analyzer.ScriptRequest{Source: source, Filename: stdinFilename, Config: encoded}
		c.pending = append(c.pending, func(ctx context.Context) ([]Diagnostic, error) {
			if validator, ok := c.backend.(analyzer.ScriptConfigValidator); ok {
				if err := validator.ValidateConfig(ctx, request.Config); err != nil {
					return nil, err
				}
			}
			if validator, ok := c.backend.(analyzer.ScriptTemplateValidator); ok && templates {
				if available, err := validator.ValidateTemplates(ctx); err != nil || !available {
					return nil, err
				}
			}
			findings, err := c.backend.Check(ctx, request)
			return nil, deliver(findings, err)
		})
	} else {
		defaults := arguments(config)
		native, err := nativeArguments(config)
		if err != nil {
			return err
		}
		args := make([]string, 1, len(defaults)+len(c.flags))
		args[0] = "check"
		args = append(args, native...)
		args = append(args, c.flags...)
		args = append(args, defaults[1:]...)
		c.run(args, source, func(stdout []byte, err error) error {
			if err != nil {
				return deliver(nil, err)
			}
			findings, decodeErr := Decode(stdout)
			return deliver(findings, errors.Join(err, decodeErr))
		})
	}
	return nil
}

func isPythonShell(shell string) bool {
	command, args := pythonShellCommand(shell)
	if command == "" {
		return false
	}
	if isPythonCommand(command) {
		return true
	}
	return pythonAdapterArguments(args, command) != nil
}

// Parse the executable separately to preserve Windows path backslashes.
func pythonShellCommand(shell string) (command, args string) {
	shell = strings.TrimSpace(shell)
	if shell == "" {
		return "", ""
	}
	if quote := shell[0]; quote == '"' || quote == '\'' {
		end := strings.IndexByte(shell[1:], quote)
		if end < 0 {
			return "", ""
		}
		end++
		if end+1 < len(shell) && !unicode.IsSpace(rune(shell[end+1])) {
			return "", ""
		}
		return shell[1:end], shell[end+1:]
	}
	end := strings.IndexFunc(shell, unicode.IsSpace)
	if end < 0 {
		return shell, ""
	}
	return shell[:end], shell[end:]
}

func pythonAdapterArguments(arguments, command string) []string {
	name := strings.ToLower(command[strings.LastIndexAny(command, `/\`)+1:])
	name = strings.TrimSuffix(name, ".cmd")
	if name != "actions-shell" && name != "actions-shells" {
		return nil
	}
	// The documented Python adapters take the runtime first and script last.
	parser := shellwords.NewParser()
	parser.ParseEnv, parser.ParseBacktick = false, false
	args, err := parser.Parse(arguments)
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
	executable, arguments := pythonShellCommand(shell)
	if executable == "" {
		return ""
	}
	words := append([]string{executable}, strings.Fields(arguments)...)
	if args := pythonAdapterArguments(arguments, executable); args != nil {
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
		if version, err := strconv.Atoi(minor); err == nil && version >= 0 && strconv.Itoa(version) == minor {
			return "py3" + minor
		}
	}
	return ""
}

func arguments(config Config) []string {
	selectRules, ignoreRules := config.Select, config.Ignore
	if selectRules != nil && len(selectRules) == 0 {
		// An empty --select is invalid. Equal-specificity ALL selectors disable
		// lint codes without suppressing Python syntax errors.
		selectRules, ignoreRules = []string{"ALL"}, []string{"ALL"}
	}
	args := []string{"check", "--isolated", "--ignore-noqa", "--no-fix", "--no-cache", "--output-format", "json", "--stdin-filename", stdinFilename, "--extension", "py:python"}
	if config.TargetVersion != "" && config.Native["target-version"] == nil {
		args = append(args, "--target-version="+config.TargetVersion)
	}
	if selectRules != nil {
		args = append(args, "--select="+strings.Join(selectRules, ","))
	}
	if len(ignoreRules) > 0 {
		args = append(args, "--ignore="+strings.Join(ignoreRules, ","))
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
	identifiers        map[Position]int
	parentheses        map[Position]Position
	quoted             map[Position]string
	lines              map[int]bool
	sourceLines        []string
	percentConversions map[Position]bool
}

func (m *templateMasks) accept(d Diagnostic) bool {
	if d.TemplateEffect == analyzer.TemplateEffectLineLength && m.lines[d.Location.Row] {
		return false
	}
	// Opaque values, quoted annotations and exports may hide references.
	// Comment-only templates add no masks; unrelated checks still apply.
	if len(m.identifiers) > 0 && d.TemplateEffect == analyzer.TemplateEffectCompleteReferences {
		return false
	}
	if m.dynamicQuotedValue(d) {
		return false
	}
	// A mask can manufacture undefined-name and useless-expression
	// findings. Only its exact identifier range is excluded.
	if width, ok := m.identifiers[d.Location]; ok && d.TemplateEffect == analyzer.TemplateEffectOpaqueName &&
		d.EndLocation == (Position{Row: d.Location.Row, Column: d.Location.Column + width}) {
		return false
	}
	// UP034 can identify the wrapper added around an opaque value.
	// Source parentheses extend outside this exact generated range.
	if end, ok := m.parentheses[d.Location]; ok && d.TemplateEffect == analyzer.TemplateEffectOpaqueParentheses && d.EndLocation == end {
		return false
	}
	return true
}

func (m *templateMasks) dynamicQuotedValue(d Diagnostic) bool {
	// Filter content-dependent findings only when their range contains a quoted mask.
	contents := d.TemplateEffect == analyzer.TemplateEffectStringContents
	summary := d.TemplateEffect == analyzer.TemplateEffectDocstringSummary
	export := d.TemplateEffect == analyzer.TemplateEffectExportedName
	conversion := d.TemplateEffect == analyzer.TemplateEffectPercentConversion
	if !contents && !summary && !export && !conversion {
		return false
	}
	for start, name := range m.quoted {
		end := Position{Row: start.Row, Column: start.Column + len(name)}
		if (d.Location.Row < start.Row || d.Location.Row == start.Row && d.Location.Column <= start.Column) &&
			(d.EndLocation.Row > end.Row || d.EndLocation.Row == end.Row && d.EndLocation.Column >= end.Column) &&
			(contents || summary && m.inFirstParagraph(d.Location.Row, start.Row) || export || conversion && m.percentConversions[start]) {
			return true
		}
	}
	return false
}

// Format-field parsing and argument consistency depend on the complete string.
// Percent conversion characters and docstring layout are handled separately
// so static mistakes surrounding a template remain visible.
func (m *templateMasks) inFirstParagraph(first, line int) bool {
	for row := first + 1; row < line; row++ {
		if row <= len(m.sourceLines) && strings.TrimSpace(m.sourceLines[row-1]) == "" {
			return false
		}
	}
	return true
}

func templateProvidesPercentConversion(prefix string) bool {
	index := strings.LastIndexByte(prefix, '%')
	if index < 0 {
		return false
	}
	count := 1
	for offset := index - 1; offset >= 0 && prefix[offset] == '%'; offset-- {
		count++
	}
	if count%2 == 0 {
		return false
	}
	for _, char := range prefix[index+1:] {
		if !strings.ContainsRune("#0- +.123456789*hlL", char) {
			return false
		}
	}
	return true
}

// Ruff accepts either a file path or an inline TOML assignment for --config.
// Existing files take precedence, even when their names contain an equals sign.
// Ruff itself validates the assignment's key and value after this boundary check.
func ruffInlineConfig(value, directory string) bool {
	path := value
	if !filepath.IsAbs(path) {
		path = filepath.Join(directory, path)
	}
	if _, err := os.Stat(path); err == nil {
		return false
	}
	var quote rune
	escaped := false
	for _, char := range value {
		if quote != 0 {
			switch {
			case escaped:
				escaped = false
			case char == '\\' && quote == '"':
				escaped = true
			case char == quote:
				quote = 0
			}
			continue
		}
		switch char {
		case '\'', '"':
			quote = char
		case '=':
			return true
		case '#', '\n', '\r':
			return false
		}
	}
	return false
}

func sanitize(src string, expressionEnd ExpressionEnd, placeholders *templateMasks) (string, bool, error) {
	var out strings.Builder
	var state pythonLexicalState
	// Python normalizes identifiers, so Unicode aliases also reserve names.
	original := norm.NFKC.String(src)
	if placeholders != nil {
		placeholders.sourceLines = strings.Split(src, "\n")
		placeholders.percentConversions = make(map[Position]bool)
	}
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
