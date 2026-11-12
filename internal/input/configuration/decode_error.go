package config

import "strings"

// decodeError keeps configuration diagnostics on one line without discarding
// the decoder's error identity or typed failure information.
type decodeError struct{ cause error }

func (e decodeError) Error() string {
	return strings.ReplaceAll(e.cause.Error(), "\n", " ")
}

func (e decodeError) Unwrap() error { return e.cause }
