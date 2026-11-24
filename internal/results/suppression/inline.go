package suppression

import (
	"slices"
	"strconv"
	"strings"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/languages/messages"
	"actionlint.kjanat.dev/v2/internal/model/rule"
	"actionlint.kjanat.dev/v2/internal/results"
)

type Directive struct {
	Position sourcetext.Position
	Lines    sourcetext.LineRange
	Rules    []string
	Reason   string
}

func ParseInlineSuppressions(source []byte, rules []string, names rule.Names) ([]Directive, []results.Diagnostic) {
	var directives []Directive
	var findings []results.Diagnostic
	for _, comment := range collectInlineSuppressionComments(source) {
		if words := strings.Fields(comment.text); len(words) > 0 && words[0] == "ignore" {
			if len(words) == 1 || !slices.ContainsFunc(rules, func(candidate string) bool {
				first, _, _ := strings.Cut(words[1], ",")
				return names.ResolveAlias(candidate) == names.ResolveAlias(first)
			}) {
				continue
			}
		}
		directive, err := parseInlineSuppression(comment, rules, names)
		if err != nil {
			findings = append(findings, *err)
		} else {
			directives = append(directives, directive)
		}
	}
	return directives, findings
}

func parseInlineSuppression(comment inlineSuppressionComment, allowed []string, index rule.Names) (Directive, *results.Diagnostic) {
	invalid := func(message string) *results.Diagnostic {
		return &results.Diagnostic{Start: comment.pos, Rule: "inline-suppression", Message: message}
	}
	declaration, reason, hasReason := strings.Cut(comment.text, " -- ")
	if !hasReason && strings.HasSuffix(declaration, " --") {
		declaration = strings.TrimSuffix(declaration, " --")
		hasReason = true
	}
	if hasReason && strings.TrimSpace(reason) == "" {
		return Directive{}, invalid("inline suppression requires a reason after \" -- \"")
	}
	command, selectors := declaration, ""
	if strings.HasPrefix(declaration, "ignore[") {
		command = "ignore"
		var closed bool
		selectors, closed = strings.CutSuffix(strings.TrimSpace(strings.TrimPrefix(declaration, "ignore[")), "]")
		if !closed || strings.ContainsAny(selectors, "[]") {
			return Directive{}, invalid("inline suppression must use \"ignore[RULE ...]\" with a closing bracket and no trailing text")
		}
	} else if boundary := strings.IndexAny(declaration, " \t"); boundary >= 0 {
		command, selectors = declaration[:boundary], declaration[boundary+1:]
	}
	target := comment.pos.Line
	switch command {
	case "actionlint:ignore", "actionlint-ignore", "ignore":
		if comment.standalone {
			return Directive{}, invalid("use \"ignore-next-line\", \"actionlint-ignore-next-line\" or \"actionlint:ignore-next-line\" in a comment before the declaration")
		}
	case "actionlint:ignore-next-line", "actionlint-ignore-next-line", "ignore-next-line":
		if !comment.standalone {
			return Directive{}, invalid(strconv.Quote(command) + " must be on its own line immediately before the declaration")
		}
		target++
	default:
		return Directive{}, invalid("unknown inline suppression directive. use \"ignore RULE\", \"ignore[RULE ...]\" or \"ignore-next-line RULE\" (\"actionlint-ignore\", \"actionlint:ignore\" and their next-line forms also work)")
	}
	if comment.startLine > 0 {
		target = min(target, comment.startLine)
	}
	var rules []string
	for group := range strings.SplitSeq(selectors, ",") {
		// Commas still require a selector on either side; whitespace can
		// separate additional selectors without creating empty ones.
		names := strings.Fields(group)
		if len(names) == 0 {
			names = []string{""}
		}
		for _, name := range names {
			if !slices.ContainsFunc(allowed, func(candidate string) bool { return index.ResolveAlias(candidate) == index.ResolveAlias(name) }) {
				message := "unknown inline suppression rule " + strconv.Quote(name) + "."
				if suggestion := messages.CloseName(name, allowed); suggestion != "" {
					message += " Did you mean " + strconv.Quote(suggestion) + "?"
				}
				return Directive{}, invalid(message + " Run actionlint rules to list available rules.")
			}
			if !slices.Contains(rules, name) {
				rules = append(rules, name)
			}
		}
	}
	return Directive{Position: comment.pos, Lines: sourcetext.LineRange{First: target, Last: max(target, comment.endLine)}, Rules: rules, Reason: strings.TrimSpace(reason)}, nil
}

type Decision struct{ Suppress, Report bool }

// Evaluation matches rule names and line ranges in Suppresses. Callers check that the finding comes from the directives' document and invocation.
type Evaluation struct {
	ranges map[string][]sourcetext.LineRange
	names  rule.Names
}

// Evaluate permits every exception when policy is nil.
func Evaluate(directives []Directive, policy func(string) Decision, names rule.Names) (Evaluation, []results.Diagnostic) {
	result := Evaluation{ranges: make(map[string][]sourcetext.LineRange), names: names}
	var reports []results.Diagnostic
	for _, directive := range directives {
		var prohibited []string
		for _, name := range directive.Rules {
			decision := Decision{Suppress: true}
			if policy != nil {
				decision = policy(names.ResolveAlias(name))
			}
			if decision.Report {
				prohibited = append(prohibited, name)
			}
			if decision.Suppress {
				current := names.ResolveAlias(name)
				result.ranges[current] = append(result.ranges[current], directive.Lines)
			}
		}
		if len(prohibited) != 0 {
			reports = append(reports, results.Diagnostic{Start: directive.Position, Rule: "forbidden-suppression", Message: "inline suppression of " + strings.Join(prohibited, ", ") + " is disallowed by policy.forbidden-suppression"})
		}
	}
	return result, reports
}

func (e Evaluation) Suppresses(ruleName string, origin sourcetext.LineRange) bool {
	return slices.ContainsFunc(e.ranges[e.names.ResolveAlias(ruleName)], origin.Overlaps)
}
