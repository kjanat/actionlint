package scripts

import (
	"fmt"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// NamedFileCommandError checks complete NAME=value and NAME<<delimiter records.
// The runner uses this format for output, environment and state files.
// See actions/runner 1ba885c2065fd9be317808ab3fd406f392635cf7,
// src/Runner.Worker/FileCommandManager.cs, EnvFileKeyValuePairs.
func NamedFileCommandError(text string) string {
	// The runner removes CR before LF on Windows only. Since the platform is
	// unknown, report an error only if both newline conventions reject the file.
	unix := namedFileCommandError(text, false)
	if unix == "" || namedFileCommandError(text, true) == "" {
		return ""
	}
	return unix
}

func namedFileCommandError(text string, windows bool) string {
	readLine := func() (string, bool) {
		if text == "" {
			return "", false
		}
		line, rest, found := strings.Cut(text, "\n")
		text = rest
		if windows && found {
			line = strings.TrimSuffix(line, "\r")
		}
		return line, true
	}
	for {
		line, exists := readLine()
		if !exists {
			return ""
		}
		if line == "" {
			continue
		}
		equal, heredoc := strings.IndexByte(line, '='), strings.Index(line, "<<")
		switch {
		case equal >= 0 && (heredoc < 0 || equal < heredoc):
			// The runner accepts an empty key in NAME=value records.
			continue
		case heredoc >= 0 && (equal < 0 || heredoc < equal):
			name, delimiter := line[:heredoc], line[heredoc+2:]
			if name == "" || delimiter == "" {
				return "a multiline record has an empty name or delimiter"
			}
			for {
				value, exists := readLine()
				if !exists {
					return fmt.Sprintf("multiline record %q has no matching delimiter %q", name, delimiter)
				}
				if value == delimiter {
					break
				}
			}
		default:
			return fmt.Sprintf("line %q is neither NAME=value nor NAME<<delimiter", line)
		}
	}
}

// OutputFileContents holds the final output-file text determined from a script.
// Start and End are byte offsets for the destination word of the write that
// starts the final contents. An overwrite discards earlier writes.
type OutputFileContents struct {
	Text       string
	Start, End int
}

type outputWrite struct {
	text       string
	append     bool
	start, end int
}

// KnownOutputFile determines the output-file contents for a supported Bash or sh
// script. It handles literal echo/printf, cat/tee with literal text, pipes and
// command groups. Unsupported commands, assignments, expansions or control flow
// make the result unknown. It returns the combined writes so the caller can
// check records assembled by several commands, including multiline values.
// Each step starts with a fresh, empty command file, including when appending.
func KnownOutputFile(text, shell string) (OutputFileContents, bool) {
	if strings.ContainsAny(shell, " \t\r\n") {
		// A custom shell template may run the script differently.
		return OutputFileContents{}, false
	}
	variant := syntax.LangBash
	switch ShellExecutableName(shell) {
	case "bash":
	case "sh":
		variant = syntax.LangPOSIX
	default:
		return OutputFileContents{}, false
	}
	if strings.Contains(text, "${{") {
		return OutputFileContents{}, false
	}
	file, err := syntax.NewParser(syntax.Variant(variant)).Parse(strings.NewReader(text), "")
	if err != nil {
		return OutputFileContents{}, false
	}
	var all []outputWrite
	for _, stmt := range file.Stmts {
		_, writes, ok := outputStatement(stmt, nil)
		if !ok {
			return OutputFileContents{}, false
		}
		all = append(all, writes...)
	}
	var result OutputFileContents
	for i, write := range all {
		if i == 0 || !write.append {
			result = OutputFileContents{Text: write.text, Start: write.start, End: write.end}
		} else {
			result.Text += write.text
		}
	}
	return result, len(all) != 0
}

func outputStatement(stmt *syntax.Stmt, input *string) (string, []outputWrite, bool) {
	if stmt.Background || stmt.Negated || stmt.Coprocess {
		return "", nil, false
	}
	var destination *syntax.Redirect
	for _, redirect := range stmt.Redirs {
		switch redirect.Op {
		case syntax.Hdoc, syntax.DashHdoc:
			if redirect.N != nil && redirect.N.Value != "0" {
				return "", nil, false
			}
			var ok bool
			body, known := literalHeredoc(redirect)
			ok = known
			input = &body
			if !ok {
				return "", nil, false
			}
		case syntax.RdrOut, syntax.AppOut:
			if redirect.N != nil && redirect.N.Value != "1" || destination != nil || !outputFileWord(redirect.Word) {
				return "", nil, false
			}
			destination = redirect
		case syntax.RdrIn, syntax.RdrInOut, syntax.DplIn, syntax.DplOut, syntax.RdrClob,
			syntax.AppClob, syntax.WordHdoc, syntax.RdrAll, syntax.RdrAllClob, syntax.AppAll, syntax.AppAllClob:
			return "", nil, false
		default:
			return "", nil, false
		}
	}
	var stdout string
	var writes []outputWrite
	var ok bool
	switch command := stmt.Cmd.(type) {
	case *syntax.CallExpr:
		stdout, writes, ok = outputCall(command, input)
	case *syntax.Block:
		if input != nil {
			return "", nil, false
		}
		ok = true
		var blockOutput strings.Builder
		for _, child := range command.Stmts {
			out, childWrites, supported := outputStatement(child, input)
			if !supported {
				return "", nil, false
			}
			if destination != nil && len(childWrites) != 0 {
				return "", nil, false
			}
			blockOutput.WriteString(out)
			writes = append(writes, childWrites...)
		}
		stdout = blockOutput.String()
	case *syntax.BinaryCmd:
		if command.Op != syntax.Pipe {
			return "", nil, false
		}
		var leftWrites []outputWrite
		stdout, leftWrites, ok = outputStatement(command.X, input)
		if !ok {
			return "", nil, false
		}
		if len(leftWrites) != 0 {
			// Both sides of a pipe may write at the same time, in an unknown order.
			return "", nil, false
		}
		stdout, writes, ok = outputStatement(command.Y, &stdout)
	default:
		return "", nil, false
	}
	if !ok {
		return "", nil, false
	}
	if destination != nil {
		if len(writes) != 0 {
			return "", nil, false
		}
		writes = append(writes, outputWrite{
			text: stdout, append: destination.Op == syntax.AppOut,
			start: int(destination.Word.Pos().Offset()), end: int(destination.Word.End().Offset()),
		})
		stdout = ""
	}
	return stdout, writes, true
}

func outputCall(call *syntax.CallExpr, input *string) (string, []outputWrite, bool) {
	if len(call.Assigns) != 0 || len(call.Args) == 0 {
		return "", nil, false
	}
	name, ok := LiteralShellWord(call.Args[0].Parts)
	if !ok {
		return "", nil, false
	}
	args := call.Args[1:]
	if name == "tee" {
		if input == nil {
			return "", nil, false
		}
		appendFile := false
		if len(args) > 0 {
			if option, _ := LiteralShellWord(args[0].Parts); option == "-a" || option == "--append" {
				appendFile, args = true, args[1:]
			}
		}
		if len(args) == 0 {
			return *input, nil, true
		}
		if len(args) != 1 || !outputFileWord(args[0]) {
			return "", nil, false
		}
		return *input, []outputWrite{{text: *input, append: appendFile, start: int(args[0].Pos().Offset()), end: int(args[0].End().Offset())}}, true
	}
	var values []string
	for _, arg := range args {
		value, known := LiteralShellWord(arg.Parts)
		if !known {
			return "", nil, false
		}
		values = append(values, value)
	}
	switch name {
	case ":", "true":
		return "", nil, len(values) == 0
	case "cat":
		if input == nil {
			return "", nil, false
		}
		return *input, nil, len(values) == 0
	case "echo":
		// Shells and echo options differ in how they handle backslashes.
		for _, value := range values {
			if strings.ContainsRune(value, '\\') {
				return "", nil, false
			}
		}
		if len(values) > 0 && strings.HasPrefix(values[0], "-") {
			return "", nil, false
		}
		return strings.Join(values, " ") + "\n", nil, true
	case "printf":
		out, known := literalPrintf(values)
		return out, nil, known
	default:
		return "", nil, false
	}
}

func literalPrintf(values []string) (string, bool) {
	if len(values) == 0 || strings.HasPrefix(values[0], "-") {
		return "", false
	}
	format := values[0]
	if len(values) == 1 {
		var out strings.Builder
		for i := 0; i < len(format); i++ {
			switch format[i] {
			case '%':
				if i+1 >= len(format) || format[i+1] != '%' {
					return "", false
				}
				i++
				out.WriteByte('%')
			case '\\':
				if i+1 >= len(format) {
					return "", false
				}
				i++
				switch format[i] {
				case 'n':
					out.WriteByte('\n')
				case 'r':
					out.WriteByte('\r')
				case 't':
					out.WriteByte('\t')
				case '\\':
					out.WriteByte('\\')
				default:
					return "", false
				}
			default:
				out.WriteByte(format[i])
			}
		}
		return out.String(), true
	}
	// Bash and POSIX sh repeat these formats for each argument.
	if format == "%s" {
		return strings.Join(values[1:], ""), true
	}
	if format == "%s\\n" {
		return strings.Join(values[1:], "\n") + "\n", true
	}
	return "", false
}

func literalHeredoc(redirect *syntax.Redirect) (string, bool) {
	var out strings.Builder
	quoted := false
	for _, part := range redirect.Word.Parts {
		switch part.(type) {
		case *syntax.SglQuoted, *syntax.DblQuoted:
			quoted = true
		}
	}
	for _, part := range redirect.Hdoc.Parts {
		literal, ok := part.(*syntax.Lit)
		if !ok || !quoted && strings.ContainsRune(literal.Value, '\\') {
			return "", false
		}
		out.WriteString(literal.Value)
	}
	text := out.String()
	if redirect.Op == syntax.DashHdoc {
		lines := strings.Split(text, "\n")
		for i := range lines {
			lines[i] = strings.TrimLeft(lines[i], "\t")
		}
		text = strings.Join(lines, "\n")
	}
	return text, true
}

func outputFileWord(word *syntax.Word) bool {
	parts := word.Parts
	if len(parts) != 1 {
		return false
	}
	quoted, ok := parts[0].(*syntax.DblQuoted)
	if !ok || quoted.Dollar {
		// An unquoted path could split into words or expand wildcards.
		return false
	}
	parts = quoted.Parts
	if len(parts) != 1 {
		return false
	}
	parameter, ok := parts[0].(*syntax.ParamExp)
	return ok && parameter.Param != nil && parameter.Param.Value == "GITHUB_OUTPUT" &&
		!parameter.Excl && !parameter.Length && !parameter.Width && !parameter.IsSet &&
		parameter.Index == nil && parameter.Exp == nil && parameter.Repl == nil && parameter.Slice == nil &&
		parameter.Flags == nil && len(parameter.Modifiers) == 0
}
