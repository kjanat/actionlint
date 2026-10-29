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
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}
