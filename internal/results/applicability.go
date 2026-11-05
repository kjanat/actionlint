package results

import "fmt"

// Applicability describes the review required for one concrete fix.
// Its zero value requires review; an omitted plugin field never enables automatic edits.
type Applicability uint8

const (
	// FixSuggested offers a correction whose intent or effects need review.
	FixSuggested Applicability = iota
	// FixSafe can be applied without changing the intended behavior.
	FixSafe
	// FixUnsafe can change behavior even when it resolves the finding.
	FixUnsafe
)

// ParseApplicability validates a wire value. Older plugins may omit it or send an empty string.
func ParseApplicability(value string) (Applicability, error) {
	switch value {
	case "", "suggested":
		return FixSuggested, nil
	case "safe":
		return FixSafe, nil
	case "unsafe":
		return FixUnsafe, nil
	default:
		return FixSuggested, fmt.Errorf("unknown fix applicability %q", value)
	}
}

// Valid reports whether the value belongs to the fix contract.
func (a Applicability) Valid() bool { return a <= FixUnsafe }

// Automatic reports whether this fix can be applied without review.
func (a Applicability) Automatic() bool { return a == FixSafe }

func (a Applicability) String() string {
	switch a {
	case FixSuggested:
		return "suggested"
	case FixSafe:
		return "safe"
	case FixUnsafe:
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
	parsed, err := ParseApplicability(string(text))
	if err == nil {
		*a = parsed
	}
	return err
}
