package expression

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func TestExprNewMapObjectType(t *testing.T) {
	o := NewMapObjectType(StringType())
	if o.Len() != 0 {
		t.Fatalf("props should be empty but %v", o)
	}
	if mapped, ok := o.Mapped(); !ok || mapped.Kind() != KindString {
		t.Fatalf("mapped type is not string: %v", mapped)
	}
	if o.IsStrict() {
		t.Fatalf("map object is not strict object but got %v", o)
	}
}

func TestExprObjectTypeSetStrict(t *testing.T) {
	o := NewEmptyObjectType()
	if o.IsStrict() || !o.IsLoose() {
		t.Fatal("should be loose")
	}
	o = NewEmptyStrictObjectType()
	if !o.IsStrict() || o.IsLoose() {
		t.Fatal("should be strict")
	}
	o = o.WithMapped(AnyType())
	if o.IsStrict() || !o.IsLoose() {
		t.Fatal("should be loose")
	}
	if !(ObjectType{}).IsStrict() {
		t.Fatal("zero object type should be strict")
	}
}

func TestExprAssignableSimple(t *testing.T) {
	testCases := []Type{
		AnyType(),
		NullType(),
		NumberType(),
		BoolType(),
		StringType(),
		NewObjectType(map[string]Type{"n": NumberType()}).Type(),
		NewStrictObjectType(map[string]Type{"b": BoolType()}).Type(),
		NewMapObjectType(NullType()).Type(),
		NewArrayType(StringType()).Type(),
	}

	for _, ty := range testCases {
		s := ty.String()
		t.Run(s, func(t *testing.T) {
			if !ty.Assignable(ty) {
				t.Fatalf("%s is not self-assignable", ty)
			}

			switch ty.Kind() {
			case KindNull, KindAny:
			case KindNumber, KindBool, KindString, KindObject, KindArray:
				if NullType().Assignable(ty) {
					t.Fatalf("%s is assignable to null", ty)
				}
			}

			if !AnyType().Assignable(ty) {
				t.Fatalf("%s is not assignable to any", ty)
			}
		})
	}
}

func TestExprAssignableObject(t *testing.T) {
	testCases := []struct {
		from, to Type
		no       bool
	}{
		{
			from: NewMapObjectType(NumberType()).Type(),
			to:   NewMapObjectType(StringType()).Type(),
		},
		{
			from: NewEmptyObjectType().Type(),
			to:   NewMapObjectType(StringType()).Type(),
		},
		{
			from: NewMapObjectType(StringType()).Type(),
			to:   NewEmptyObjectType().Type(),
		},
		{
			from: NewStrictObjectType(map[string]Type{
				"a": NumberType(),
				"b": StringType(),
			}).Type(),
			to: NewMapObjectType(StringType()).Type(),
		},
		{
			from: NewStrictObjectType(map[string]Type{"a": NullType()}).Type(),
			to:   NewMapObjectType(StringType()).Type(),
			no:   true,
		},
		{
			from: NewMapObjectType(NumberType()).Type(),
			to: NewStrictObjectType(map[string]Type{
				"a": AnyType(),
				"b": StringType(),
			}).Type(),
		},
		{
			from: NewMapObjectType(NumberType()).Type(),
			to: NewStrictObjectType(map[string]Type{
				"a": NullType(),
				"b": StringType(),
			}).Type(),
			no: true,
		},
		{
			from: NewStrictObjectType(map[string]Type{"a": NumberType()}).Type(),
			to:   NewStrictObjectType(map[string]Type{"a": StringType()}).Type(),
		},
		{
			from: NewStrictObjectType(map[string]Type{"a": StringType()}).Type(),
			to:   NewStrictObjectType(map[string]Type{"b": StringType()}).Type(),
			no:   true,
		},
		{
			from: NewStrictObjectType(map[string]Type{"a": NullType()}).Type(),
			to:   NewStrictObjectType(map[string]Type{"a": StringType()}).Type(),
			no:   true,
		},
	}

	for _, tc := range testCases {
		l, r := tc.to.String(), tc.from.String()
		t.Run(l+" := "+r, func(t *testing.T) {
			if tc.to.Assignable(tc.from) == tc.no {
				not := ""
				if tc.no {
					not = " not"
				}
				t.Fatalf("%s should%s be assignable to %s", r, not, l)
			}
		})
	}
}

func TestExprEqualTypes(t *testing.T) {
	testCases := []struct {
		what string
		ty   Type
		// neq and eq hold at most one type each.
		neq []Type
		eq  []Type
	}{
		{
			what: "null",
			ty:   NullType(),
			neq:  []Type{StringType()},
		},
		{
			what: "number",
			ty:   NumberType(),
			neq:  []Type{StringType()},
		},
		{
			what: "bool",
			ty:   BoolType(),
			neq:  []Type{StringType()},
		},
		{
			what: "string",
			ty:   StringType(),
			neq:  []Type{BoolType()},
		},
		{
			what: "object",
			ty:   NewEmptyObjectType().Type(),
			neq:  []Type{NewArrayType(AnyType()).Type()},
		},
		{
			what: "strict props object",
			ty:   NewEmptyStrictObjectType().Type(),
			neq:  []Type{NewArrayType(AnyType()).Type()},
		},
		{
			what: "nested object",
			ty: NewObjectType(map[string]Type{
				"foo": NewObjectType(map[string]Type{
					"bar": StringType(),
				}).Type(),
			}).Type(),
			neq: []Type{NewArrayType(AnyType()).Type()},
		},
		{
			what: "nested strict props object",
			ty: NewStrictObjectType(map[string]Type{
				"foo": NewStrictObjectType(map[string]Type{
					"bar": StringType(),
				}).Type(),
			}).Type(),
			neq: []Type{NewArrayType(AnyType()).Type()},
		},
		{
			what: "nested object prop name",
			ty: NewStrictObjectType(map[string]Type{
				"foo": StringType(),
			}).Type(),
			neq: []Type{NewStrictObjectType(map[string]Type{
				"bar": StringType(),
			}).Type()},
		},
		{
			what: "nested object prop type",
			ty: NewStrictObjectType(map[string]Type{
				"foo": StringType(),
			}).Type(),
			neq: []Type{NewStrictObjectType(map[string]Type{
				"foo": BoolType(),
			}).Type()},
		},
		{
			what: "strict props object and loose object",
			ty: NewStrictObjectType(map[string]Type{
				"foo": NullType(),
			}).Type(),
			eq: []Type{NewObjectType(map[string]Type{
				"foo": NullType(),
			}).Type()},
		},
		{
			what: "loose object and strict props object",
			ty: NewObjectType(map[string]Type{
				"foo": NullType(),
			}).Type(),
			eq: []Type{NewObjectType(map[string]Type{
				"foo": NullType(),
			}).Type()},
		},
		{
			what: "map objects",
			ty:   NewMapObjectType(NullType()).Type(),
			eq:   []Type{NewMapObjectType(NullType()).Type()},
			neq:  []Type{NewMapObjectType(NumberType()).Type()},
		},
		{
			what: "map object equals loose object",
			ty:   NewMapObjectType(StringType()).Type(),
			eq:   []Type{NewEmptyObjectType().Type()},
		},
		{
			what: "loose object equals map object",
			ty:   NewEmptyObjectType().Type(),
			eq:   []Type{NewMapObjectType(StringType()).Type()},
		},
		{
			what: "map object equals strict object",
			ty:   NewMapObjectType(StringType()).Type(),
			eq: []Type{NewStrictObjectType(map[string]Type{
				"foo": StringType(),
			}).Type()},
			neq: []Type{NewStrictObjectType(map[string]Type{
				"foo": NullType(),
			}).Type()},
		},
		{
			what: "map object equals strict object including any prop",
			ty:   NewMapObjectType(StringType()).Type(),
			eq: []Type{NewStrictObjectType(map[string]Type{
				"foo": StringType(),
				"bar": AnyType(),
			}).Type()},
			neq: []Type{NewStrictObjectType(map[string]Type{
				"foo": NullType(),
				"bar": AnyType(),
			}).Type()},
		},
		{
			what: "strict object equals map object",
			ty: NewStrictObjectType(map[string]Type{
				"foo": StringType(),
			}).Type(),
			eq:  []Type{NewMapObjectType(StringType()).Type()},
			neq: []Type{NewMapObjectType(NullType()).Type()},
		},
		{
			what: "array",
			ty:   NewArrayType(StringType()).Type(),
			neq:  []Type{NewEmptyObjectType().Type()},
		},
		{
			what: "array element type",
			ty:   NewArrayType(StringType()).Type(),
			neq:  []Type{NewArrayType(BoolType()).Type()},
		},
		{
			what: "nested array",
			ty:   NewArrayType(NewArrayType(StringType()).Type()).Type(),
			neq:  []Type{NewArrayType(NewArrayType(BoolType()).Type()).Type()},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.what, func(t *testing.T) {
			var l, r Type

			l, r = tc.ty, tc.ty
			if !EqualTypes(l, r) {
				t.Errorf("%s should equal to %s", l.String(), r.String())
			}
			for _, neq := range tc.neq {
				l, r = tc.ty, neq
				if EqualTypes(l, r) {
					t.Errorf("%s should not equal to %s", l.String(), r.String())
				}
			}
			for _, eq := range tc.eq {
				l, r = tc.ty, eq
				if !EqualTypes(l, r) {
					t.Errorf("%s should equal to %s", l.String(), r.String())
				}
			}
			l, r = tc.ty, AnyType()
			if !EqualTypes(l, r) {
				t.Errorf("%s should equal to %s", l.String(), r.String())
			}
			l, r = AnyType(), tc.ty
			if !EqualTypes(l, r) {
				t.Errorf("%s should equal to %s", l.String(), r.String())
			}
		})
	}
}

func TestExprTypeStringize(t *testing.T) {
	testCases := []struct {
		what string
		ty   Type
		want string
	}{
		{
			what: "any",
			ty:   AnyType(),
			want: "any",
		},
		{
			what: "null",
			ty:   NullType(),
			want: "null",
		},
		{
			what: "number",
			ty:   NumberType(),
			want: "number",
		},
		{
			what: "bool",
			ty:   BoolType(),
			want: "bool",
		},
		{
			what: "string",
			ty:   StringType(),
			want: "string",
		},
		{
			what: "empty object",
			ty:   NewEmptyObjectType().Type(),
			want: "object",
		},
		{
			what: "empty strict props object",
			ty:   NewEmptyStrictObjectType().Type(),
			want: "{}",
		},
		{
			what: "strict object",
			ty: NewStrictObjectType(map[string]Type{
				"foo": StringType(),
			}).Type(),
			want: "{foo: string}",
		},
		{
			what: "loose object",
			ty: NewObjectType(map[string]Type{
				"foo": StringType(),
			}).Type(),
			want: "object",
		},
		{
			what: "strict object",
			ty: NewStrictObjectType(map[string]Type{
				"foo": StringType(),
			}).Type(),
			want: "{foo: string}",
		},
		{
			what: "multiple props object",
			ty: NewStrictObjectType(map[string]Type{
				"foo": StringType(),
				"bar": NumberType(),
			}).Type(),
			want: "{bar: number; foo: string}",
		},
		{
			what: "nested objects",
			ty: NewStrictObjectType(map[string]Type{
				"foo": StringType(),
				"nested": NewStrictObjectType(map[string]Type{
					"foo": StringType(),
					"bar": NumberType(),
				}).Type(),
			}).Type(),
			want: "{foo: string; nested: {bar: number; foo: string}}",
		},
		{
			what: "array",
			ty:   NewArrayType(AnyType()).Type(),
			want: "array<any>",
		},
		{
			what: "nested arrays",
			ty:   NewArrayType(newFilteredArrayType(BoolType()).Type()).Type(),
			want: "array<array<bool>>",
		},
		{
			what: "array nested in object",
			ty: NewStrictObjectType(map[string]Type{
				"foo": NewArrayType(
					NewStrictObjectType(map[string]Type{
						"bar": NewArrayType(StringType()).Type(),
					}).Type(),
				).Type(),
			}).Type(),
			want: "{foo: array<{bar: array<string>}>}",
		},
		{
			what: "map object",
			ty:   NewMapObjectType(NumberType()).Type(),
			want: "{string => number}",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.what, func(t *testing.T) {
			have := tc.ty.String()
			if have != tc.want {
				t.Fatalf("wanted %q but got %q", tc.want, have)
			}
		})
	}
}

func TestExprTypeMergeSimple(t *testing.T) {
	testCases := []Type{
		AnyType(),
		NullType(),
		NumberType(),
		BoolType(),
		StringType(),
		NewEmptyObjectType().Type(),
		NewEmptyStrictObjectType().Type(),
		NewMapObjectType(NullType()).Type(),
		NewArrayType(StringType()).Type(),
	}

	opt := cmpopts.EquateEmpty()

	for _, ty := range testCases {
		t.Run("any/"+ty.String(), func(t *testing.T) {
			have := ty.Merge(AnyType())
			if have.Kind() != KindAny {
				t.Errorf("any type merged with %s was %s while expecting any", ty.String(), have.String())
			}

			have = AnyType().Merge(ty)
			if have.Kind() != KindAny {
				t.Errorf("%s merged with any type was %s while expecting any", ty.String(), have.String())
			}
		})
	}

	for _, ty := range testCases {
		t.Run("incompatible/"+ty.String(), func(t *testing.T) {
			in := NullType()
			if ty == NullType() {
				in = StringType() // null is compatible with null so use string instead
			}

			have := ty.Merge(in)
			if have.Kind() != KindAny {
				t.Errorf("incompatible %s type merged with %s was %s while expecting any", in.String(), ty.String(), have.String())
			}
		})
	}

	for _, ty := range testCases {
		t.Run("self/"+ty.String(), func(t *testing.T) {
			have := ty.Merge(ty)
			if !cmp.Equal(ty, have, opt) {
				s := ty.String()
				t.Errorf("%s merged with %s was %s while expecting %s", s, s, have.String(), s)
			}
		})
	}
}

func TestExprTypeMergeComplicated(t *testing.T) {
	testCases := []struct {
		what string
		ty   Type
		with Type
		want Type
	}{
		{
			what: "number merges with string",
			ty:   NumberType(),
			with: StringType(),
			want: StringType(),
		},
		{
			what: "string is merged by number",
			ty:   StringType(),
			with: NumberType(),
			want: StringType(),
		},
		{
			what: "bool merges with string",
			ty:   BoolType(),
			with: StringType(),
			want: StringType(),
		},
		{
			what: "string is merged by bool",
			ty:   StringType(),
			with: BoolType(),
			want: StringType(),
		},
		{
			what: "object props",
			ty: NewObjectType(map[string]Type{
				"foo": NumberType(),
			}).Type(),
			with: NewObjectType(map[string]Type{
				"bar": StringType(),
			}).Type(),
			want: NewObjectType(map[string]Type{
				"foo": NumberType(),
				"bar": StringType(),
			}).Type(),
		},
		{
			what: "loose object with strict object",
			ty: NewObjectType(map[string]Type{
				"foo": NumberType(),
			}).Type(),
			with: NewStrictObjectType(map[string]Type{
				"bar": StringType(),
			}).Type(),
			want: NewObjectType(map[string]Type{
				"foo": NumberType(),
				"bar": StringType(),
			}).Type(),
		},
		{
			what: "strict object with strict object",
			ty: NewStrictObjectType(map[string]Type{
				"foo": NumberType(),
			}).Type(),
			with: NewStrictObjectType(map[string]Type{
				"bar": StringType(),
			}).Type(),
			want: NewStrictObjectType(map[string]Type{
				"foo": NumberType(),
				"bar": StringType(),
			}).Type(),
		},
		{
			what: "compatible prop",
			ty: NewObjectType(map[string]Type{
				"foo": NumberType(),
			}).Type(),
			with: NewObjectType(map[string]Type{
				"foo": StringType(),
			}).Type(),
			want: NewObjectType(map[string]Type{
				"foo": StringType(),
			}).Type(),
		},
		{
			what: "any prop with prop",
			ty: NewObjectType(map[string]Type{
				"foo": AnyType(),
			}).Type(),
			with: NewObjectType(map[string]Type{
				"foo": StringType(),
			}).Type(),
			want: NewObjectType(map[string]Type{
				"foo": AnyType(),
			}).Type(),
		},
		{
			what: "prop with any prop",
			ty: NewObjectType(map[string]Type{
				"foo": StringType(),
			}).Type(),
			with: NewObjectType(map[string]Type{
				"foo": AnyType(),
			}).Type(),
			want: NewObjectType(map[string]Type{
				"foo": AnyType(),
			}).Type(),
		},
		{
			what: "incompatible prop",
			ty: NewObjectType(map[string]Type{
				"foo": NullType(),
			}).Type(),
			with: NewObjectType(map[string]Type{
				"foo": StringType(),
			}).Type(),
			want: NewObjectType(map[string]Type{
				"foo": AnyType(),
			}).Type(),
		},
		{
			what: "compatible array element",
			ty:   NewArrayType(NumberType()).Type(),
			with: NewArrayType(StringType()).Type(),
			want: NewArrayType(StringType()).Type(),
		},
		{
			what: "incompatible array element",
			ty:   NewArrayType(NullType()).Type(),
			with: NewArrayType(StringType()).Type(),
			want: NewArrayType(AnyType()).Type(),
		},
		{
			what: "any array element with element",
			ty:   NewArrayType(AnyType()).Type(),
			with: NewArrayType(StringType()).Type(),
			want: NewArrayType(AnyType()).Type(),
		},
		{
			what: "array element with any element",
			ty:   NewArrayType(StringType()).Type(),
			with: NewArrayType(AnyType()).Type(),
			want: NewArrayType(AnyType()).Type(),
		},
		{
			what: "array with array deref",
			ty:   NewArrayType(StringType()).Type(),
			with: newFilteredArrayType(StringType()).Type(),
			want: NewArrayType(StringType()).Type(),
		},
		{
			what: "array deref with array",
			ty:   newFilteredArrayType(StringType()).Type(),
			with: NewArrayType(StringType()).Type(),
			want: NewArrayType(StringType()).Type(),
		},
		{
			what: "array deref with array deref",
			ty:   newFilteredArrayType(StringType()).Type(),
			with: newFilteredArrayType(StringType()).Type(),
			want: NewArrayType(StringType()).Type(),
		},
		{
			what: "object no prop at left hand side",
			ty:   NewEmptyObjectType().Type(),
			with: NewObjectType(map[string]Type{
				"foo": StringType(),
			}).Type(),
			want: NewObjectType(map[string]Type{
				"foo": StringType(),
			}).Type(),
		},
		{
			what: "object no prop at right hand side",
			ty: NewObjectType(map[string]Type{
				"foo": StringType(),
			}).Type(),
			with: NewEmptyObjectType().Type(),
			want: NewObjectType(map[string]Type{
				"foo": StringType(),
			}).Type(),
		},
		{
			what: "any elem array at left hand side",
			ty:   NewArrayType(AnyType()).Type(),
			with: NewArrayType(StringType()).Type(),
			want: NewArrayType(AnyType()).Type(),
		},
		{
			what: "any elem array at right hand side",
			ty:   NewArrayType(StringType()).Type(),
			with: NewArrayType(AnyType()).Type(),
			want: NewArrayType(AnyType()).Type(),
		},
		{
			what: "nested array",
			ty:   NewArrayType(NewArrayType(NumberType()).Type()).Type(),
			with: NewArrayType(NewArrayType(StringType()).Type()).Type(),
			want: NewArrayType(NewArrayType(StringType()).Type()).Type(),
		},
		{
			what: "nested objects",
			ty: NewObjectType(map[string]Type{
				"foo": NewObjectType(map[string]Type{
					"foo":  NumberType(),
					"piyo": NumberType(),
				}).Type(),
				"aaa": NumberType(),
				"ccc": NumberType(),
			}).Type(),
			with: NewObjectType(map[string]Type{
				"foo": NewObjectType(map[string]Type{
					"bar":  StringType(),
					"piyo": StringType(),
				}).Type(),
				"bbb": StringType(),
				"ccc": StringType(),
			}).Type(),
			want: NewObjectType(map[string]Type{
				"foo": NewObjectType(map[string]Type{
					"foo":  NumberType(),
					"bar":  StringType(),
					"piyo": StringType(),
				}).Type(),
				"aaa": NumberType(),
				"bbb": StringType(),
				"ccc": StringType(),
			}).Type(),
		},
		{
			what: "map object with compatible map object",
			ty:   NewMapObjectType(NumberType()).Type(),
			with: NewMapObjectType(StringType()).Type(),
			want: NewMapObjectType(StringType()).Type(),
		},
		{
			what: "map object with incompatible map object",
			ty:   NewMapObjectType(NumberType()).Type(),
			with: NewMapObjectType(NullType()).Type(),
			want: NewEmptyObjectType().Type(),
		},
		{
			what: "map object with compatible object",
			ty:   NewMapObjectType(NumberType()).Type(),
			with: NewObjectType(map[string]Type{
				"foo": NumberType(),
			}).Type(),
			want: NewObjectType(map[string]Type{
				"foo": NumberType(),
			}).Type(),
		},
		{
			what: "map object with incompatible object",
			ty:   NewMapObjectType(NumberType()).Type(),
			with: NewObjectType(map[string]Type{
				"foo": BoolType(),
			}).Type(),
			want: NewObjectType(map[string]Type{
				"foo": BoolType(),
			}).Type(),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.what, func(t *testing.T) {
			opt := cmpopts.EquateEmpty()
			ty := tc.with.Merge(tc.ty)
			if diff := cmp.Diff(tc.want, ty, opt); diff != "" {
				t.Fatalf(
					`%s was merged with %s as %s while expecting %s
diff:
%s`,
					tc.ty.String(),
					tc.with.String(),
					ty.String(),
					tc.want.String(),
					diff,
				)
			}
		})
	}
}

func TestExprTypeMergeCreateNewInstance(t *testing.T) {
	{
		ty := NewArrayType(NumberType()).Type()
		ty2 := ty.Merge(NewArrayType(StringType()).Type())
		if ty == ty2 {
			t.Fatalf("did not make a new instance (%v => %v)", ty, ty2)
		}
		if array, _ := ty.Array(); array.Elem().Kind() != KindNumber {
			t.Fatalf("original element type was modified: %v", ty)
		}
	}

	{
		object := NewObjectType(map[string]Type{
			"foo": NumberType(),
		})
		ty := object.Type()
		ty2 := ty.Merge(
			NewObjectType(map[string]Type{
				"foo": StringType(),
				"bar": BoolType(),
			}).Type(),
		)
		if ty == ty2 {
			t.Fatalf("did not make a new instance (%v => %v)", ty, ty2)
		}
		if object.Len() != 1 {
			t.Fatalf("new prop was added: %v", ty)
		}
		if foo, _ := object.Property("foo"); foo.Kind() != KindNumber {
			t.Fatalf("prop type was modified: %v", ty)
		}
	}
}

func TestExprTypeDerivedObjectsLeaveOriginalUnchanged(t *testing.T) {
	props := map[string]Type{"foo": NumberType()}
	o := NewObjectType(props)
	props["foo"] = BoolType()
	props["bar"] = StringType()
	if o.Len() != 1 {
		t.Fatal("constructor kept the caller's map", o)
	}
	if foo, _ := o.Property("foo"); foo.Kind() != KindNumber {
		t.Fatal("o.foo is not number", foo)
	}

	derived := []ObjectType{
		o.With("foo", BoolType()),
		o.WithProperty("Foo", BoolType()),
		o.WithMapped(NumberType()),
	}
	for _, d := range derived {
		if d.Equal(o) {
			t.Fatal("derived object equals the original", d)
		}
	}
	if foo, _ := o.Property("foo"); foo.Kind() != KindNumber {
		t.Fatal("o.foo is not number", foo)
	}
	if o.PropertyName("foo") != "foo" {
		t.Fatal("o.foo spelling changed", o.PropertyName("foo"))
	}
	if mapped, _ := o.Mapped(); mapped.Kind() != KindAny {
		t.Fatal("mapped type of o is not any", mapped)
	}

	var b ObjectBuilder
	b.SetProperty("Piyo", StringType())
	built := b.Build()
	b.Set("piyo", NullType())
	b.SetProperty("other", NullType())
	b.SetMapped(AnyType())
	if piyo, _ := built.Property("piyo"); piyo.Kind() != KindString || built.Len() != 1 || !built.IsStrict() {
		t.Fatal("builder changes reached an object it already built", built)
	}
	if built.PropertyName("piyo") != "Piyo" {
		t.Fatal("built object lost its declared spelling", built.PropertyName("piyo"))
	}
}

func TestExprTypeTypeOfJSONValue(t *testing.T) {
	tests := []struct {
		what  string
		value any
		want  Type
	}{
		{
			what:  "null",
			value: nil,
			want:  NullType(),
		},
		{
			what:  "num",
			value: 1.0,
			want:  NumberType(),
		},
		{
			what:  "string",
			value: "hello",
			want:  StringType(),
		},
		{
			what:  "bool",
			value: true,
			want:  BoolType(),
		},
		{
			what:  "empty array",
			value: []any{},
			want:  NewArrayType(AnyType()).Type(),
		},
		{
			what:  "array",
			value: []any{"hello", "world"},
			want:  NewArrayType(StringType()).Type(),
		},
		{
			what:  "nested array",
			value: []any{[]any{[]any{[]any{"hi"}}}},
			want:  NewArrayType(NewArrayType(NewArrayType(NewArrayType(StringType()).Type()).Type()).Type()).Type(),
		},
		{
			what:  "merged array element",
			value: []any{"hello", 1.0, true},
			want:  NewArrayType(StringType()).Type(),
		},
		{
			what:  "recursively merged array element",
			value: []any{[]any{"hello"}, []any{1.0}, []any{true}},
			want:  NewArrayType(NewArrayType(StringType()).Type()).Type(),
		},
		{
			what:  "array element fallback to any",
			value: []any{"hello", nil},
			want:  NewArrayType(AnyType()).Type(),
		},
		{
			what:  "empty object",
			value: map[string]any{},
			want:  NewEmptyStrictObjectType().Type(),
		},
		{
			what:  "object",
			value: map[string]any{"hello": 1.0, "world": true},
			want: NewStrictObjectType(map[string]Type{
				"hello": NumberType(),
				"world": BoolType(),
			}).Type(),
		},
		{
			what: "nested object",
			value: map[string]any{
				"hello": []any{1.0},
				"world": map[string]any{
					"foo": true,
					"bar": "x",
				},
			},
			want: NewStrictObjectType(map[string]Type{
				"hello": NewArrayType(NumberType()).Type(),
				"world": NewStrictObjectType(map[string]Type{
					"foo": BoolType(),
					"bar": StringType(),
				}).Type(),
			}).Type(),
		},
	}

	for _, tc := range tests {
		t.Run(tc.what, func(t *testing.T) {
			have := TypeOfJSONValue(tc.value)
			if diff := cmp.Diff(tc.want, have); diff != "" {
				t.Fatal(diff)
			}
		})
	}
}

func TestExprTypePanicTypeOfJSONValue(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("error didn't occur")
		}
	}()
	TypeOfJSONValue(struct{}{})
}
