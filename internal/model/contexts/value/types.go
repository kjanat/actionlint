package value

import "actionlint.kjanat.dev/v2/internal/languages/expressions/types"

// TypeWithFallback derives a type from known values. Unknown values use inferred;
// unknown object properties and array elements use its corresponding child type.
// Pass AnyType when no type was inferred. This does not change evaluation state
// or establish that a dynamic or invalid value can be evaluated successfully.
func (v Value) TypeWithFallback(inferred types.Type) types.Type {
	if !v.Known() {
		return inferred
	}
	switch v.Kind() {
	case Null:
		return types.NullType()
	case Bool:
		return types.BoolType()
	case Number:
		return types.NumberType()
	case String:
		return types.StringType()
	case Object:
		object, _ := inferred.Object()
		var properties types.ObjectBuilder
		for name, property := range v.Props() {
			propertyType, _ := object.Property(name)
			properties.Set(name, property.TypeWithFallback(propertyType))
		}
		return properties.Build().Type()
	case Array:
		array, _ := inferred.Array()
		element := types.AnyType()
		first := true
		for child := range v.Items() {
			ty := child.TypeWithFallback(array.Elem())
			if first {
				element, first = ty, false
			} else {
				element = element.Merge(ty)
			}
		}
		return types.NewArrayType(element).Type()
	case KindUnknown:
	}
	return types.AnyType()
}
