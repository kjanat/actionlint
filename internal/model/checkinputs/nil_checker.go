package checkinputs

import (
	"reflect"
)

// IsNilChecker detects both nil interfaces and typed nil checkers before
// metadata or execution methods are called.
func IsNilChecker(value Checker) bool {
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
