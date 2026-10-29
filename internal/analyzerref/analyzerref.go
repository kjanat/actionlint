// Package analyzerref inspects analyzer registrations without hosting them.
// Checks and planning can validate registrations without importing the
// analyzer host, which launches processes.
package analyzerref

import (
	"reflect"

	"actionlint.kjanat.dev/v2/analyzer"
)

// IsNil detects both nil interfaces and typed nil analyzer values before
// metadata or execution methods are called.
func IsNil(value analyzer.Analyzer) bool {
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
