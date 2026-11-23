// Package severity defines the level of a finding: info, warning or error.
package severity

// Severity is empty when the check supplied no level, and Effective reads that as Error.
type Severity string

const (
	Info    Severity = "info"
	Warning Severity = "warning"
	Error   Severity = "error"
)

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

func (s Severity) Effective() Severity {
	if s == "" {
		return Error
	}
	return s
}

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
