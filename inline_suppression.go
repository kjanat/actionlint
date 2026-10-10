package actionlint

import (
	"slices"
	"strconv"
	"strings"
)

// InlineSuppressibleRules returns the rule IDs accepted by inline directives and
// disallow-suppressions configuration. The returned slice is owned by the caller.
func InlineSuppressibleRules() []string {
	rules := builtinRuleDescriptors()
	names := make([]string, 0, len(rules))
	for _, rule := range rules {
		names = append(names, rule.Name)
	}
	return names
}

func isInlineSuppressibleRule(name string) bool {
	return slices.Contains(InlineSuppressibleRules(), name)
}

// inlineSuppressionDirective is validated before policy evaluation. targetLine
// selects matching diagnostics on that physical line, including anchors. A block
// header extends the range through its body using endLine.
type inlineSuppressionDirective struct {
	pos        Pos
	targetLine int
	endLine    int
	rules      []string
	reason     string
}

func parseInlineSuppression(comment inlineSuppressionComment) (inlineSuppressionDirective, *Error) {
	invalid := func(message string) *Error {
		return errorAt(&comment.pos, "inline-suppression", message)
	}
	declaration, reason, hasReason := strings.Cut(comment.text, " -- ")
	command, selectors, _ := strings.Cut(declaration, " ")
	target := comment.pos.Line
	switch command {
	case "actionlint:ignore":
		if comment.standalone {
			return inlineSuppressionDirective{}, invalid("use \"actionlint:ignore-next-line\" in a comment before the declaration")
		}
	case "actionlint:ignore-next-line":
		if !comment.standalone {
			return inlineSuppressionDirective{}, invalid("\"actionlint:ignore-next-line\" must be on its own line immediately before the declaration")
		}
		target++
	default:
		return inlineSuppressionDirective{}, invalid("unknown inline suppression directive. use \"actionlint:ignore RULE -- reason\" or \"actionlint:ignore-next-line RULE -- reason\"")
	}
	if !hasReason || strings.TrimSpace(reason) == "" {
		return inlineSuppressionDirective{}, invalid("inline suppression requires a reason after \" -- \"")
	}
	var rules []string
	for name := range strings.SplitSeq(selectors, ",") {
		name = strings.TrimSpace(name)
		if !isInlineSuppressibleRule(name) {
			allowed := InlineSuppressibleRules()
			for i, rule := range allowed {
				allowed[i] = strconv.Quote(rule)
			}
			return inlineSuppressionDirective{}, invalid("unknown inline suppression rule " + strconv.Quote(name) + ". expected " + strings.Join(allowed, ", "))
		}
		if !slices.Contains(rules, name) {
			rules = append(rules, name)
		}
	}
	return inlineSuppressionDirective{pos: comment.pos, targetLine: target, endLine: comment.endLine, rules: rules, reason: strings.TrimSpace(reason)}, nil
}

func filterInlineSuppressions(source []byte, errors []*Error, policy *SuppressionsPolicy) []*Error {
	comments := collectInlineSuppressionComments(source)
	if len(comments) == 0 {
		return errors
	}
	var directives []inlineSuppressionDirective
	var directiveErrors []*Error
	for _, comment := range comments {
		directive, err := parseInlineSuppression(comment)
		if err != nil {
			directiveErrors = append(directiveErrors, err)
		} else {
			directives = append(directives, directive)
		}
	}
	return applyInlineSuppressions(append(errors, directiveErrors...), directives, policy)
}

// Scope directives to the diagnostic's own YAML source, never its caller.
func filterForeignInlineSuppressions(path string, source []byte, findings []*Error, policy *SuppressionsPolicy) []*Error {
	local := make([]*Error, len(findings))
	for i, finding := range findings {
		scoped := *finding
		scoped.source = nil
		local[i] = &scoped
	}
	local = filterInlineSuppressions(source, local, policy)
	for _, finding := range local {
		finding.source, finding.Filepath = source, path
	}
	return local
}

// applyInlineSuppressions enforces restrictions independently of YAML layout.
// Invalid directives never enter this stage. CLI and path filters run afterwards.
func applyInlineSuppressions(errors []*Error, directives []inlineSuppressionDirective, policy *SuppressionsPolicy) []*Error {
	type lineRange struct {
		start, end int
	}
	suppressed := map[string][]lineRange{}
	var policyErrors []*Error
	for _, directive := range directives {
		var prohibited []string
		for _, name := range directive.rules {
			mode := policy.reportFor(name)
			if mode == reportSuppression || mode == reportAll {
				prohibited = append(prohibited, name)
			}
			if mode == suppressionsAllowed || mode == reportSuppression {
				suppressed[name] = append(suppressed[name], lineRange{directive.targetLine, max(directive.targetLine, directive.endLine)})
			}
		}
		if len(prohibited) != 0 {
			policyErrors = append(policyErrors, errorfAt(&directive.pos, "disallow-suppressions", "inline suppression of %s is disallowed by policy.disallow-suppressions", strings.Join(prohibited, ", ")))
		}
	}
	filtered := make([]*Error, 0, len(errors)+len(policyErrors))
	for _, err := range errors {
		if err.source == nil && slices.ContainsFunc(suppressed[err.Kind], func(lines lineRange) bool { return err.Line >= lines.start && err.Line <= lines.end }) {
			continue
		}
		filtered = append(filtered, err)
	}
	return append(filtered, policyErrors...)
}
