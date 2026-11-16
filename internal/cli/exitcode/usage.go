package exitcode

// UsageError reports an invalid command, flag or combination of command
// inputs. A command that fails with it exits with ExitInvalidCommandOption.
type UsageError struct{ Cause error }

func (e UsageError) Error() string { return e.Cause.Error() }

func (e UsageError) Unwrap() error { return e.Cause }
