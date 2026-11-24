package value

import "actionlint.kjanat.dev/v2/internal/languages/expressions/exprtypes"

// TypeWithFallback expects AnyType when no type was inferred.
func (v Value) TypeWithFallback(inferred exprtypes.Type) exprtypes.Type {
	if !v.Known() {
		return inferred
	}
	switch v.Kind() {
	case Null:
		return exprtypes.NullType()
	case Bool:
		return exprtypes.BoolType()
	case Number:
		return exprtypes.NumberType()
	case String:
		return exprtypes.StringType()
	case Object:
		object, _ := inferred.Object()
		var properties exprtypes.ObjectBuilder
		for name, property := range v.Props() {
			propertyType, _ := object.Property(name)
			properties.Set(name, property.TypeWithFallback(propertyType))
		}
		return properties.Build().Type()
	case Array:
		array, _ := inferred.Array()
		element := exprtypes.AnyType()
		first := true
		for child := range v.Items() {
			ty := child.TypeWithFallback(array.Elem())
			if first {
				element, first = ty, false
			} else {
				element = element.Merge(ty)
			}
		}
		return exprtypes.NewArrayType(element).Type()
	case KindUnknown:
	}
	return exprtypes.AnyType()
}
