package actionlint

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

var deprecatedCommandsPattern = regexp.MustCompile(`(?:::(save-state|set-output|set-env)\s+name=[a-zA-Z][a-zA-Z_-]*::\S+|::(add-path)::\S+)`)

// RuleDeprecatedCommands is a rule checker to detect deprecated workflow commands. Currently
// 'set-state', 'set-output', `set-env' and 'add-path' are detected as deprecated.
//
// - https://github.blog/changelog/2020-10-01-github-actions-deprecating-set-env-and-add-path-commands/
// - https://github.blog/changelog/2022-10-11-github-actions-deprecating-save-state-and-set-output-commands/
type RuleDeprecatedCommands struct {
	RuleBase
}

// NewRuleDeprecatedCommands creates a new RuleDeprecatedCommands instance.
func NewRuleDeprecatedCommands() *RuleDeprecatedCommands {
	return &RuleDeprecatedCommands{
		RuleBase: builtinRuleBase("deprecated-commands"),
	}
}

// VisitStep is callback when visiting Step node.
func (rule *RuleDeprecatedCommands) VisitStep(n *Step) error {
	if r, ok := n.Exec.(*ExecRun); ok && r.Run != nil {
		for _, m := range deprecatedCommandsPattern.FindAllStringSubmatchIndex(r.Run.Value, -1) {
			start, end := m[2], m[3]
			if start < 0 {
				start, end = m[4], m[5]
			}
			c := r.Run.Value[start:end]

			var a string
			switch c {
			case "set-output":
				a = `echo "{name}={value}" >> $GITHUB_OUTPUT`
			case "save-state":
				a = `echo "{name}={value}" >> $GITHUB_STATE`
			case "set-env":
				a = `echo "{name}={value}" >> $GITHUB_ENV`
			case "add-path":
				a = `echo "{path}" >> $GITHUB_PATH`
			default:
				panic("unreachable")
			}

			position := func(offset int, exclusive bool) *Pos {
				prefix := r.Run.Value[:offset]
				line := strings.Count(prefix, "\n") + 1
				column := utf8.RuneCountInString(prefix[strings.LastIndexByte(prefix, '\n')+1:]) + 1
				if exclusive {
					p, _ := r.source.endPos(line, column)
					return p
				}
				p, _ := r.source.pos(line, column)
				return p
			}
			pos, endPos := position(start, false), position(end, true)
			if pos == nil {
				pos, endPos = r.Run.Pos, nil
			}
			rule.errorfRange(
				pos, endPos,
				"workflow command %q was deprecated. use `%s` instead: https://docs.github.com/en/actions/using-workflows/workflow-commands-for-github-actions",
				c,
				a,
			)
		}
	}
	return nil
}
