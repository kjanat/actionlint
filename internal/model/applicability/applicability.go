// Package applicability describes the review that one proposed fix requires.
package applicability

import "fmt"

// Applicability requires review when it is the zero value.
type Applicability uint8

const (
	Suggested Applicability = iota
	// Safe can be applied without changing the intended behavior.
	Safe
	// Unsafe can change behavior even when it resolves the finding.
	Unsafe
)

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

func (a Applicability) Valid() bool { return a <= Unsafe }

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

func (a Applicability) MarshalText() ([]byte, error) {
	if !a.Valid() {
		return nil, fmt.Errorf("invalid fix applicability %d", a)
	}
	return []byte(a.String()), nil
}

func (a *Applicability) UnmarshalText(text []byte) error {
	parsed, err := Parse(string(text))
	if err == nil {
		*a = parsed
	}
	return err
}
