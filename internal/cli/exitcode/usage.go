package exitcode

// UsageError makes a command exit with ExitInvalidCommandOption.
type UsageError struct{ Cause error }

func (e UsageError) Error() string { return e.Cause.Error() }

func (e UsageError) Unwrap() error { return e.Cause }

var _ error = UsageError{}
