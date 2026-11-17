package messages

import "strings"

func adjacentTypo(a, b string) bool {
	if a == b {
		return false
	}
	x, y := []rune(a), []rune(b)
	if len(x) > len(y) {
		x, y = y, x
	}
	if len(y)-len(x) > 1 {
		return false
	}
	for i := range x {
		if x[i] == y[i] {
			continue
		}
		if len(x) < len(y) {
			return string(x[i:]) == string(y[i+1:])
		}
		if string(x[i+1:]) == string(y[i+1:]) {
			return true
		}
		return i+1 < len(x) && x[i] == y[i+1] && x[i+1] == y[i] && string(x[i+2:]) == string(y[i+2:])
	}
	return len(x) != len(y)
}

// CloseName suggests only a unique, single-edit candidate, independent of order.
func CloseName(name string, candidates []string) string {
	suggestion := ""
	for _, candidate := range candidates {
		if !adjacentTypo(strings.ToLower(name), strings.ToLower(candidate)) {
			continue
		}
		if suggestion != "" && !strings.EqualFold(suggestion, candidate) {
			return ""
		}
		suggestion = candidate
	}
	return suggestion
}
