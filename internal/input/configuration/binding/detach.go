package binding

import (
	"errors"
	"fmt"
	"reflect"
)

// DetachValue returns a deep copy without changing a dynamic value's type.
func DetachValue(value any) (any, error) {
	if value == nil {
		return nil, nil
	}
	detached, err := detachDynamic(reflect.ValueOf(value), map[dynamicVisit]bool{}, false)
	if err != nil {
		return nil, err
	}
	return detached.Interface(), nil
}

type dynamicVisit struct {
	kind    reflect.Kind
	pointer uintptr
}

// detachDynamic preserves concrete values reachable through an interface. It
// rejects cyclic or opaque state instead of silently dropping it.
func detachDynamic(value reflect.Value, active map[dynamicVisit]bool, hooks bool) (reflect.Value, error) {
	if !value.IsValid() {
		return value, nil
	}
	if hooks && value.Kind() != reflect.Pointer && value.Kind() != reflect.Interface && value.CanInterface() {
		owner, ok := reflect.TypeAssert[OwnedCopier](value)
		if !ok {
			addressable := reflect.New(value.Type())
			addressable.Elem().Set(value)
			owner, ok = reflect.TypeAssert[OwnedCopier](addressable)
		}
		if ok {
			copied, err := owner.CopyConfig()
			if err != nil {
				return reflect.Value{}, err
			}
			result := reflect.ValueOf(copied)
			if !result.IsValid() || result.Type() != value.Type() {
				return reflect.Value{}, fmt.Errorf("invalid configuration copy of %s", value.Type())
			}
			return result, nil
		}
	}

	switch value.Kind() {
	case reflect.Interface:
		if value.IsNil() {
			return reflect.Zero(value.Type()), nil
		}
		child, err := detachDynamic(value.Elem(), active, hooks)
		if err != nil {
			return reflect.Value{}, err
		}
		out := reflect.New(value.Type()).Elem()
		out.Set(child)
		return out, nil
	case reflect.Pointer, reflect.Map, reflect.Slice:
		if value.IsNil() {
			return reflect.Zero(value.Type()), nil
		}
		key := dynamicVisit{value.Kind(), value.Pointer()}
		if active[key] {
			return reflect.Value{}, fmt.Errorf("cyclic configuration value %s", value.Type())
		}
		active[key] = true
		defer delete(active, key)
		switch value.Kind() {
		case reflect.Pointer:
			child, err := detachDynamic(value.Elem(), active, hooks)
			if err != nil {
				return reflect.Value{}, err
			}
			out := reflect.New(value.Type().Elem())
			out.Elem().Set(child)
			return out, nil
		case reflect.Slice:
			out := reflect.MakeSlice(value.Type(), value.Len(), value.Len())
			for i := range value.Len() {
				child, err := detachDynamic(value.Index(i), active, hooks)
				if err != nil {
					return reflect.Value{}, err
				}
				out.Index(i).Set(child)
			}
			return out, nil
		case reflect.Map:
			out := reflect.MakeMapWithSize(value.Type(), value.Len())
			iter := value.MapRange()
			for iter.Next() {
				key := iter.Key()
				if key.Kind() != reflect.String && !isScalar(key.Kind()) {
					return reflect.Value{}, fmt.Errorf("unsupported configuration map key %s", key.Type())
				}
				child, err := detachDynamic(iter.Value(), active, hooks)
				if err != nil {
					return reflect.Value{}, err
				}
				out.SetMapIndex(key, child)
			}
			return out, nil
		default:
			return reflect.Value{}, fmt.Errorf("unsupported configuration value %s", value.Type())
		}
	case reflect.Array:
		out := reflect.New(value.Type()).Elem()
		for i := range value.Len() {
			child, err := detachDynamic(value.Index(i), active, hooks)
			if err != nil {
				return reflect.Value{}, err
			}
			out.Index(i).Set(child)
		}
		return out, nil
	case reflect.Struct:
		out := reflect.New(value.Type()).Elem()
		out.Set(value)
		for i := range value.NumField() {
			field := value.Type().Field(i)
			if !field.IsExported() {
				if !hooks || !immutableType(field.Type) {
					return reflect.Value{}, fmt.Errorf("unsupported opaque configuration value %s", value.Type())
				}
				continue
			}
			child, err := detachDynamic(value.Field(i), active, hooks)
			if err != nil {
				return reflect.Value{}, err
			}
			out.Field(i).Set(child)
		}
		return out, nil
	default:
		if isScalar(value.Kind()) {
			return value, nil
		}
	}
	return reflect.Value{}, fmt.Errorf("unsupported configuration value %s", value.Type())
}

func isScalar(kind reflect.Kind) bool {
	switch kind {
	case reflect.Bool, reflect.String, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Float32, reflect.Float64:
		return true
	default:
		return false
	}
}

// DetachSnapshot copies a typed configuration without parsing or validating it.
func DetachSnapshot[T any](value T) (T, error) {
	var zero T
	v := reflect.ValueOf(value)
	if !v.IsValid() {
		return zero, nil
	}
	out, err := detachDynamic(v, map[dynamicVisit]bool{}, true)
	if err != nil {
		return zero, err
	}
	copied, ok := reflect.TypeAssert[T](out)
	if !ok {
		return zero, fmt.Errorf("invalid configuration copy of %T", value)
	}
	return copied, nil
}

// DetachPublicFields is for owners that restore their private state separately.
func DetachPublicFields[T any](value T) (T, error) {
	var zero T
	v := reflect.ValueOf(value)
	if v.Kind() != reflect.Struct {
		return zero, errors.New("configuration owner must be a struct")
	}
	out := reflect.New(v.Type()).Elem()
	out.Set(v)
	for i := range v.NumField() {
		if !v.Type().Field(i).IsExported() {
			continue
		}
		child, err := detachDynamic(v.Field(i), map[dynamicVisit]bool{}, true)
		if err != nil {
			return zero, err
		}
		out.Field(i).Set(child)
	}
	copied, ok := reflect.TypeAssert[T](out)
	if !ok {
		return zero, fmt.Errorf("invalid configuration copy of %T", value)
	}
	return copied, nil
}

func immutableType(t reflect.Type) bool {
	if isScalar(t.Kind()) {
		return true
	}
	switch t.Kind() {
	case reflect.Array:
		return immutableType(t.Elem())
	case reflect.Struct:
		for field := range t.Fields() {
			if !immutableType(field.Type) {
				return false
			}
		}
		return true
	default:
		return false
	}
}
