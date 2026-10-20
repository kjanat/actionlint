package expression

import (
	"fmt"
	"maps"
	"sort"
	"strings"
)

// Types

// Type is interface for types of values in expression.
type Type interface {
	// String returns String representation of the Type.
	String() string
	// Assignable returns if other Type can be Assignable to the Type.
	Assignable(other Type) bool
	// Merge merges other Type into this Type. When other Type conflicts with this Type, the merged
	// result is any Type as fallback.
	Merge(other Type) Type
	// DeepCopy duplicates itself. All its child types are copied recursively.
	DeepCopy() Type
}

// AnyType represents Type which can be any Type. It also indicates that a value of the Type cannot
// be Type-checked since it's Type cannot be known statically.
type AnyType struct{}

func (ty AnyType) String() string {
	return "any"
}

// Assignable returns if other Type can be Assignable to the Type.
func (ty AnyType) Assignable(_ Type) bool {
	return true
}

// Merge merges other Type into this Type. When other Type conflicts with this Type, the merged
// result is any Type as fallback.
func (ty AnyType) Merge(other Type) Type {
	return ty
}

// DeepCopy duplicates itself. All its child types are copied recursively.
func (ty AnyType) DeepCopy() Type {
	return ty
}

// NullType is Type for null value.
type NullType struct{}

func (ty NullType) String() string {
	return "null"
}

// Assignable returns if other Type can be Assignable to the Type.
func (ty NullType) Assignable(other Type) bool {
	switch other.(type) {
	case NullType, AnyType:
		return true
	default:
		return false
	}
}

// Merge merges other Type into this Type. When other Type conflicts with this Type, the merged
// result is any Type as fallback.
func (ty NullType) Merge(other Type) Type {
	if _, ok := other.(NullType); ok {
		return ty
	}
	return AnyType{}
}

// DeepCopy duplicates itself. All its child types are copied recursively.
func (ty NullType) DeepCopy() Type {
	return ty
}

// NumberType is Type for number values such as integer or float.
type NumberType struct{}

func (ty NumberType) String() string {
	return "number"
}

// Assignable returns if other Type can be Assignable to the Type.
func (ty NumberType) Assignable(other Type) bool {
	// TODO: Is String of numbers corced into number?
	switch other.(type) {
	case NumberType, AnyType:
		return true
	default:
		return false
	}
}

// Merge merges other Type into this Type. When other Type conflicts with this Type, the merged
// result is any Type as fallback.
func (ty NumberType) Merge(other Type) Type {
	switch other.(type) {
	case NumberType:
		return ty
	case StringType:
		return other
	default:
		return AnyType{}
	}
}

// DeepCopy duplicates itself. All its child types are copied recursively.
func (ty NumberType) DeepCopy() Type {
	return ty
}

// BoolType is Type for boolean values.
type BoolType struct{}

func (ty BoolType) String() string {
	return "bool"
}

// Assignable returns if other Type can be Assignable to the Type.
func (ty BoolType) Assignable(other Type) bool {
	// Any Type can be converted into bool..
	// e.g.
	//    if: ${{ steps.foo }}
	return true
}

// Merge merges other Type into this Type. When other Type conflicts with this Type, the merged
// result is any Type as fallback.
func (ty BoolType) Merge(other Type) Type {
	switch other.(type) {
	case BoolType:
		return ty
	case StringType:
		return other
	default:
		return AnyType{}
	}
}

// DeepCopy duplicates itself. All its child types are copied recursively.
func (ty BoolType) DeepCopy() Type {
	return ty
}

// StringType is Type for String values.
type StringType struct{}

func (ty StringType) String() string {
	return "string"
}

// Assignable returns if other Type can be Assignable to the Type.
func (ty StringType) Assignable(other Type) bool {
	// Bool and null types also can be coerced into String. But in almost all case, those coercing
	// would be mistakes.
	switch other.(type) {
	case StringType, NumberType, AnyType:
		return true
	default:
		return false
	}
}

// Merge merges other Type into this Type. When other Type conflicts with this Type, the merged
// result is any Type as fallback.
func (ty StringType) Merge(other Type) Type {
	switch other.(type) {
	case StringType, NumberType, BoolType:
		return ty
	default:
		return AnyType{}
	}
}

// DeepCopy duplicates itself. All its child types are copied recursively.
func (ty StringType) DeepCopy() Type {
	return ty
}

// ObjectType is Type for objects, which can hold key-values.
type ObjectType struct {
	// DisplayName abbreviates large externally supplied structural types in diagnostics.
	DisplayName string
	// Props is map from properties name to their Type.
	Props map[string]Type
	// Mapped is an element Type of this object. This means all props have the Type. For example,
	// The element Type of env context is String.
	// AnyType means its property types can be any Type so it shapes a Loose object. Setting nil
	// means properties are mapped to no Type so it shapes a Strict object.
	//
	// Invariant: All types in Props field must be Assignable to this Type.
	Mapped Type
}

// NewEmptyObjectType creates new Loose ObjectType instance which allows unknown props. When
// accessing to unknown props, their values will fall back to any.
func NewEmptyObjectType() *ObjectType {
	return &ObjectType{Props: map[string]Type{}, Mapped: AnyType{}}
}

// NewObjectType creates new Loose ObjectType instance which allows unknown props with given props.
func NewObjectType(props map[string]Type) *ObjectType {
	return &ObjectType{Props: props, Mapped: AnyType{}}
}

// NewEmptyStrictObjectType creates new ObjectType instance which does not allow unknown props.
func NewEmptyStrictObjectType() *ObjectType {
	return &ObjectType{Props: map[string]Type{}}
}

// NewStrictObjectType creates new ObjectType instance which does not allow unknown props with
// given prop types.
func NewStrictObjectType(props map[string]Type) *ObjectType {
	return &ObjectType{Props: props}
}

// NewMapObjectType creates new ObjectType which maps keys to a specific Type value.
func NewMapObjectType(t Type) *ObjectType {
	return &ObjectType{Mapped: t}
}

// IsStrict returns if the Type is a Strict object, which means no unknown prop is allowed.
func (ty *ObjectType) IsStrict() bool {
	return ty.Mapped == nil
}

// IsLoose returns if the Type is a Loose object, which allows any unknown props.
func (ty *ObjectType) IsLoose() bool {
	_, ok := ty.Mapped.(AnyType)
	return ok
}

// Strict sets the object is Strict, which means only known properties are allowed.
func (ty *ObjectType) Strict() {
	ty.Mapped = nil
}

// Loose sets the object is Loose, which means any properties can be set.
func (ty *ObjectType) Loose() {
	ty.Mapped = AnyType{}
}

func (ty *ObjectType) String() string {
	if ty.DisplayName != "" {
		return ty.DisplayName
	}
	if !ty.IsStrict() {
		if ty.IsLoose() {
			return "object"
		}
		return fmt.Sprintf("{string => %s}", ty.Mapped.String())
	}

	ps := make([]string, 0, len(ty.Props))
	for n := range ty.Props {
		ps = append(ps, n)
	}
	sort.Strings(ps)

	var b strings.Builder
	b.WriteByte('{')
	first := true
	for _, p := range ps {
		if first {
			first = false
		} else {
			b.WriteString("; ")
		}
		b.WriteString(p)
		b.WriteString(": ")
		b.WriteString(ty.Props[p].String())
	}
	b.WriteByte('}')

	return b.String()
}

// Assignable reports whether the other object's properties satisfy this type.
func (ty *ObjectType) Assignable(other Type) bool {
	switch other := other.(type) {
	case AnyType:
		return true
	case *ObjectType:
		if !ty.IsStrict() {
			if !other.IsStrict() {
				return ty.Mapped.Assignable(other.Mapped)
			}
			for _, t := range other.Props {
				if !ty.Mapped.Assignable(t) {
					return false
				}
			}
			return true
		}
		// ty is Strict

		if !other.IsStrict() {
			for _, t := range ty.Props {
				if !t.Assignable(other.Mapped) {
					return false
				}
			}
			return true
		}
		// ty and other are Strict

		for n, r := range other.Props {
			if l, ok := ty.Props[n]; !ok || !l.Assignable(r) {
				return false
			}
		}

		return true
	default:
		return false
	}
}

// Merge merges two object types into one. When other object has unknown props, they are merged into
// current object. When both have same property, when they are Assignable, it remains as-is.
// Otherwise, the property falls back to any Type.
func (ty *ObjectType) Merge(other Type) Type {
	switch other := other.(type) {
	case *ObjectType:
		// Shortcuts
		if len(ty.Props) == 0 && other.IsLoose() {
			return other
		}
		if len(other.Props) == 0 && ty.IsLoose() {
			return ty
		}

		mapped := ty.Mapped
		if mapped == nil {
			mapped = other.Mapped
		} else if other.Mapped != nil {
			mapped = mapped.Merge(other.Mapped)
		}

		props := make(map[string]Type, len(ty.Props))
		maps.Copy(props, ty.Props)
		for n, r := range other.Props {
			if l, ok := props[n]; ok {
				props[n] = l.Merge(r)
			} else {
				props[n] = r
				if mapped != nil {
					mapped = mapped.Merge(r)
				}
			}
		}

		return &ObjectType{
			Props:       props,
			Mapped:      mapped,
			DisplayName: ty.DisplayName,
		}
	default:
		return AnyType{}
	}
}

// DeepCopy duplicates itself. All its child types are copied recursively.
func (ty *ObjectType) DeepCopy() Type {
	p := make(map[string]Type, len(ty.Props))
	for n, t := range ty.Props {
		p[n] = t.DeepCopy()
	}
	m := ty.Mapped
	if m != nil {
		m = m.DeepCopy()
	}
	return &ObjectType{Props: p, Mapped: m, DisplayName: ty.DisplayName}
}

// ArrayType is Type for arrays.
type ArrayType struct {
	// Elem is Type of element of the array.
	Elem Type
	// Deref is true when this Type was derived from object filtering syntax (foo.*).
	Deref bool
}

func (ty *ArrayType) String() string {
	return fmt.Sprintf("array<%s>", ty.Elem.String())
}

// Assignable returns if other Type can be Assignable to the Type.
func (ty *ArrayType) Assignable(other Type) bool {
	switch other := other.(type) {
	case AnyType:
		return true
	case *ArrayType:
		return ty.Elem.Assignable(other.Elem)
	default:
		return false
	}
}

// Merge merges two object types into one. When other object has unknown props, they are merged into
// current object. When both have same property, when they are Assignable, it remains as-is.
// Otherwise, the property falls back to any Type.
func (ty *ArrayType) Merge(other Type) Type {
	switch other := other.(type) {
	case *ArrayType:
		if _, ok := ty.Elem.(AnyType); ok {
			return ty
		}
		if _, ok := other.Elem.(AnyType); ok {
			return other
		}
		return &ArrayType{
			Elem:  ty.Elem.Merge(other.Elem),
			Deref: false, // When fusing array deref type, it means prop deref chain breaks
		}
	default:
		return AnyType{}
	}
}

// DeepCopy duplicates itself. All its child types are copied recursively.
func (ty *ArrayType) DeepCopy() Type {
	return &ArrayType{ty.Elem.DeepCopy(), ty.Deref}
}

// EqualTypes returns if the two types are equal.
func EqualTypes(l, r Type) bool {
	return l.Assignable(r) && r.Assignable(l)
}

// TypeOfJSONValue returns the Type of the given JSON value. The JSON value is an any value decoded by json.Unmarshal.
// https://pkg.go.dev/encoding/json#Unmarshal
//
// To unmarshal JSON into an interface value, Unmarshal stores one of these in the interface value:
//   - bool, for JSON booleans
//   - float64, for JSON numbers
//   - String, for JSON strings
//   - []any, for JSON arrays
//   - map[String]any, for JSON objects
//   - nil for JSON null
func TypeOfJSONValue(v any) Type {
	switch v := v.(type) {
	case bool:
		return BoolType{}
	case float64:
		return NumberType{}
	case string:
		return StringType{}
	case []any:
		var elem Type
		for _, e := range v {
			t := TypeOfJSONValue(e)
			if elem == nil {
				elem = t
			} else {
				elem = elem.Merge(t)
			}
		}
		if elem == nil {
			elem = AnyType{}
		}
		return &ArrayType{Elem: elem}
	case map[string]any:
		props := make(map[string]Type, len(v))
		for k, v := range v {
			props[k] = TypeOfJSONValue(v)
		}
		return NewStrictObjectType(props)
	case nil:
		return NullType{}
	default:
		panic(v) // Unreachable
	}
}
