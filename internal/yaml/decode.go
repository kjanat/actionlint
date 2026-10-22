package yaml

import (
	"errors"
	"fmt"
	"reflect"

	upstream "go.yaml.in/yaml/v4"
)

// captured is the only parser hook. Shadow structs replace owned-tree hooks with
// captures, then decode them against the owned graph. Ordinary scalars and structs
// retain the parser's conversion, duplicate-key and merge-key behavior.
type captured struct{ node *Node }

func (c *captured) UnmarshalYAML(n *upstream.Node) error { c.node = fromParser(n); return nil }

var (
	nodeType    = reflect.TypeFor[Node]()
	captureType = reflect.TypeFor[captured]()
	hookType    = reflect.TypeFor[Unmarshaler]()
)

func shadowType(t reflect.Type, active map[reflect.Type]bool) (reflect.Type, error) {
	if t == nodeType {
		return reflect.TypeFor[upstream.Node](), nil
	}
	if t.Kind() != reflect.Pointer && reflect.PointerTo(t).Implements(hookType) {
		return captureType, nil
	}
	if active[t] {
		return nil, fmt.Errorf("recursive decoding type %v requires an explicit YAML decoder", t)
	}
	active[t] = true
	defer delete(active, t)
	switch t.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Array, reflect.Map:
		e, err := shadowType(t.Elem(), active)
		if err != nil {
			return nil, err
		}
		if e == t.Elem() {
			return t, nil
		}
		switch t.Kind() {
		case reflect.Pointer:
			return reflect.PointerTo(e), nil
		case reflect.Slice:
			return reflect.SliceOf(e), nil
		case reflect.Array:
			return reflect.ArrayOf(t.Len(), e), nil
		case reflect.Map:
			return reflect.MapOf(t.Key(), e), nil
		}
	case reflect.Struct:
		fields := make([]reflect.StructField, t.NumField())
		changed := false
		for i := range fields {
			f := t.Field(i)
			fields[i] = f
			if !f.IsExported() || f.Tag.Get("yaml") == "-" {
				continue
			}
			s, err := shadowType(f.Type, active)
			if err != nil {
				return nil, err
			}
			if s != f.Type {
				changed = true
				fields[i].Type = s
				fields[i].Anonymous = false
			}
		}
		if changed {
			return reflect.StructOf(fields), nil
		}
	}
	return t, nil
}

func restore(dst, src reflect.Value) error {
	if dst.Type() == nodeType && src.Type() == reflect.TypeFor[upstream.Node]() {
		raw, ok := reflect.TypeAssert[upstream.Node](src)
		if !ok {
			return errors.New("invalid parser node capture")
		}
		dst.Set(reflect.ValueOf(*fromParser(&raw)))
		return nil
	}
	if dst.Type() == src.Type() {
		dst.Set(src)
		return nil
	}
	if src.Type() == captureType {
		capture, ok := reflect.TypeAssert[captured](src)
		if !ok {
			return errors.New("invalid decoder capture")
		}
		n := capture.node
		if n == nil {
			return nil
		}
		if dst.Type() == nodeType {
			dst.Set(reflect.ValueOf(*n))
			return nil
		}
		if dst.CanAddr() {
			if hook, ok := reflect.TypeAssert[Unmarshaler](dst.Addr()); ok {
				return decodeHook(hook, n)
			}
		}
		return fmt.Errorf("no owned YAML decoder for %v", dst.Type())
	}
	switch dst.Kind() {
	case reflect.Pointer:
		if src.IsNil() {
			return nil
		}
		dst.Set(reflect.New(dst.Type().Elem()))
		return restore(dst.Elem(), src.Elem())
	case reflect.Struct:
		var errs []error
		for i := range dst.NumField() {
			if dst.Field(i).CanSet() {
				if err := restore(dst.Field(i), src.Field(i)); err != nil {
					errs = append(errs, err)
				}
			}
		}
		return joinDecodeErrors(errs...)
	case reflect.Slice, reflect.Array:
		if dst.Kind() == reflect.Slice {
			if src.IsNil() {
				return nil
			}
			dst.Set(reflect.MakeSlice(dst.Type(), src.Len(), src.Len()))
		}
		for i := range src.Len() {
			if err := restore(dst.Index(i), src.Index(i)); err != nil {
				return err
			}
		}
		return nil
	case reflect.Map:
		if src.IsNil() {
			return nil
		}
		dst.Set(reflect.MakeMapWithSize(dst.Type(), src.Len()))
		iter := src.MapRange()
		for iter.Next() {
			v := reflect.New(dst.Type().Elem()).Elem()
			if err := restore(v, iter.Value()); err != nil {
				return err
			}
			dst.SetMapIndex(iter.Key(), v)
		}
		return nil
	}
	return fmt.Errorf("cannot restore YAML %v as %v", src.Type(), dst.Type())
}

func (n *Node) Decode(target any) error {
	v := reflect.ValueOf(target)
	if v.Kind() != reflect.Pointer || v.IsNil() {
		return errors.New("YAML target must be a non-nil pointer")
	}
	if p, ok := target.(*Node); ok {
		*p = *n
		return nil
	}
	current := n
	if current.Kind == DocumentNode && len(current.Content) > 0 {
		current = current.Content[0]
	}
	if hook, ok := target.(Unmarshaler); ok {
		if current.Kind == ScalarNode && current.Tag == "!!null" {
			return nil
		}
		return decodeHook(hook, current)
	}
	shape, err := shadowType(v.Elem().Type(), map[reflect.Type]bool{})
	if err != nil {
		return err
	}
	if shape == v.Elem().Type() {
		return translate(toParser(n).Decode(target))
	}
	shadow := reflect.New(shape)
	parseErr := translate(toParser(n).Decode(shadow.Interface()))
	return joinDecodeErrors(parseErr, restore(v.Elem(), shadow.Elem()))
}

func decodeHook(hook Unmarshaler, n *Node) error {
	err := hook.UnmarshalYAML(n)
	if err == nil {
		return nil
	}
	if _, ok := errors.AsType[*DecodeError](err); ok {
		return err
	}
	if _, ok := errors.AsType[*LoadError](err); ok {
		return err
	}
	return &DecodeError{Errors: []*LoadError{{Message: err.Error(), Mark: Mark{Line: n.Line, Column: n.Column}}}}
}

func joinDecodeErrors(errs ...error) error {
	var result DecodeError
	for _, err := range errs {
		if err == nil {
			continue
		}
		if list, ok := errors.AsType[*DecodeError](err); ok {
			result.Errors = append(result.Errors, list.Errors...)
		} else if e, ok := errors.AsType[*LoadError](err); ok {
			result.Errors = append(result.Errors, e)
		} else {
			return err
		}
	}
	if len(result.Errors) == 0 {
		return nil
	}
	return &result
}
