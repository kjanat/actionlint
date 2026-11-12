package scripts

import (
	"strings"

	"actionlint.kjanat.dev/v2/internal/model/commands"
	"mvdan.cc/sh/v3/syntax"
)

// ScriptRange is a half-open byte range in decoded script text.
type ScriptRange struct{ Start, End int }

// ErrexitStatusReads finds status reads after simple commands that would exit
// on failure. It does not claim the read is unreachable after a successful command.
// Function bodies and command substitutions require call-site or inheritance
// analysis and are deliberately excluded. Bash is selected by its registered
// invocation, never by a substring in the executable name.
func ErrexitStatusReads(text string, invocation commands.Invocation, registry *commands.Registry) []ScriptRange {
	if registry == nil || invocation.Status != commands.Valid || invocation.Language != "bash" || invocation.Input != "file" || strings.Contains(text, "${{") {
		return nil
	}
	state := errexitState{known: true}
	noexec, onecmd := false, false
	for _, setting := range invocation.Settings {
		if setting.Group == "set" && setting.Name == "errexit" {
			state.enabled = setting.Enabled
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
	file, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(text), "")
	if err != nil {
		return nil
	}
	analyzer := statusAnalyzer{registry: registry, functions: make(map[string]bool)}
	syntax.Walk(file, func(node syntax.Node) bool {
		if fn, ok := node.(*syntax.FuncDecl); ok && fn.Name != nil {
			analyzer.functions[fn.Name.Value] = true
		}
		return true
	})
	analyzer.statements(file.Stmts, state, false)
	return analyzer.findings
}

type (
	errexitState   struct{ known, enabled, opaque bool }
	statusAnalyzer struct {
		registry  *commands.Registry
		functions map[string]bool
		findings  []ScriptRange
	}
)

func (a *statusAnalyzer) statements(stmts []*syntax.Stmt, state errexitState, ignored bool) errexitState {
	previous := false
	for _, stmt := range stmts {
		call, simple := stmt.Cmd.(*syntax.CallExpr)
		ordinary := !stmt.Negated && !stmt.Background && !stmt.Coprocess
		if simple && ordinary && previous && state.known && state.enabled && !ignored {
			name := callName(call)
			if name != "trap" {
				syntax.Walk(call, func(node syntax.Node) bool {
					switch node := node.(type) {
					case *syntax.CmdSubst, *syntax.ProcSubst:
						return false
					case *syntax.ParamExp:
						if node.Param != nil && node.Param.Value == "?" {
							a.findings = append(a.findings, ScriptRange{int(node.Pos().Offset()), int(node.End().Offset())})
						}
					}
					return true
				})
			}
		}
		next := a.statement(stmt, state, ignored || !ordinary)
		if !stmt.Background {
			state = next
		}
		previous = simple && ordinary && potentiallyFailing(call)
	}
	return state
}

func (a *statusAnalyzer) statement(stmt *syntax.Stmt, state errexitState, ignored bool) errexitState {
	switch cmd := stmt.Cmd.(type) {
	case *syntax.CallExpr:
		name := callName(cmd)
		if name == "set" {
			if state.opaque {
				return state
			}
			var tokens []commands.Token
			for _, arg := range cmd.Args[1:] {
				value, known := LiteralShellWord(arg.Parts)
				if !known {
					return errexitState{}
				}
				tokens = append(tokens, commands.Token{Value: value})
			}
			tokens = append(tokens, commands.Token{Value: "{0}"})
			invocation := a.registry.Validate("bash", tokens)
			if invocation.Status != commands.Valid {
				return errexitState{}
			}
			for _, setting := range invocation.Settings {
				if setting.Group == "set" && setting.Name == "errexit" {
					state = errexitState{known: true, enabled: setting.Enabled}
				}
				if setting.Group == "set" && setting.Name == "noexec" && setting.Enabled {
					return errexitState{opaque: true}
				}
			}
		}
		if a.functions[name] || name == "source" || name == "." || name == "eval" || name == "trap" || name == "alias" || name == "enable" || name == "builtin" || name == "command" || name == "" && len(cmd.Args) != 0 {
			return errexitState{opaque: true}
		}
	case *syntax.Block:
		return a.statements(cmd.Stmts, state, ignored)
	case *syntax.Subshell:
		a.statements(cmd.Stmts, state, ignored)
	case *syntax.BinaryCmd:
		// An AND/OR list can change options along only one path. A pipeline runs
		// in subshells except for lastpipe, which is not inferred here.
		left := a.statement(cmd.X, state, true)
		right := a.statement(cmd.Y, left, true)
		if left != state || right != state {
			return errexitState{opaque: left.opaque || right.opaque}
		}
	case *syntax.IfClause:
		condition := a.statements(cmd.Cond, state, true)
		yes := a.statements(cmd.Then, condition, ignored)
		no := condition
		if cmd.Else != nil {
			no = a.statement(&syntax.Stmt{Cmd: cmd.Else}, condition, ignored)
		}
		if yes != no {
			return errexitState{opaque: yes.opaque || no.opaque}
		}
		return yes
	case *syntax.WhileClause:
		// An option change can affect the next iteration or a zero-iteration
		// exit. Analyze only loops without operations that might change options.
		if a.changesOptions(cmd) {
			return errexitState{opaque: true}
		}
		a.statements(cmd.Cond, state, true)
		a.statements(cmd.Do, state, ignored)
	case *syntax.ForClause:
		if a.changesOptions(cmd) {
			return errexitState{opaque: true}
		}
		a.statements(cmd.Do, state, ignored)
	case *syntax.CaseClause:
		if a.changesOptions(cmd) {
			return errexitState{opaque: true}
		}
		for _, item := range cmd.Items {
			a.statements(item.Stmts, state, ignored)
		}
	case *syntax.FuncDecl:
		// The function can be called in a context that ignores errexit.
	default:
		return errexitState{opaque: true}
	}
	return state
}

func callName(call *syntax.CallExpr) string {
	if len(call.Args) == 0 {
		return ""
	}
	name, _ := LiteralShellWord(call.Args[0].Parts)
	return name
}

func potentiallyFailing(call *syntax.CallExpr) bool {
	if len(call.Args) != 0 {
		switch callName(call) {
		case ":", "true", "set", "trap":
			return false
		}
		return true
	}
	found := false
	syntax.Walk(call, func(node syntax.Node) bool {
		if _, ok := node.(*syntax.CmdSubst); ok {
			found = true
		}
		return !found
	})
	return found
}

func (a *statusAnalyzer) changesOptions(node syntax.Node) bool {
	found := false
	syntax.Walk(node, func(node syntax.Node) bool {
		if call, ok := node.(*syntax.CallExpr); ok {
			if a.functions[callName(call)] || callName(call) == "" && len(call.Args) != 0 {
				found = true
			}
			switch callName(call) {
			case "set", "eval", ".", "source", "trap", "alias", "enable", "builtin", "command":
				found = true
			}
		}
		return !found
	})
	return found
}
