package functions

// Availability describes a caller-supplied location restriction on a function.
// AvailableAt names the allowed locations for diagnostic explanations.
type Availability struct {
	Allowed     bool
	AvailableAt []string
}
