package workflownames

import (
	"regexp"
	"strings"
)

// ValidPattern checks the workflow-name grammar, including exclusion filters.
func ValidPattern(pattern string) bool {
	_, ok := compilePattern(strings.TrimPrefix(pattern, "!"))
	return ok
}

// Missing reports positive workflow filters that match no known producer.
// Invalid patterns and exclusion filters cannot establish a missing producer.
func (n Names) Missing(pattern string) bool {
	if !n.Complete || strings.HasPrefix(pattern, "!") {
		return false
	}
	rx, ok := compilePattern(pattern)
	if !ok {
		return false
	}
	for name := range n.Values {
		if rx.MatchString(name) {
			return false
		}
	}
	return true
}

// ExcludesAll reports when an ordered filter list removes all its positive matches.
// Incomplete inventories and invalid patterns leave the result uncertain.
func (n Names) ExcludesAll(patterns []string) bool {
	if !n.Complete {
		return false
	}
	selected := make(map[string]bool)
	matched := false
	for _, pattern := range patterns {
		pattern, negative := strings.CutPrefix(pattern, "!")
		rx, valid := compilePattern(pattern)
		if !valid {
			return false
		}
		for name := range n.Values {
			if !rx.MatchString(name) {
				continue
			}
			if negative {
				delete(selected, name)
			} else {
				selected[name] = true
				matched = true
			}
		}
	}
	return matched && len(selected) == 0
}

// compilePattern follows GitHub's workflow-name filter syntax:
// https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax#filter-pattern-cheat-sheet
func compilePattern(pattern string) (*regexp.Regexp, bool) {
	if pattern == "" {
		return nil, false
	}
	var out strings.Builder
	out.WriteString("(?s)^")
	chars := []rune(pattern)
	repeatable := false
	for i := 0; i < len(chars); i++ {
		c := chars[i]
		switch c {
		case '\\':
			if i+1 < len(chars) && strings.ContainsRune("[]?*+\\!", chars[i+1]) {
				i++
				c = chars[i]
			}
			out.WriteString(regexp.QuoteMeta(string(c)))
			repeatable = true
		case '*':
			if i+1 < len(chars) && chars[i+1] == '*' {
				i++
				if i+1 < len(chars) && chars[i+1] == '/' && (i == 1 || chars[i-2] == '/') {
					i++
					out.WriteString("(?:.*/)?")
				} else {
					out.WriteString(".*")
				}
			} else {
				out.WriteString("[^/]*")
			}
			repeatable = false
		case '+', '?':
			if !repeatable {
				return nil, false
			}
			out.WriteRune(c)
			repeatable = false
		case '[':
			start := i
			for i++; i < len(chars) && chars[i] != ']'; i++ {
				c := chars[i]
				if c == '-' {
					if i == start+1 || i+1 == len(chars) || !sameRangeFamily(chars[i-1], chars[i+1]) {
						return nil, false
					}
				}
				if !sameRangeFamily(c, c) && c != '-' {
					return nil, false
				}
			}
			if i == len(chars) || i == start+1 {
				return nil, false
			}
			out.WriteString(string(chars[start : i+1]))
			repeatable = true
		default:
			out.WriteString(regexp.QuoteMeta(string(c)))
			repeatable = true
		}
	}
	out.WriteByte('$')
	rx, err := regexp.Compile(out.String())
	return rx, err == nil
}

func sameRangeFamily(start, end rune) bool {
	return start <= end && (start >= 'a' && end <= 'z' || start >= 'A' && end <= 'Z' || start >= '0' && end <= '9')
}
