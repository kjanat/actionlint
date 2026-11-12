// Package validation checks checker references before their methods are called.
package validation

import (
	"reflect"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
)

// IsNil detects both nil interfaces and typed nil checkers before
// metadata or execution methods are called.
func IsNil(value checkinputs.Checker) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	if !nillableKinds[reflected.Kind()] {
		return false
	}
	return reflected.IsNil()
}

// nillableKinds lists the kinds whose values reflect.Value.IsNil accepts.
var nillableKinds = map[reflect.Kind]bool{
	reflect.Chan:      true,
	reflect.Func:      true,
	reflect.Interface: true,
	reflect.Map:       true,
	reflect.Pointer:   true,
	reflect.Slice:     true,
}
