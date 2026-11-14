// Package arguments identifies invalid command inputs independently of command execution.
package arguments

// UsageError reports an invalid command, flag or combination of command inputs.
type UsageError struct{ Cause error }

func (e UsageError) Error() string { return e.Cause.Error() }

func (e UsageError) Unwrap() error { return e.Cause }
