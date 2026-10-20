package suppression

import (
	"slices"
	"strconv"
	"strings"

	"actionlint.kjanat.dev/v2/diagnostic"
)

// Directive is a validated inline exception over a physical source range.
// Position identifies its comment; First and Last bound the declaration it covers.
type Directive struct {
	Position    diagnostic.Position
	First, Last int
	Rules       []string
	Reason      string
}

// Parse reads real YAML comments, recovers comments in malformed documents, and
// validates directives against the caller's ordered rule catalogue.
func Parse(source []byte, rules []string) ([]Directive, []diagnostic.Diagnostic) {
	var directives []Directive
	var findings []diagnostic.Diagnostic
	for _, comment := range collectInlineSuppressionComments(source) {
		directive, err := parseInlineSuppression(comment, rules)
		if err != nil {
			findings = append(findings, *err)
		} else {
			directives = append(directives, directive)
		}
	}
	return directives, findings
}

func parseInlineSuppression(comment inlineSuppressionComment, allowed []string) (Directive, *diagnostic.Diagnostic) {
	invalid := func(message string) *diagnostic.Diagnostic {
		return &diagnostic.Diagnostic{Start: comment.pos, Rule: "inline-suppression", Message: message}
	}
	declaration, reason, hasReason := strings.Cut(comment.text, " -- ")
	command, selectors, _ := strings.Cut(declaration, " ")
	target := comment.pos.Line
	switch command {
	case "actionlint:ignore":
		if comment.standalone {
			return Directive{}, invalid("use \"actionlint:ignore-next-line\" in a comment before the declaration")
		}
	case "actionlint:ignore-next-line":
		if !comment.standalone {
			return Directive{}, invalid("\"actionlint:ignore-next-line\" must be on its own line immediately before the declaration")
		}
		target++
	default:
		return Directive{}, invalid("unknown inline suppression directive. use \"actionlint:ignore RULE -- reason\" or \"actionlint:ignore-next-line RULE -- reason\"")
	}
	if comment.startLine > 0 {
		target = min(target, comment.startLine)
	}
	if !hasReason && strings.HasSuffix(declaration, " --") {
		hasReason = true
	}
	if hasReason && strings.TrimSpace(reason) == "" {
		return Directive{}, invalid("inline suppression requires a reason after \" -- \"")
	}
	var rules []string
	for name := range strings.SplitSeq(selectors, ",") {
		name = strings.TrimSpace(name)
		if !slices.Contains(allowed, name) {
			allowed := slices.Clone(allowed)
			for i, rule := range allowed {
				allowed[i] = strconv.Quote(rule)
			}
			return Directive{}, invalid("unknown inline suppression rule " + strconv.Quote(name) + ". expected " + strings.Join(allowed, ", "))
		}
		if !slices.Contains(rules, name) {
			rules = append(rules, name)
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
	ranges      map[string][]lineRange
	Diagnostics []diagnostic.Diagnostic
}

// Evaluate applies restrictions to already validated directives. A nil policy
// permits every exception. Grammar diagnostics are evaluated by the same ranges
// as analyzer diagnostics; policy diagnostics are emitted after filtering.
func Evaluate(directives []Directive, policy func(string) Decision) Evaluation {
	result := Evaluation{ranges: make(map[string][]lineRange)}
	for _, directive := range directives {
		var prohibited []string
		for _, name := range directive.Rules {
			decision := Decision{Suppress: true}
			if policy != nil {
				decision = policy(name)
			}
			if decision.Report {
				prohibited = append(prohibited, name)
			}
			if decision.Suppress {
				result.ranges[name] = append(result.ranges[name], lineRange{directive.First, directive.Last})
			}
		}
		if len(prohibited) != 0 {
			result.Diagnostics = append(result.Diagnostics, diagnostic.Diagnostic{Start: directive.Position, Rule: "disallow-suppressions", Message: "inline suppression of " + strings.Join(prohibited, ", ") + " is disallowed by policy.disallow-suppressions"})
		}
	}
	return result
}

// Suppresses reports whether a finding's origin overlaps an allowed exception.
func (e Evaluation) Suppresses(rule string, first, last int) bool {
	return slices.ContainsFunc(e.ranges[rule], func(lines lineRange) bool { return first <= lines.last && last >= lines.first })
}
