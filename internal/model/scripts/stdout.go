package scripts

import (
	"strconv"
	"strings"

	"actionlint.kjanat.dev/v2/internal/model/commands"
	"mvdan.cc/sh/v3/syntax"
)

// LiteralOutput is one stdout line with the range of its first contributing
// arguments. Several writes can contribute to the same line.
type LiteralOutput struct {
	ScriptRange
	Text string
}

// LiteralStdoutPaths extracts supported prefixes along possible stdout paths.
// The caller evaluates workflow-command state separately for each path.
// Unsupported commands stop that path because unknown output may disable
// runner command processing.
// Strings that are never emitted do not become output.
func LiteralStdoutPaths(text string, invocation commands.Invocation, registry *commands.Registry) [][]LiteralOutput {
	if invocation.Status != commands.Valid || invocation.Input != "file" || strings.Contains(text, "${{") {
		return nil
	}
	language := invocation.Language
	errexit := false
	noexec, onecmd := false, false
	for _, setting := range invocation.Settings {
		if setting.Group == "set" && setting.Name == "errexit" {
			errexit = setting.Enabled
		}
		if setting.Group == "set" && setting.Name == "noexec" {
			noexec = setting.Enabled
		}
		if setting.Group == "set" && setting.Name == "onecmd" {
			onecmd = setting.Enabled
		}
	}
	if noexec || onecmd {
		return nil
	}
	switch language {
	case "bash", "sh", "dash", "ksh":
		variant := syntax.LangBash
		if language != "bash" {
			variant = syntax.LangPOSIX
		}
		file, err := syntax.NewParser(syntax.Variant(variant)).Parse(strings.NewReader(text), "")
		if err != nil {
			return nil
		}
		analyzer := stdoutAnalyzer{registry: registry, definition: invocation.DefinitionID, substitutions: make(map[*syntax.CmdSubst]stdoutStatus)}
		paths := analyzer.statements(file.Stmts, []stdoutPath{{errexit: errexit, status: stdoutSuccess}}, false)
		var output [][]LiteralOutput
		for _, path := range paths {
			output = append(output, literalOutputLines(path.output, path.state != stdoutUnknown))
		}
		return output
	case "python", "pwsh", "powershell":
		return [][]LiteralOutput{literalOutputLines(literalLineOutput(text, language), true)}
	}
	return nil
}

type stdoutPathState uint8

const (
	stdoutActive stdoutPathState = iota
	stdoutFinished
	stdoutUnknown
)

type stdoutStatus uint8

const (
	stdoutStatusUnknown stdoutStatus = iota
	stdoutSuccess
	stdoutFailure
)

type stdoutPath struct {
	output  []LiteralOutput
	state   stdoutPathState
	errexit bool
	status  stdoutStatus
}
type stdoutAnalyzer struct {
	registry      *commands.Registry
	definition    string
	substitutions map[*syntax.CmdSubst]stdoutStatus
}

func (a stdoutAnalyzer) statements(stmts []*syntax.Stmt, paths []stdoutPath, ignored bool) []stdoutPath {
	for _, stmt := range stmts {
		var next []stdoutPath
		for _, path := range paths {
			if path.state != stdoutActive {
				next = append(next, path)
				continue
			}
			next = append(next, a.statement(stmt, path, ignored)...)
		}
		paths = next
		if len(paths) > 32 {
			// Bound path growth. Return completed lines from the analyzed
			// prefixes; stop before later statements.
			for i := range paths {
				paths[i].state = stdoutUnknown
			}
			return paths
		}
	}
	return paths
}

func (a stdoutAnalyzer) statement(stmt *syntax.Stmt, path stdoutPath, ignored bool) []stdoutPath {
	paths := a.command(stmt, path, ignored || stmt.Negated)
	if stmt.Negated {
		for i := range paths {
			if paths[i].state != stdoutActive {
				continue
			}
			switch paths[i].status {
			case stdoutStatusUnknown:
				continue
			case stdoutSuccess:
				paths[i].status = stdoutFailure
			case stdoutFailure:
				paths[i].status = stdoutSuccess
			}
		}
	}
	return paths
}

func (a stdoutAnalyzer) command(stmt *syntax.Stmt, path stdoutPath, ignored bool) []stdoutPath {
	stop := func() []stdoutPath { path.state = stdoutUnknown; return []stdoutPath{path} }
	if stmt.Background || stmt.Coprocess || len(stmt.Redirs) != 0 {
		return stop()
	}
	switch cmd := stmt.Cmd.(type) {
	case *syntax.Block:
		return a.statements(cmd.Stmts, []stdoutPath{path}, ignored)
	case *syntax.Subshell:
		paths := a.statements(cmd.Stmts, []stdoutPath{path}, ignored)
		for i := range paths {
			paths[i].errexit = path.errexit
			if paths[i].state != stdoutUnknown {
				if paths[i].state == stdoutFinished {
					paths[i].state = stdoutActive
				}
				if !ignored && path.errexit && paths[i].status == stdoutFailure {
					paths[i].state = stdoutFinished
				}
			}
		}
		return paths
	case *syntax.TestClause, *syntax.ArithmCmd:
		if path.errexit && !ignored {
			return stop()
		}
		path.status = stdoutStatusUnknown
		return []stdoutPath{path}
	case *syntax.BinaryCmd:
		if cmd.Op != syntax.AndStmt && cmd.Op != syntax.OrStmt {
			return stop()
		}
		var paths []stdoutPath
		for _, left := range a.statement(cmd.X, path, true) {
			if left.state != stdoutActive {
				paths = append(paths, left)
				continue
			}
			runRight := left.status == stdoutSuccess && cmd.Op == syntax.AndStmt || left.status == stdoutFailure && cmd.Op == syntax.OrStmt
			if runRight || left.status == stdoutStatusUnknown {
				right := left
				right.output = append([]LiteralOutput(nil), left.output...)
				paths = append(paths, a.statement(cmd.Y, right, ignored)...)
			}
			if !runRight {
				paths = append(paths, left)
			}
		}
		return paths
	case *syntax.IfClause:
		if len(cmd.Cond) == 0 {
			return a.statements(cmd.Then, []stdoutPath{path}, ignored)
		}
		conditions := a.statements(cmd.Cond, []stdoutPath{path}, true)
		var branches []stdoutPath
		for _, condition := range conditions {
			if condition.state != stdoutActive {
				branches = append(branches, condition)
				continue
			}
			// Detach branch storage: appending one path must not overwrite
			// the output of its sibling.
			yes, no := condition, condition
			yes.output = append([]LiteralOutput(nil), condition.output...)
			no.output = append([]LiteralOutput(nil), condition.output...)
			if condition.status != stdoutFailure {
				branches = append(branches, a.statements(cmd.Then, []stdoutPath{yes}, ignored)...)
			}
			if condition.status != stdoutSuccess {
				if cmd.Else == nil {
					no.status = stdoutSuccess
					branches = append(branches, no)
				} else {
					branches = append(branches, a.statement(&syntax.Stmt{Cmd: cmd.Else}, no, ignored)...)
				}
			}
		}
		return branches
	case *syntax.CallExpr:
		if len(cmd.Args) == 0 {
			// Assignment command substitutions capture stdout in the value;
			// their source strings are not direct runner output.
			path.status = a.assignmentStatus(cmd)
			if path.status == stdoutFailure && path.errexit && !ignored {
				path.state = stdoutFinished
			}
			return []stdoutPath{path}
		}
		name := callName(cmd)
		if name == "exit" {
			if len(cmd.Args) > 2 {
				return stop()
			}
			if len(cmd.Args) == 2 {
				value, known := LiteralShellWord(cmd.Args[1].Parts)
				code, err := strconv.Atoi(value)
				if !known || err != nil {
					return stop()
				}
				path.status = stdoutSuccess
				if code%256 != 0 {
					path.status = stdoutFailure
				}
			}
			path.state = stdoutFinished
			return []stdoutPath{path}
		}
		if name == "set" {
			if a.registry == nil {
				return stop()
			}
			var tokens []commands.Token
			for _, arg := range cmd.Args[1:] {
				value, known := LiteralShellWord(arg.Parts)
				if !known {
					return stop()
				}
				tokens = append(tokens, commands.Token{Value: value})
			}
			tokens = append(tokens, commands.Token{Value: "{0}"})
			invocation := a.registry.Validate(a.definition, tokens)
			if invocation.Status != commands.Valid {
				return stop()
			}
			for _, setting := range invocation.Settings {
				if setting.Group == "set" && setting.Name == "errexit" {
					path.errexit = setting.Enabled
				}
				if setting.Group == "set" && (setting.Name == "noexec" || setting.Name == "onecmd") && setting.Enabled {
					path.state = stdoutFinished
				}
			}
			// `set` without options writes shell variables to stdout.
			if len(cmd.Args) == 1 {
				return stop()
			}
			path.status = stdoutSuccess
			return []stdoutPath{path}
		}
		if name == "false" {
			path.status = stdoutFailure
			if path.errexit && !ignored {
				path.state = stdoutFinished
			}
			return []stdoutPath{path}
		}
		if name == "test" || name == "[" {
			if path.errexit && !ignored {
				return stop()
			}
			path.status = stdoutStatusUnknown
			return []stdoutPath{path}
		}
		out, _, known := outputCall(cmd, nil)
		if !known {
			return stop()
		}
		if out != "" {
			start, end := cmd.Pos(), cmd.End()
			if len(cmd.Args) > 1 {
				start = cmd.Args[1].Pos()
			}
			path.output = append(path.output, LiteralOutput{ScriptRange{int(start.Offset()), int(end.Offset())}, out})
		}
		path.status = stdoutSuccess
		return []stdoutPath{path}
	}
	return stop()
}

func (a stdoutAnalyzer) assignmentStatus(call *syntax.CallExpr) stdoutStatus {
	status := stdoutSuccess
	syntax.Walk(call, func(node syntax.Node) bool {
		switch node := node.(type) {
		case *syntax.ProcSubst:
			return false
		case *syntax.ParamExp:
			// A default-value expansion might not evaluate its substitution.
			syntax.Walk(node, func(child syntax.Node) bool {
				if _, ok := child.(*syntax.CmdSubst); ok {
					status = stdoutStatusUnknown
					return false
				}
				return true
			})
			return false
		case *syntax.CmdSubst:
			status = a.substitutionStatus(node)
			return false
		}
		return true
	})
	return status
}

func (a stdoutAnalyzer) substitutionStatus(substitution *syntax.CmdSubst) (status stdoutStatus) {
	if cached, ok := a.substitutions[substitution]; ok {
		return cached
	}
	defer func() { a.substitutions[substitution] = status }()
	// Bash normally clears errexit in command substitutions, while POSIX mode
	// and inherit_errexit preserve it. A failure is known here only when both
	// policies agree; outputs and option changes stay inside the substitution.
	for _, errexit := range []bool{false, true} {
		for _, path := range a.statements(substitution.Stmts, []stdoutPath{{errexit: errexit, status: stdoutSuccess}}, false) {
			if path.state == stdoutUnknown || path.status == stdoutStatusUnknown {
				return stdoutStatusUnknown
			}
			if status == stdoutStatusUnknown {
				status = path.status
			} else if status != path.status {
				return stdoutStatusUnknown
			}
		}
	}
	return status
}

// literalOutputLines follows the runner's line-oriented stdout stream. A partial
// final line is observable at known EOF, but not before unknown output that might
// finish the line. The first contributing source span identifies a joined line.
func literalOutputLines(fragments []LiteralOutput, eof bool) []LiteralOutput {
	var lines []LiteralOutput
	var pending strings.Builder
	var span ScriptRange
	afterCR := false
	for _, fragment := range fragments {
		text := fragment.Text
		for text != "" {
			if afterCR {
				afterCR = false
				if text[0] == '\n' {
					text = text[1:]
					continue
				}
			}
			if pending.Len() == 0 {
				span = fragment.ScriptRange
			}
			index := strings.IndexAny(text, "\r\n")
			if index < 0 {
				pending.WriteString(text)
				break
			}
			pending.WriteString(text[:index])
			lines = append(lines, LiteralOutput{span, pending.String()})
			pending.Reset()
			afterCR = text[index] == '\r'
			text = text[index+1:]
		}
	}
	if eof && pending.Len() != 0 {
		lines = append(lines, LiteralOutput{span, pending.String()})
	}
	return lines
}

// Python and PowerShell are deliberately limited to flat literal print calls.
// A multiline string, expansion, indentation or unrecognized statement makes
// the script unknown. These interpreters parse the whole script before executing
// it, so an unsupported trailing statement might prevent earlier output too.
func literalLineOutput(text, language string) []LiteralOutput {
	if strings.Contains(text, `"""`) || strings.Contains(text, "'''") || strings.Contains(text, "@'") || strings.Contains(text, "@\"") {
		return nil
	}
	var output []LiteralOutput
	offset := 0
	for _, raw := range strings.SplitAfter(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			offset += len(raw)
			continue
		}
		if raw[0] == ' ' || raw[0] == '\t' {
			return nil
		}
		var argument string
		suffix := ""
		if language == "python" {
			if !strings.HasPrefix(line, "print(") {
				return nil
			}
			argument, suffix = strings.TrimSpace(line[len("print("):]), ")"
		} else {
			name, rest, found := strings.Cut(line, " ")
			if !found {
				return nil
			}
			switch strings.ToLower(name) {
			case "echo", "write-host", "write-output":
				argument = strings.TrimSpace(rest)
			default:
				return nil
			}
		}
		value, used, known := flatString(argument, language)
		if !known {
			return nil
		}
		rest := strings.TrimSpace(argument[used:])
		if suffix != "" {
			if !strings.HasPrefix(rest, suffix) {
				return nil
			}
			rest = strings.TrimSpace(rest[len(suffix):])
		}
		if rest != "" && !strings.HasPrefix(rest, "#") {
			return nil
		}
		start := offset + strings.Index(raw, argument)
		output = append(output, LiteralOutput{ScriptRange{start, start + used}, value + "\n"})
		offset += len(raw)
	}
	return output
}

func flatString(text, language string) (string, int, bool) {
	if len(text) < 2 || text[0] != '\'' && text[0] != '"' {
		return "", 0, false
	}
	quote := text[0]
	var value strings.Builder
	for i := 1; i < len(text); i++ {
		c := text[i]
		if c == quote {
			if language != "python" && quote == '\'' && i+1 < len(text) && text[i+1] == quote {
				value.WriteByte(quote)
				i++
				continue
			}
			return value.String(), i + 1, true
		}
		if c == '\\' && language == "python" {
			if i+1 >= len(text) {
				return "", 0, false
			}
			i++
			switch text[i] {
			case '\\', '\'', '"':
				value.WriteByte(text[i])
			case 'n':
				value.WriteByte('\n')
			case 'r':
				value.WriteByte('\r')
			case 't':
				value.WriteByte('\t')
			default:
				return "", 0, false
			}
			continue
		}
		if language != "python" && quote == '"' && (c == '$' || c == '`') {
			return "", 0, false
		}
		value.WriteByte(c)
	}
	return "", 0, false
}
