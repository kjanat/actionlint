// Package applicability describes the review that one proposed fix requires.
package applicability

import "fmt"

// Applicability describes the review required for one concrete fix.
// Its zero value requires review; an omitted plugin field never enables automatic edits.
type Applicability uint8

const (
	// Suggested offers a correction whose intent or effects need review.
	Suggested Applicability = iota
	// Safe can be applied without changing the intended behavior.
	Safe
	// Unsafe can change behavior even when it resolves the finding.
	Unsafe
)

// Parse validates a wire value. Older plugins may omit it or send an empty string.
func Parse(value string) (Applicability, error) {
	switch value {
	case "", "suggested":
		return Suggested, nil
	case "safe":
		return Safe, nil
	case "unsafe":
		return Unsafe, nil
	default:
		return Suggested, fmt.Errorf("unknown fix applicability %q", value)
	}
}

// Valid reports whether the value belongs to the fix contract.
func (a Applicability) Valid() bool { return a <= Unsafe }

// Automatic reports whether this fix can be applied without review.
func (a Applicability) Automatic() bool { return a == Safe }

func (a Applicability) String() string {
	switch a {
	case Suggested:
		return "suggested"
	case Safe:
		return "safe"
	case Unsafe:
		return "unsafe"
	default:
		return "invalid"
	}
}

// MarshalText preserves the string representation used by JSON plugin protocols.
func (a Applicability) MarshalText() ([]byte, error) {
	if !a.Valid() {
		return nil, fmt.Errorf("invalid fix applicability %d", a)
	}
	return []byte(a.String()), nil
}

// UnmarshalText validates applicability before it enters the diagnostic model.
func (a *Applicability) UnmarshalText(text []byte) error {
	parsed, err := Parse(string(text))
	if err == nil {
		*a = parsed
	}
	return err
}
