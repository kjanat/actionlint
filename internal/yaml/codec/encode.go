package codec

import (
	"encoding"
	"errors"
	"reflect"
	"time"

	"actionlint.kjanat.dev/v2/internal/yaml/syntax"

	upstream "github.com/kjanat/go-yaml/v4"
)

func encodingValue(value any) (any, error) {
	var node *syntax.Node
	switch v := value.(type) {
	case syntax.Node:
		node = &v
	case *syntax.Node:
		if v == nil {
			return nil, nil
		}
		node = v
	}
	if node != nil {
		if err := validateEncodingGraph(node); err != nil {
			return nil, err
		}
		graph, err := toParser(node)
		return graph.root, err
	}
	if err := checkEncoding(reflect.ValueOf(value), map[encodingVisit]bool{}); err != nil {
		return nil, err
	}
	return value, nil
}

type encodingVisit struct {
	typeOf  reflect.Type
	pointer uintptr
	length  int
}

// Detect cycles in reflected fields before invoking custom marshalers.
func checkEncoding(v reflect.Value, active map[encodingVisit]bool) error {
	if !v.IsValid() || v.Kind() == reflect.Pointer && v.IsNil() {
		return nil
	}
	if v.CanInterface() {
		switch v.Interface().(type) {
		case upstream.Node, *upstream.Node, time.Time, *time.Time, time.Duration, upstream.Marshaler, encoding.TextMarshaler:
			return nil
		}
	}
	switch v.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice:
		if v.IsNil() {
			return nil
		}
		visit := encodingVisit{typeOf: v.Type(), pointer: v.Pointer()}
		if v.Kind() == reflect.Slice {
			visit.length = v.Len()
		}
		if active[visit] {
			return errors.New("cannot encode cyclic Go values as YAML")
		}
		active[visit] = true
		defer delete(active, visit)
	}
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if v.IsNil() {
			return nil
		}
		return checkEncoding(v.Elem(), active)
	case reflect.Struct:
		for i := range v.NumField() {
			field := v.Type().Field(i)
			if !field.IsExported() && !field.Anonymous || field.Tag.Get("yaml") == "-" {
				continue
			}
			if err := checkEncoding(v.Field(i), active); err != nil {
				return err
			}
		}
	case reflect.Array, reflect.Slice:
		for i := range v.Len() {
			if err := checkEncoding(v.Index(i), active); err != nil {
				return err
			}
		}
	case reflect.Map:
		iter := v.MapRange()
		for iter.Next() {
			if err := checkEncoding(iter.Key(), active); err != nil {
				return err
			}
			if err := checkEncoding(iter.Value(), active); err != nil {
				return err
			}
		}
	}
	return nil
}
