package suppression

import (
	"slices"
	"strconv"
	"strings"

	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/results"
	"actionlint.kjanat.dev/v2/internal/results/messages"
)

// Directive is a validated inline exception over a physical source range.
// Position identifies its comment; First and Last bound the declaration it covers.
type Directive struct {
	Position    sourcepos.Position
	First, Last int
	Rules       []string
	Reason      string
}

// Parse reads real YAML comments, recovers comments in malformed documents, and
// validates directives against the caller's ordered rule catalogue.
func Parse(source []byte, rules []string, names rule.Names) ([]Directive, []results.Diagnostic) {
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
	return Directive{Position: comment.pos, First: target, Last: max(target, comment.endLine), Rules: rules, Reason: strings.TrimSpace(reason)}, nil
}

// Decision independently controls whether an exception hides findings and
// whether using that exception produces a policy diagnostic.
type Decision struct{ Suppress, Report bool }

type lineRange struct{ first, last int }

// Evaluation contains the source-local exceptions permitted by policy.
// Invocation/source ownership is supplied by the host before calling Suppresses.
type Evaluation struct {
	ranges map[string][]lineRange
	names  rule.Names
}

// Evaluate applies restrictions to already validated directives. A nil policy
// permits every exception. It returns the permitted exceptions and the policy
// diagnostics for prohibited ones. Grammar diagnostics are evaluated by the same
// ranges as analyzer diagnostics; policy diagnostics are emitted after filtering.
func Evaluate(directives []Directive, policy func(string) Decision, names rule.Names) (Evaluation, []results.Diagnostic) {
	result := Evaluation{ranges: make(map[string][]lineRange), names: names}
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
				result.ranges[current] = append(result.ranges[current], lineRange{directive.First, directive.Last})
			}
		}
		if len(prohibited) != 0 {
			reports = append(reports, results.Diagnostic{Start: directive.Position, Rule: "forbidden-suppression", Message: "inline suppression of " + strings.Join(prohibited, ", ") + " is disallowed by policy.forbidden-suppression"})
		}
	}
	return result, reports
}

// Suppresses reports whether a finding's origin overlaps an allowed exception.
func (e Evaluation) Suppresses(ruleName string, first, last int) bool {
	return slices.ContainsFunc(e.ranges[e.names.ResolveAlias(ruleName)], func(lines lineRange) bool { return first <= lines.last && last >= lines.first })
}
