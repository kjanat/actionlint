// Package severity defines the level of a finding: info, warning or error.
package severity

// Severity is the level of a finding. The zero value means the check supplied
// no level; Effective reads it as Error. Text from configuration, tools and
// plugins becomes a Severity through Parse.
type Severity string

const (
	Info    Severity = "info"
	Warning Severity = "warning"
	Error   Severity = "error"
)

// Parse reads a severity name. It accepts info, warning and error, and the
// spellings warn for Warning and note and hint for Info that tools and SARIF
// use. It reports false for any other text, including the empty string.
func Parse(text string) (Severity, bool) {
	switch text {
	case "info", "note", "hint":
		return Info, true
	case "warning", "warn":
		return Warning, true
	case "error":
		return Error, true
	}
	return "", false
}

// Effective returns s, or Error when the check supplied no level.
func (s Severity) Effective() Severity {
	if s == "" {
		return Error
	}
	return s
}

// Rank orders severities from Info (1) to Error (3). The zero value ranks 0.
func (s Severity) Rank() int {
	switch s {
	case Info:
		return 1
	case Warning:
		return 2
	case Error:
		return 3
	}
	return 0
}
