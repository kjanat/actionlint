package codec

import (
	"cmp"
	"encoding"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"actionlint.kjanat.dev/v2/internal/yaml/syntax"

	upstream "go.yaml.in/yaml/v4"
)

// captured is the only parser hook. Shadow structs replace owned-tree hooks with
// captures, then decode them against the owned graph.
type captured struct {
	node  *upstream.Node
	prior reflect.Value
}

type seededValue struct{ original reflect.Value }

func (c *captured) UnmarshalYAML(n *upstream.Node) error {
	c.node = n
	return nil
}

var (
	nodeType        = reflect.TypeFor[syntax.Node]()
	captureType     = reflect.TypeFor[captured]()
	hookType        = reflect.TypeFor[Unmarshaler]()
	parserHookType  = reflect.TypeFor[upstream.Unmarshaler]()
	textHookType    = reflect.TypeFor[encoding.TextUnmarshaler]()
	legacyHookType  = reflect.TypeFor[interface{ UnmarshalYAML(func(any) error) error }]()
	seededValueType = reflect.TypeFor[*seededValue]()
)

func hasParserDecoder(t reflect.Type) bool {
	for _, hook := range []reflect.Type{parserHookType, textHookType, legacyHookType} {
		if t.Implements(hook) || t.Kind() != reflect.Pointer && reflect.PointerTo(t).Implements(hook) {
			return true
		}
	}
	return false
}

func needsShadow(t reflect.Type, seen map[reflect.Type]bool) bool {
	if t == nodeType || t == reflect.PointerTo(nodeType) || t.Implements(hookType) || t.Kind() != reflect.Pointer && reflect.PointerTo(t).Implements(hookType) {
		return true
	}
	if seen[t] || hasParserDecoder(t) {
		return false
	}
	seen[t] = true
	switch t.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Array, reflect.Map:
		return needsShadow(t.Elem(), seen)
	case reflect.Struct:
		for field := range t.Fields() {
			if (field.IsExported() || field.Anonymous) && field.Tag.Get("yaml") != "-" && needsShadow(field.Type, seen) {
				return true
			}
		}
	}
	return false
}

func shadowStruct(fields []reflect.StructField) (shape reflect.Type, err error) {
	defer func() {
		if failure := recover(); failure != nil {
			err = fmt.Errorf("cannot construct YAML decoding shape: %v", failure)
		}
	}()
	return reflect.StructOf(fields), nil
}

func shadowType(t reflect.Type, active map[reflect.Type]bool, names map[string]string) (shape reflect.Type, err error) {
	defer func() {
		if shape == nil || shape == t || shape == captureType || shape == reflect.PointerTo(captureType) || shape == reflect.TypeFor[upstream.Node]() {
			return
		}
		name := t.String()
		if previous, exists := names[shape.String()]; exists && previous != name {
			name = t.Kind().String()
		}
		names[shape.String()] = name
	}()
	if t == nodeType {
		return reflect.TypeFor[upstream.Node](), nil
	}
	if t == reflect.PointerTo(nodeType) {
		return reflect.PointerTo(captureType), nil
	}
	if t.Kind() != reflect.Pointer && reflect.PointerTo(t).Implements(hookType) {
		return captureType, nil
	}
	if !needsShadow(t, map[reflect.Type]bool{}) {
		return t, nil
	}
	if active[t] {
		return nil, fmt.Errorf("recursive decoding type %v requires an explicit YAML decoder", t)
	}
	active[t] = true
	defer delete(active, t)
	switch t.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Array, reflect.Map:
		e, err := shadowType(t.Elem(), active, names)
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
			fields[i].Anonymous = false
			if !f.IsExported() && !f.Anonymous || f.Tag.Get("yaml") == "-" {
				continue
			}
			if !f.IsExported() {
				name := "CodecEmbedded" + strconv.Itoa(i)
				for {
					if _, exists := t.FieldByName(name); !exists {
						break
					}
					name += "X"
				}
				fields[i].Name, fields[i].PkgPath = name, ""
				tag := f.Tag.Get("yaml")
				if tag == "" || strings.HasPrefix(tag, ",") {
					tag = strings.ToLower(f.Name) + tag
				}
				fields[i].Tag = reflect.StructTag("yaml:" + strconv.Quote(tag))
			}
			s, err := shadowType(f.Type, active, names)
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
			name := "CodecOriginal"
			for {
				if _, exists := t.FieldByName(name); !exists {
					break
				}
				name += "X"
			}
			fields = append(fields, reflect.StructField{Name: name, Type: seededValueType, Tag: `yaml:"-"`})
			return shadowStruct(fields)
		}
	}
	return t, nil
}

func (d *Decoder) restore(dst, src reflect.Value) error {
	g := d.graph
	if dst.Type() == reflect.PointerTo(nodeType) && src.Type() == reflect.PointerTo(captureType) {
		if src.IsNil() {
			dst.SetZero()
			return nil
		}
		capture, ok := reflect.TypeAssert[*captured](src)
		if !ok {
			return errors.New("invalid YAML node capture")
		}
		if capture.node == nil {
			if capture.prior.IsValid() {
				dst.Set(capture.prior)
			}
			return nil
		}
		n, ok := g.originals[capture.node]
		if !ok {
			return errors.New("captured YAML node is outside its source graph")
		}
		dst.Set(reflect.ValueOf(n))
		return nil
	}
	if dst.Type() == nodeType && src.Type() == reflect.TypeFor[upstream.Node]() {
		raw, ok := reflect.TypeAssert[upstream.Node](src)
		if !ok {
			return errors.New("invalid parser node capture")
		}
		owned, err := fromParser(&raw, g.originals)
		if err != nil {
			return err
		}
		dst.Set(reflect.ValueOf(*owned))
		return nil
	}
	if dst.Type() == src.Type() && dst.CanSet() {
		dst.Set(src)
		return nil
	}
	if src.Type() == captureType {
		capture, ok := reflect.TypeAssert[captured](src)
		if !ok {
			return errors.New("invalid decoder capture")
		}
		if capture.prior.IsValid() {
			dst.Set(capture.prior)
		}
		if capture.node == nil {
			return nil
		}
		n, ok := g.originals[capture.node]
		if !ok {
			return errors.New("decoder hook received a node outside its source graph")
		}
		if dst.CanAddr() {
			if hook, ok := reflect.TypeAssert[Unmarshaler](dst.Addr()); ok {
				return d.decodeHook(hook, n)
			}
		}
		return fmt.Errorf("no owned YAML decoder for %v", dst.Type())
	}
	switch dst.Kind() {
	case reflect.Pointer:
		if src.IsNil() {
			dst.SetZero()
			return nil
		}
		if original, seeded := d.seeded[shadowID(src)]; seeded {
			dst.Set(original)
		} else if dst.IsNil() {
			dst.Set(reflect.New(dst.Type().Elem()))
		}
		return d.restore(dst.Elem(), src.Elem())
	case reflect.Struct:
		if src.NumField() == dst.NumField()+1 && src.Field(dst.NumField()).Type() == seededValueType {
			prior, ok := reflect.TypeAssert[*seededValue](src.Field(dst.NumField()))
			if ok && prior != nil && dst.CanSet() {
				dst.Set(prior.original)
			}
		}
		var errs []error
		for i := range dst.NumField() {
			if dst.Field(i).CanSet() || dst.Type().Field(i).Anonymous && dst.Field(i).Kind() == reflect.Struct {
				if err := d.restore(dst.Field(i), src.Field(i)); err != nil {
					errs = append(errs, err)
				}
			}
		}
		return joinDecodeErrors(errs...)
	case reflect.Slice, reflect.Array:
		if dst.Kind() == reflect.Slice {
			if src.IsNil() {
				dst.SetZero()
				return nil
			}
			if original, seeded := d.seeded[shadowID(src)]; seeded {
				dst.Set(original)
			} else {
				dst.Set(reflect.MakeSlice(dst.Type(), src.Len(), src.Len()))
			}
		}
		var errs []error
		for i := range src.Len() {
			if err := d.restore(dst.Index(i), src.Index(i)); err != nil {
				errs = append(errs, err)
			}
		}
		return joinDecodeErrors(errs...)
	case reflect.Map:
		if src.IsNil() {
			dst.SetZero()
			return nil
		}
		if original, seeded := d.seeded[shadowID(src)]; seeded {
			dst.Set(original)
		} else if dst.IsNil() {
			dst.Set(reflect.MakeMapWithSize(dst.Type(), src.Len()))
		}
		var errs []error
		iter := src.MapRange()
		for iter.Next() {
			v := reflect.New(dst.Type().Elem()).Elem()
			if err := d.restore(v, iter.Value()); err != nil {
				errs = append(errs, err)
			}
			dst.SetMapIndex(iter.Key(), v)
		}
		return joinDecodeErrors(errs...)
	}
	return fmt.Errorf("cannot restore YAML %v as %v", src.Type(), dst.Type())
}

func Decode(n *syntax.Node, target any) error {
	graph, err := toParser(n)
	if err != nil {
		return err
	}
	d := newDecoder(graph)
	defer func() { d.closed = true }()
	return d.Decode(n, target)
}

func (d *Decoder) Decode(n *syntax.Node, target any) error {
	if n == nil {
		return errors.New("YAML source node must not be nil")
	}
	position := Mark{Line: n.Line, Column: n.Column}
	if d == nil || d.nodes == nil || d.closed {
		return &GraphError{Mark: position, Reason: "decoder is not active"}
	}
	if d.fatal != nil {
		return d.fatal
	}
	raw, member := d.nodes[n]
	if !member {
		return &GraphError{Mark: position, Reason: "node belongs to another source graph"}
	}
	v := reflect.ValueOf(target)
	if v.Kind() != reflect.Pointer || v.IsNil() {
		return errors.New("YAML target must be a non-nil pointer")
	}
	if p, ok := target.(*syntax.Node); ok {
		*p = *n
		return nil
	}
	current := n
	if current.Kind == syntax.DocumentNode && len(current.Content) > 0 {
		current = current.Content[0]
	}
	if hook, ok := target.(Unmarshaler); ok {
		if current.Kind == syntax.DocumentNode && len(current.Content) == 0 {
			return nil
		}
		seen := map[*syntax.Node]bool{}
		for current.Kind == syntax.AliasNode {
			if seen[current] || current.Alias == nil {
				return &LoadError{Message: "invalid YAML alias cycle or missing target", Mark: Mark{Line: current.Line, Column: current.Column}}
			}
			seen[current] = true
			current = current.Alias
		}
		if current.Kind == syntax.ScalarNode && current.Tag == "!!null" {
			return nil
		}
		return d.decodeHook(hook, current)
	}
	names := map[string]string{}
	shape, err := shadowType(v.Elem().Type(), map[reflect.Type]bool{}, names)
	if err != nil {
		return err
	}
	if shape == v.Elem().Type() {
		err := translate(d.load(raw, target))
		if fatal, ok := err.(*LoadError); ok {
			d.fatal = fatal
		}
		return err
	}
	shadow := reflect.New(shape)
	if err := d.seed(shadow.Elem(), v.Elem()); err != nil {
		return err
	}
	parseErr := translate(d.load(raw, shadow.Interface()))
	restoreTypeNames(parseErr, names)
	if fatal, ok := parseErr.(*LoadError); ok {
		d.fatal = fatal
		return fatal
	}
	restoreErr := d.restore(v.Elem(), shadow.Elem())
	if d.fatal != nil {
		return d.fatal
	}
	return joinDecodeErrors(parseErr, restoreErr)
}

func restoreTypeNames(err error, names map[string]string) {
	keys := make([]string, 0, len(names))
	for shape := range names {
		keys = append(keys, shape)
	}
	slices.SortFunc(keys, func(a, b string) int { return cmp.Compare(len(b), len(a)) })
	pairs := make([]string, 0, len(keys)*2)
	for _, shape := range keys {
		pairs = append(pairs, shape, names[shape])
	}
	replacer := strings.NewReplacer(pairs...)
	replace := func(item *LoadError) {
		item.Message = replacer.Replace(item.Message)
		item.text = replacer.Replace(item.text)
	}
	switch err := err.(type) {
	case *DecodeError:
		for _, item := range err.Errors {
			replace(item)
		}
	case *LoadError:
		replace(err)
	}
}

func (d *Decoder) decodeHook(hook Unmarshaler, n *syntax.Node) error {
	if d.fatal != nil {
		return d.fatal
	}
	visit := hookVisit{typeOf: reflect.TypeOf(hook), node: n}
	if d.active[visit] {
		d.fatal = &LoadError{Message: "recursive alias through YAML decoder hook", Mark: Mark{Line: n.Line, Column: n.Column}}
		return d.fatal
	}
	d.active[visit] = true
	defer delete(d.active, visit)
	err := hook.UnmarshalYAML(d, n)
	if d.fatal != nil {
		return d.fatal
	}
	if err == nil {
		return nil
	}
	if _, ok := errors.AsType[*DecodeError](err); ok {
		return joinDecodeErrors(err)
	}
	if _, ok := errors.AsType[*LoadError](err); ok {
		return joinDecodeErrors(err)
	}
	return &DecodeError{Errors: []*LoadError{{Message: err.Error(), Mark: Mark{Line: n.Line, Column: n.Column}, cause: err}}}
}

func joinDecodeErrors(errs ...error) error {
	var result DecodeError
	var collect func(error)
	collect = func(err error) {
		if err == nil {
			return
		}
		switch err := err.(type) {
		case *DecodeError:
			result.Errors = append(result.Errors, err.Errors...)
		case *LoadError:
			result.Errors = append(result.Errors, err)
		case interface{ Unwrap() []error }:
			for _, child := range err.Unwrap() {
				collect(child)
			}
		default:
			if inner := errors.Unwrap(err); inner != nil {
				_, decode := errors.AsType[*DecodeError](inner)
				_, load := errors.AsType[*LoadError](inner)
				if decode || load {
					collect(inner)
					return
				}
			}
			result.Errors = append(result.Errors, &LoadError{Message: err.Error(), cause: err})
		}
	}
	for _, err := range errs {
		collect(err)
	}
	if len(result.Errors) == 0 {
		return nil
	}
	slices.SortStableFunc(result.Errors, func(a, b *LoadError) int {
		if order := cmp.Compare(a.Mark.Line, b.Mark.Line); order != 0 {
			return order
		}
		if order := cmp.Compare(a.Mark.Column, b.Mark.Column); order != 0 {
			return order
		}
		return cmp.Compare(a.Message, b.Message)
	})
	return &result
}
