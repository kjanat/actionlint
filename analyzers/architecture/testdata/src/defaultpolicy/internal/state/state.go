package state // want package:"function flow summaries"

// Internal operational representation is outside the default public API check.
type Operational struct {
	private int
	Public  int
}

func (s *Operational) Increment() { s.Public++ }

// Imported effects are still summarized even though this package's state is
// outside default diagnostic admission.
func Increment(value *int) { (*value)++ }
