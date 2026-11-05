package matrix

import value "actionlint.kjanat.dev/v2/internal/model/contexts/values"

// ValueHasUnknownParts reports dynamic or invalid values, including nested members.
func ValueHasUnknownParts(v value.Value) bool { return dynamic(v) }

func dynamic(v value.Value) bool {
	if v.State() == value.Dynamic || v.State() == value.Invalid {
		return true
	}
	if v.Kind() == value.Array {
		for item := range v.Items() {
			if dynamic(item) {
				return true
			}
		}
	}
	if v.Kind() == value.Object {
		for _, item := range v.Props() {
			if dynamic(item) {
				return true
			}
		}
	}
	return false
}

// EqualDeclaredValues compares declaration values without applying matrix-filter coercions.
func EqualDeclaredValues(left, right value.Value) bool {
	if left.State() == value.Dynamic || right.State() == value.Dynamic {
		return left.State() == value.Dynamic && right.State() == value.Dynamic && left.Source() == right.Source()
	}
	if left.Kind() == value.Array && right.Kind() == value.Array && left.Known() && right.Known() {
		if left.Len() != right.Len() {
			return false
		}
		for i := range left.Len() {
			a, _ := left.Index(i)
			b, _ := right.Index(i)
			if !EqualDeclaredValues(a, b) {
				return false
			}
		}
		return true
	}
	if left.Kind() == value.Object && right.Kind() == value.Object && left.Known() && right.Known() {
		if left.Len() != right.Len() {
			return false
		}
		for name, a := range left.Props() {
			b, ok := right.Prop(name)
			if !ok || !EqualDeclaredValues(a, b) {
				return false
			}
		}
		return true
	}
	return left.Equal(right)
}

// FilterMayCompareDifferentTypes reports known scalar type differences and unknown operands that may require loose filter conversion.
func FilterMayCompareDifferentTypes(axis, filter value.Value) bool {
	if !filter.Known() {
		return false
	}
	if axis.State() == value.Invalid {
		return false
	}
	if axis.State() == value.Dynamic {
		return unknownTypeMismatch(filter)
	}
	if filter.Kind() == value.Object {
		for name, expected := range filter.Props() {
			if axis.Kind() == value.Array {
				if index, err := matrixIndex(name); err == nil && dynamicIndex(axis, index) {
					continue
				}
			}
			actual, ok := lookup(axis, name)
			if ok && FilterMayCompareDifferentTypes(actual, expected) || !ok && missingTypeMismatch(expected) {
				return true
			}
		}
		return false
	}
	if filter.Kind() == value.Array {
		for i := range filter.Len() {
			expected, _ := filter.Index(i)
			if expected.State() == value.Dynamic || expected.State() == value.Invalid {
				return false
			}
			uncertain := axis.Kind() == value.Array && dynamicIndex(axis, i)
			if uncertain {
				return false
			}
			actual, aok := axis.Index(i)
			if aok && FilterMayCompareDifferentTypes(actual, expected) || !aok && missingTypeMismatch(expected) {
				return true
			}
		}
		return false
	}
	if !axis.Known() {
		return false
	}
	if axis.Kind() == value.Array || axis.Kind() == value.Object {
		return false
	}
	if axis.Kind() == filter.Kind() {
		return false
	}
	if axis.Kind() == value.String && filter.Kind() == value.String {
		return false
	}
	return true
}

func missingTypeMismatch(filter value.Value) bool {
	if !filter.Known() || filter.Kind() == value.Null {
		return false
	}
	if filter.Kind() == value.Object {
		for _, nested := range filter.Props() {
			if missingTypeMismatch(nested) {
				return true
			}
		}
		return false
	}
	if filter.Kind() == value.Array {
		for nested := range filter.Items() {
			if missingTypeMismatch(nested) {
				return true
			}
		}
		return false
	}
	return true
}

func unknownTypeMismatch(filter value.Value) bool {
	if !filter.Known() {
		return false
	}
	if filter.Kind() == value.Object {
		for _, nested := range filter.Props() {
			if unknownTypeMismatch(nested) {
				return true
			}
		}
		return false
	}
	if filter.Kind() == value.Array {
		for nested := range filter.Items() {
			if nested.State() == value.Dynamic || nested.State() == value.Invalid {
				return false
			}
			if unknownTypeMismatch(nested) {
				return true
			}
		}
		return false
	}
	return filter.Kind() == value.Bool || filter.Kind() == value.Number
}
