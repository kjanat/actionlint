package configuration

import "strings"

type decodeError struct{ cause error }

func (e decodeError) Error() string {
	return strings.ReplaceAll(e.cause.Error(), "\n", " ")
}

func (e decodeError) Unwrap() error { return e.cause }
