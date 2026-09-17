package actionlint

import (
	"slices"
	"strconv"
	"strings"
)

// InlineSuppressibleRules returns the rule IDs accepted by inline directives and
// disallow-suppressions configuration. The returned slice is owned by the caller.
func InlineSuppressibleRules() []string {
	return []string{"cache-write-untrusted", "cache-call-unrestricted", "cache-operation"}
}

func isInlineSuppressibleRule(name string) bool {
	return slices.Contains(InlineSuppressibleRules(), name)
}

// inlineSuppressionDirective is validated before policy evaluation. targetLine
// selects every matching diagnostic on that physical line, including anchors.
type inlineSuppressionDirective struct {
	pos        Pos
	targetLine int
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
	return inlineSuppressionDirective{pos: comment.pos, targetLine: target, rules: rules, reason: strings.TrimSpace(reason)}, nil
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
	filtered := applyInlineSuppressions(errors, directives, policy)
	return append(filtered, directiveErrors...)
}

// applyInlineSuppressions enforces restrictions independently of YAML layout.
// Invalid directives never enter this stage. CLI and path filters run afterwards.
func applyInlineSuppressions(errors []*Error, directives []inlineSuppressionDirective, policy *SuppressionsPolicy) []*Error {
	type target struct {
		line int
		rule string
	}
	suppressed := map[target]bool{}
	var policyErrors []*Error
	for _, directive := range directives {
		var prohibited []string
		for _, name := range directive.rules {
			mode := policy.reportFor(name)
			if mode == reportSuppression || mode == reportAll {
				prohibited = append(prohibited, name)
			}
			if mode == suppressionsAllowed || mode == reportSuppression {
				suppressed[target{directive.targetLine, name}] = true
			}
		}
		if len(prohibited) != 0 {
			policyErrors = append(policyErrors, errorfAt(&directive.pos, "disallow-suppressions", "inline suppression of %s is disallowed by policy.disallow-suppressions", strings.Join(prohibited, ", ")))
		}
	}
	filtered := make([]*Error, 0, len(errors)+len(policyErrors))
	for _, err := range errors {
		if err.source == nil && suppressed[target{err.Line, err.Kind}] {
			continue
		}
		filtered = append(filtered, err)
	}
	return append(filtered, policyErrors...)
}
