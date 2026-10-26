package codec

import (
	"fmt"
	"maps"
	"reflect"

	"actionlint.kjanat.dev/v2/internal/yaml/syntax"
)

// seed preserves values for fields absent from the source before the parser
// updates the shadow. Hook captures carry their original value until restore.
func (d *Decoder) seed(dst, src reflect.Value) error {
	g := d.graph
	if src.Type() == reflect.PointerTo(nodeType) && dst.Type() == reflect.PointerTo(captureType) {
		if !src.IsNil() {
			dst.Set(reflect.ValueOf(&captured{prior: src}))
		}
		return nil
	}
	if dst.Type() == src.Type() && src.CanInterface() {
		dst.Set(src)
		return nil
	}
	if dst.Type() == captureType {
		dst.Set(reflect.ValueOf(captured{prior: src}))
		return nil
	}
	if src.Type() == nodeType {
		node, ok := reflect.TypeAssert[syntax.Node](src)
		if !ok {
			return fmt.Errorf("invalid YAML node value %v", src.Type())
		}
		graph, err := toParser(&node)
		if err != nil {
			return err
		}
		maps.Copy(g.originals, graph.originals)
		dst.Set(reflect.ValueOf(*graph.root))
		return nil
	}
	switch src.Kind() {
	case reflect.Pointer:
		if src.IsNil() {
			return nil
		}
		dst.Set(reflect.New(dst.Type().Elem()))
		d.seeded[shadowID(dst)] = src
		return d.seed(dst.Elem(), src.Elem())
	case reflect.Struct:
		if dst.NumField() == src.NumField()+1 && dst.Field(src.NumField()).Type() == seededValueType {
			dst.Field(src.NumField()).Set(reflect.ValueOf(&seededValue{original: src}))
		}
		for i := range src.NumField() {
			if dst.Field(i).CanSet() && (src.Type().Field(i).IsExported() || src.Type().Field(i).Anonymous) {
				if err := d.seed(dst.Field(i), src.Field(i)); err != nil {
					return err
				}
			}
		}
		return nil
	case reflect.Slice, reflect.Array:
		if src.Kind() == reflect.Slice {
			if src.IsNil() {
				return nil
			}
			dst.Set(reflect.MakeSlice(dst.Type(), src.Len(), max(1, src.Len())))
			d.seeded[shadowID(dst)] = src
		}
		for i := range src.Len() {
			if err := d.seed(dst.Index(i), src.Index(i)); err != nil {
				return err
			}
		}
		return nil
	case reflect.Map:
		if src.IsNil() {
			return nil
		}
		dst.Set(reflect.MakeMapWithSize(dst.Type(), src.Len()))
		d.seeded[shadowID(dst)] = src
		iter := src.MapRange()
		for iter.Next() {
			value := reflect.New(dst.Type().Elem()).Elem()
			if err := d.seed(value, iter.Value()); err != nil {
				return err
			}
			dst.SetMapIndex(iter.Key(), value)
		}
		return nil
	}
	return fmt.Errorf("cannot seed YAML shadow %v from %v", dst.Type(), src.Type())
}
