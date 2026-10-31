package expression

import (
	"fmt"
	"iter"
	"maps"
	"slices"
	"sort"
	"strings"
)

// Kind is the shape of a Type.
type Kind uint8

const (
	// KindAny is a value whose type cannot be known statically, so it cannot be
	// type-checked. It is the zero Kind, which makes the zero Type any.
	KindAny Kind = iota
	// KindNull is the type of null.
	KindNull
	// KindNumber is the type of integer and float values.
	KindNumber
	// KindBool is the type of true and false.
	KindBool
	// KindString is the type of string values.
	KindString
	// KindObject is an object type. Type.Object returns its members.
	KindObject
	// KindArray is an array type. Type.Array returns its element type.
	KindArray
)

var kindNames = [...]string{
	KindAny:    "any",
	KindNull:   "null",
	KindNumber: "number",
	KindBool:   "bool",
	KindString: "string",
	KindObject: "object",
	KindArray:  "array",
}

// String returns the name of the kind, as used in type diagnostics.
func (k Kind) String() string {
	if int(k) < len(kindNames) {
		return kindNames[k]
	}
	return fmt.Sprintf("Kind(%d)", uint8(k))
}

// Type is the type of an expression value. A Type is an immutable value:
// copying it is cheap, and no method changes it or anything it refers to. The
// zero Type is any.
//
// Build object and array types with the New*ObjectType constructors, an
// ObjectBuilder or NewArrayType, and widen them to a Type with their Type
// method. Read them back with Kind, Object and Array.
type Type struct {
	kind Kind
	// object holds the members when kind is KindObject.
	object ObjectType
	// array holds the element type when kind is KindArray.
	array ArrayType
}

// AnyType returns the type of a value that cannot be known statically. It is
// the zero Type.
func AnyType() Type { return Type{} }

// NullType returns the type of null.
func NullType() Type { return Type{kind: KindNull} }

// NumberType returns the type of integer and float values.
func NumberType() Type { return Type{kind: KindNumber} }

// BoolType returns the type of true and false.
func BoolType() Type { return Type{kind: KindBool} }

// StringType returns the type of string values.
func StringType() Type { return Type{kind: KindString} }

// Kind returns the shape of the type.
func (t Type) Kind() Kind { return t.kind }

// Object returns the object members of t. ok is false when t is not an object.
func (t Type) Object() (o ObjectType, ok bool) { return t.object, t.kind == KindObject }

// Array returns the array view of t. ok is false when t is not an array.
func (t Type) Array() (a ArrayType, ok bool) { return t.array, t.kind == KindArray }

// String returns the spelling of the type used in diagnostics.
func (t Type) String() string {
	switch t.kind {
	case KindObject:
		return t.object.String()
	case KindArray:
		return t.array.String()
	case KindAny, KindNull, KindNumber, KindBool, KindString:
	}
	return t.kind.String()
}

// Assignable reports whether a value of type other can be used where a value
// of type t is expected.
func (t Type) Assignable(other Type) bool {
	switch t.kind {
	case KindAny:
		return true
	case KindNull:
		return other.kind == KindNull || other.kind == KindAny
	case KindNumber:
		return other.kind == KindNumber || other.kind == KindAny
	case KindBool:
		// Every value converts to bool, as in `if: ${{ steps.foo }}`.
		return true
	case KindString:
		// Bool and null values also convert to string, but in almost every
		// expression such a conversion is a mistake.
		return other.kind == KindString || other.kind == KindNumber || other.kind == KindAny
	case KindObject:
		if other.kind == KindAny {
			return true
		}
		return other.kind == KindObject && t.object.assignable(other.object)
	case KindArray:
		if other.kind == KindAny {
			return true
		}
		return other.kind == KindArray && t.array.rep().elem.Assignable(other.array.rep().elem)
	}
	return false
}

// Merge returns the type of a value that has either type t or type other, as
// the operands of && and || do. Types that do not combine merge to any.
func (t Type) Merge(other Type) Type {
	switch t.kind {
	case KindAny:
		return t
	case KindNull:
		if other.kind == KindNull {
			return t
		}
	case KindNumber:
		// A number merged with a string is a string.
		if other.kind == KindNumber || other.kind == KindString {
			return other
		}
	case KindBool:
		// A bool merged with a string is a string.
		if other.kind == KindBool || other.kind == KindString {
			return other
		}
	case KindString:
		if other.kind == KindString || other.kind == KindNumber || other.kind == KindBool {
			return t
		}
	case KindObject:
		if other.kind == KindObject {
			return t.object.merge(other.object)
		}
	case KindArray:
		if other.kind == KindArray {
			return t.array.merge(other.array)
		}
	}
	return AnyType()
}

// Equal reports whether t and other describe the same type, including
// property spellings, display names and event variants. EqualTypes is the
// weaker test that each type is assignable to the other.
func (t Type) Equal(other Type) bool {
	if t.kind != other.kind {
		return false
	}
	switch t.kind {
	case KindObject:
		return t.object.Equal(other.object)
	case KindArray:
		return t.array.Equal(other.array)
	case KindAny, KindNull, KindNumber, KindBool, KindString:
	}
	return true
}

// ObjectType is an immutable object type. Its members are fixed when it is
// created; the With methods return a changed copy and leave the original as it
// was. The zero ObjectType is a strict object with no properties.
//
// A strict object allows only its declared properties. A loose object allows
// any property, typed as any. A map object, such as the env context, types
// every property it does not declare with one mapped type.
type ObjectType struct {
	data *objectData
}

type objectData struct {
	// displayName replaces the structural spelling of large types, such as
	// webhook payloads, in diagnostics.
	displayName string
	// props maps lookup keys to property types.
	props map[string]Type
	// names maps a lookup key to its declared spelling when the two differ.
	names map[string]string
	// variants are the trigger-specific payloads used to narrow
	// github.event under event-name and action guards.
	variants []EventVariant
	// mapped is the type of properties not in props. nil makes the object
	// strict, and any makes it loose.
	mapped *Type
}

// emptyObject backs the zero ObjectType. Nothing writes to it.
var emptyObject objectData

func (o ObjectType) rep() *objectData {
	if o.data == nil {
		return &emptyObject
	}
	return o.data
}

// NewEmptyObjectType returns a loose object type with no declared properties.
// Every property of it has type any.
func NewEmptyObjectType() ObjectType {
	return ObjectType{&objectData{mapped: new(AnyType())}}
}

// NewObjectType returns a loose object type with the given properties. Other
// properties have type any. The map is copied.
func NewObjectType(props map[string]Type) ObjectType {
	return ObjectType{&objectData{props: maps.Clone(props), mapped: new(AnyType())}}
}

// NewEmptyStrictObjectType returns a strict object type with no properties.
func NewEmptyStrictObjectType() ObjectType {
	return ObjectType{&objectData{}}
}

// NewStrictObjectType returns a strict object type that allows only the given
// properties. The map is copied.
func NewStrictObjectType(props map[string]Type) ObjectType {
	return ObjectType{&objectData{props: maps.Clone(props)}}
}

// NewMapObjectType returns an object type whose properties all have type t,
// such as the env context, where every property is a string.
func NewMapObjectType(t Type) ObjectType {
	return ObjectType{&objectData{mapped: new(t)}}
}

// newNamedObjectType builds a strict object whose properties are looked up
// case-insensitively but keep their declared spelling in diagnostics.
func newNamedObjectType(props map[string]Type) ObjectType {
	var b ObjectBuilder
	for name, t := range props {
		b.SetProperty(name, t)
	}
	return b.Build()
}

// Type widens o to a Type.
func (o ObjectType) Type() Type { return Type{kind: KindObject, object: o} }

// IsStrict reports whether the object allows only its declared properties.
func (o ObjectType) IsStrict() bool { return o.rep().mapped == nil }

// IsLoose reports whether the object allows any property, typed as any.
func (o ObjectType) IsLoose() bool {
	mapped := o.rep().mapped
	return mapped != nil && mapped.kind == KindAny
}

// Mapped returns the type of properties the object does not declare. ok is
// false for a strict object, which has no such properties.
func (o ObjectType) Mapped() (t Type, ok bool) {
	mapped := o.rep().mapped
	if mapped == nil {
		return Type{}, false
	}
	return *mapped, true
}

// Property returns the type declared under key. Keys are matched exactly;
// expression lookups lower-case the property name first.
func (o ObjectType) Property(key string) (Type, bool) {
	t, ok := o.rep().props[key]
	return t, ok
}

// Properties yields each declared key and its type, in no particular order.
func (o ObjectType) Properties() iter.Seq2[string, Type] {
	props := o.rep().props
	return func(yield func(string, Type) bool) {
		for key, t := range props {
			if !yield(key, t) {
				return
			}
		}
	}
}

// Len returns the number of declared properties.
func (o ObjectType) Len() int { return len(o.rep().props) }

// PropertyName returns the declared spelling of key, or key itself when the
// property was declared in lower case.
func (o ObjectType) PropertyName(key string) string {
	if name, ok := o.rep().names[key]; ok {
		return name
	}
	return key
}

// EventVariants returns a copy of the trigger-specific payloads attached by
// NewEventType. Other objects have none.
func (o ObjectType) EventVariants() []EventVariant {
	return slices.Clone(o.rep().variants)
}

// With returns a copy of o with t stored under key exactly as given. The
// declared spelling recorded for key, if any, is kept.
func (o ObjectType) With(key string, t Type) ObjectType {
	d := o.rep()
	props := maps.Clone(d.props)
	if props == nil {
		props = map[string]Type{}
	}
	props[key] = t
	return ObjectType{&objectData{displayName: d.displayName, props: props, names: d.names, variants: d.variants, mapped: d.mapped}}
}

// WithProperty returns a copy of o with t stored under the lower-cased name,
// recording name as the declared spelling when it is not already lower case.
func (o ObjectType) WithProperty(name string, t Type) ObjectType {
	d := o.rep()
	key := strings.ToLower(name)
	props := maps.Clone(d.props)
	if props == nil {
		props = map[string]Type{}
	}
	props[key] = t
	names := spell(maps.Clone(d.names), key, name)
	return ObjectType{&objectData{displayName: d.displayName, props: props, names: names, variants: d.variants, mapped: d.mapped}}
}

// WithMapped returns a copy of o in which undeclared properties have type t.
// Passing AnyType makes the copy loose.
func (o ObjectType) WithMapped(t Type) ObjectType {
	d := o.rep()
	return ObjectType{&objectData{displayName: d.displayName, props: d.props, names: d.names, variants: d.variants, mapped: new(t)}}
}

// spell records name as the declared spelling of key in names, or forgets the
// spelling of key when name is already lower case. It returns names, which it
// allocates when names is nil and a spelling must be stored.
func spell(names map[string]string, key, name string) map[string]string {
	if name == key {
		delete(names, key)
		return names
	}
	if names == nil {
		names = map[string]string{}
	}
	names[key] = name
	return names
}

// String returns the display name when one is set, "object" for a loose
// object, "{string => T}" for a map object and the sorted property list for a
// strict object.
func (o ObjectType) String() string {
	d := o.rep()
	if d.displayName != "" {
		return d.displayName
	}
	if d.mapped != nil {
		if d.mapped.kind == KindAny {
			return "object"
		}
		return fmt.Sprintf("{string => %s}", d.mapped.String())
	}

	ps := make([]string, 0, len(d.props))
	for n := range d.props {
		ps = append(ps, n)
	}
	sort.Strings(ps)

	var b strings.Builder
	b.WriteByte('{')
	for i, p := range ps {
		if i > 0 {
			b.WriteString("; ")
		}
		b.WriteString(o.PropertyName(p))
		b.WriteString(": ")
		b.WriteString(d.props[p].String())
	}
	b.WriteByte('}')

	return b.String()
}

// Equal reports whether o and other have the same properties, property
// spellings, mapped type, display name and event variants.
func (o ObjectType) Equal(other ObjectType) bool {
	a, b := o.rep(), other.rep()
	if a == b {
		return true
	}
	if a.displayName != b.displayName || len(a.props) != len(b.props) || len(a.names) != len(b.names) || len(a.variants) != len(b.variants) {
		return false
	}
	if (a.mapped == nil) != (b.mapped == nil) || a.mapped != nil && !a.mapped.Equal(*b.mapped) {
		return false
	}
	for key, t := range a.props {
		if u, ok := b.props[key]; !ok || !t.Equal(u) {
			return false
		}
	}
	for key, name := range a.names {
		if other, ok := b.names[key]; !ok || name != other {
			return false
		}
	}
	for i, variant := range a.variants {
		w := b.variants[i]
		if variant.Event != w.Event || variant.Action != w.Action || !variant.Payload.Equal(w.Payload) {
			return false
		}
	}
	return true
}

// assignable reports whether other's properties satisfy o.
func (o ObjectType) assignable(other ObjectType) bool {
	a, b := o.rep(), other.rep()
	if a.mapped != nil {
		if b.mapped != nil {
			return a.mapped.Assignable(*b.mapped)
		}
		for _, t := range b.props {
			if !a.mapped.Assignable(t) {
				return false
			}
		}
		return true
	}
	// o is strict.

	if b.mapped != nil {
		for _, t := range a.props {
			if !t.Assignable(*b.mapped) {
				return false
			}
		}
		return true
	}
	// o and other are strict.

	for n, r := range b.props {
		if l, ok := a.props[n]; !ok || !l.Assignable(r) {
			return false
		}
	}
	return true
}

// merge unions the properties of two objects and merges the types of shared
// keys. The result has no event variants.
func (o ObjectType) merge(other ObjectType) Type {
	a, b := o.rep(), other.rep()
	if len(a.props) == 0 && other.IsLoose() {
		return other.Type()
	}
	if len(b.props) == 0 && o.IsLoose() {
		return o.Type()
	}

	mapped := a.mapped
	if mapped == nil {
		mapped = b.mapped
	} else if b.mapped != nil {
		mapped = new(mapped.Merge(*b.mapped))
	}

	props := maps.Clone(a.props)
	if props == nil {
		props = make(map[string]Type, len(b.props))
	}
	for n, r := range b.props {
		if l, ok := props[n]; ok {
			props[n] = l.Merge(r)
		} else {
			props[n] = r
			if mapped != nil {
				mapped = new(mapped.Merge(r))
			}
		}
	}

	return ObjectType{&objectData{
		props:       props,
		mapped:      mapped,
		displayName: a.displayName,
		names:       mergePropertyNames(a, b),
	}}.Type()
}

// mergePropertyNames joins the declared spellings of two objects. a's
// spelling wins for a key both declare.
func mergePropertyNames(a, b *objectData) map[string]string {
	if a.names == nil && b.names == nil {
		return nil
	}
	names := maps.Clone(a.names)
	if names == nil {
		names = make(map[string]string, len(b.names))
	}
	for key, name := range b.names {
		if _, declared := names[key]; !declared {
			names[key] = name
		}
	}
	return names
}

// ObjectBuilder collects the members of a new ObjectType. The zero
// ObjectBuilder is ready to use and describes a strict object with no
// properties. Build copies what was collected, so later changes to the builder
// do not affect object types it already built.
type ObjectBuilder struct {
	data objectData
}

// Set stores t under key exactly as given, without recording a spelling.
func (b *ObjectBuilder) Set(key string, t Type) {
	if b.data.props == nil {
		b.data.props = map[string]Type{}
	}
	b.data.props[key] = t
}

// SetProperty stores t under the lower-cased name, so expressions find it
// case-insensitively, and records name as the spelling used in diagnostics.
func (b *ObjectBuilder) SetProperty(name string, t Type) {
	key := strings.ToLower(name)
	b.Set(key, t)
	b.data.names = spell(b.data.names, key, name)
}

// Has reports whether a property is stored under key.
func (b *ObjectBuilder) Has(key string) bool {
	_, ok := b.data.props[key]
	return ok
}

// SetMapped gives undeclared properties type t. Passing AnyType makes the
// object loose.
func (b *ObjectBuilder) SetMapped(t Type) {
	b.data.mapped = new(t)
}

// SetDisplayName replaces the structural spelling of the object in
// diagnostics with name.
func (b *ObjectBuilder) SetDisplayName(name string) {
	b.data.displayName = name
}

// Build returns an object type holding the collected members.
func (b *ObjectBuilder) Build() ObjectType {
	data := b.data
	data.props = maps.Clone(b.data.props)
	data.names = maps.Clone(b.data.names)
	data.variants = slices.Clone(b.data.variants)
	return ObjectType{&data}
}

// ArrayType is an immutable array type. The zero ArrayType is array<any>.
type ArrayType struct {
	data *arrayData
}

type arrayData struct {
	elem Type
	// deref is true when the array came from object filtering, as in foo.*.
	// A property access on it maps over the elements.
	deref bool
}

// emptyArray backs the zero ArrayType. Nothing writes to it.
var emptyArray arrayData

func (a ArrayType) rep() *arrayData {
	if a.data == nil {
		return &emptyArray
	}
	return a.data
}

// NewArrayType returns the type of arrays whose elements have type elem.
func NewArrayType(elem Type) ArrayType {
	return ArrayType{&arrayData{elem: elem}}
}

// newFilteredArrayType returns the array type produced by object filtering
// syntax such as foo.*.
func newFilteredArrayType(elem Type) ArrayType {
	return ArrayType{&arrayData{elem: elem, deref: true}}
}

// Type widens a to a Type.
func (a ArrayType) Type() Type { return Type{kind: KindArray, array: a} }

// Elem returns the element type.
func (a ArrayType) Elem() Type { return a.rep().elem }

// Deref reports whether the array came from object filtering syntax, as in
// foo.*, so that a following property access maps over its elements.
func (a ArrayType) Deref() bool { return a.rep().deref }

// String returns "array<T>" for element type T.
func (a ArrayType) String() string {
	return fmt.Sprintf("array<%s>", a.rep().elem.String())
}

// Equal reports whether a and other have equal element types and both or
// neither came from object filtering.
func (a ArrayType) Equal(other ArrayType) bool {
	x, y := a.rep(), other.rep()
	return x.deref == y.deref && x.elem.Equal(y.elem)
}

// merge combines the element types of two arrays.
func (a ArrayType) merge(other ArrayType) Type {
	x, y := a.rep(), other.rep()
	if x.elem.kind == KindAny {
		return a.Type()
	}
	if y.elem.kind == KindAny {
		return other.Type()
	}
	// The merged array is no longer the direct result of object filtering.
	return NewArrayType(x.elem.Merge(y.elem)).Type()
}

// EqualTypes reports whether each type is assignable to the other.
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
		return BoolType()
	case float64:
		return NumberType()
	case string:
		return StringType()
	case []any:
		if len(v) == 0 {
			return NewArrayType(AnyType()).Type()
		}
		elem := TypeOfJSONValue(v[0])
		for _, e := range v[1:] {
			elem = elem.Merge(TypeOfJSONValue(e))
		}
		return NewArrayType(elem).Type()
	case map[string]any:
		props := make(map[string]Type, len(v))
		for k, v := range v {
			props[k] = TypeOfJSONValue(v)
		}
		return ObjectType{&objectData{props: props}}.Type()
	case nil:
		return NullType()
	default:
		panic(v) // Unreachable
	}
}
