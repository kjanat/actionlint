package exprtypes

import (
	"fmt"
	"iter"
	"maps"
	"slices"
	"sort"
	"strings"
)

type TypeKind uint8

const (
	KindAny TypeKind = iota
	KindNull
	KindNumber
	KindBool
	KindString
	KindObject
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

func (k TypeKind) String() string {
	if int(k) < len(kindNames) {
		return kindNames[k]
	}
	return fmt.Sprintf("Kind(%d)", uint8(k))
}

// Type is an immutable expression type. The zero Type is any.
type Type struct {
	kind   TypeKind
	object ObjectType
	array  ArrayType
}

func AnyType() Type { return Type{} }

func NullType() Type { return Type{kind: KindNull} }

func NumberType() Type { return Type{kind: KindNumber} }

func BoolType() Type { return Type{kind: KindBool} }

func StringType() Type { return Type{kind: KindString} }

func (t Type) Kind() TypeKind { return t.kind }

func (t Type) Object() (o ObjectType, ok bool) { return t.object, t.kind == KindObject }

func (t Type) Array() (a ArrayType, ok bool) { return t.array, t.kind == KindArray }

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
	case KindNumber, KindBool, KindString:
		if t.kind == other.kind {
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
// property spellings, display names and event variants.
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

// ObjectType is an immutable object type. The zero ObjectType is a strict
// object with no properties.
type ObjectType struct {
	data *objectData
}

type objectData struct {
	displayName string
	props       map[string]Type
	names       map[string]string
	variants    []EventVariant
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

func NewEmptyObjectType() ObjectType {
	return ObjectType{&objectData{mapped: new(AnyType())}}
}

// NewObjectType returns a loose object type with the given properties. Other
// properties have type any. The map is copied.
func NewObjectType(props map[string]Type) ObjectType {
	return ObjectType{&objectData{props: maps.Clone(props), mapped: new(AnyType())}}
}

func NewEmptyStrictObjectType() ObjectType {
	return ObjectType{&objectData{}}
}

// NewStrictObjectType returns a strict object type that allows only the given
// properties. The map is copied.
func NewStrictObjectType(props map[string]Type) ObjectType {
	return ObjectType{&objectData{props: maps.Clone(props)}}
}

func NewMapObjectType(t Type) ObjectType {
	return ObjectType{&objectData{mapped: new(t)}}
}

// NewNamedObjectType builds a strict object whose properties are looked up
// case-insensitively but keep their declared spelling in diagnostics.
func NewNamedObjectType(props map[string]Type) ObjectType {
	var b ObjectBuilder
	for name, t := range props {
		b.SetProperty(name, t)
	}
	return b.Build()
}

func (o ObjectType) Type() Type { return Type{kind: KindObject, object: o} }

func (o ObjectType) IsStrict() bool { return o.rep().mapped == nil }

func (o ObjectType) IsLoose() bool {
	mapped := o.rep().mapped
	return mapped != nil && mapped.kind == KindAny
}

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

func (o ObjectType) Len() int { return len(o.rep().props) }

func (o ObjectType) PropertyName(key string) string {
	if name, ok := o.rep().names[key]; ok {
		return name
	}
	return key
}

// EventVariants returns a copy of the trigger-specific payloads.
func (o ObjectType) EventVariants() []EventVariant {
	return slices.Clone(o.rep().variants)
}

// WithEventVariants returns an object with a detached copy of the supplied
// trigger-specific payloads.
func (o ObjectType) WithEventVariants(variants []EventVariant) ObjectType {
	data := *o.rep()
	data.variants = slices.Clone(variants)
	return ObjectType{&data}
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

func (o ObjectType) WithMapped(t Type) ObjectType {
	d := o.rep()
	return ObjectType{&objectData{displayName: d.displayName, props: d.props, names: d.names, variants: d.variants, mapped: new(t)}}
}

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

	if b.mapped != nil {
		for _, t := range a.props {
			if !t.Assignable(*b.mapped) {
				return false
			}
		}
		return true
	}

	for n, r := range b.props {
		if l, ok := a.props[n]; !ok || !l.Assignable(r) {
			return false
		}
	}
	return true
}

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

type ObjectBuilder struct {
	data objectData
}

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

func (b *ObjectBuilder) Has(key string) bool {
	_, ok := b.data.props[key]
	return ok
}

func (b *ObjectBuilder) SetMapped(t Type) {
	b.data.mapped = new(t)
}

func (b *ObjectBuilder) SetDisplayName(name string) {
	b.data.displayName = name
}

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
	elem  Type
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

func NewArrayType(elem Type) ArrayType {
	return ArrayType{&arrayData{elem: elem}}
}

func NewFilteredArrayType(elem Type) ArrayType {
	return ArrayType{&arrayData{elem: elem, deref: true}}
}

func (a ArrayType) Type() Type { return Type{kind: KindArray, array: a} }

func (a ArrayType) Elem() Type { return a.rep().elem }

// Deref reports whether the array came from object filtering syntax, as in
// foo.*. A following property access then maps over the array's elements.
func (a ArrayType) Deref() bool { return a.rep().deref }

func (a ArrayType) String() string {
	return fmt.Sprintf("array<%s>", a.rep().elem.String())
}

func (a ArrayType) Equal(other ArrayType) bool {
	x, y := a.rep(), other.rep()
	return x.deref == y.deref && x.elem.Equal(y.elem)
}

func (a ArrayType) merge(other ArrayType) Type {
	x, y := a.rep(), other.rep()
	if x.elem.kind == KindAny {
		return a.Type()
	}
	if y.elem.kind == KindAny {
		return other.Type()
	}
	return NewArrayType(x.elem.Merge(y.elem)).Type()
}

func EqualTypes(l, r Type) bool {
	return l.Assignable(r) && r.Assignable(l)
}

// TypeOfJSONValue returns the Type of a value that json.Unmarshal decoded into an any.
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
