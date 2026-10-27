package codec

import (
	"errors"
	"reflect"

	upstream "go.yaml.in/yaml/v4"
)

// Selected marks a struct field for source projection. Its value carries no data.
type Selected struct{ _ byte }

func (*Selected) YAMLSelectionTarget() {}

// Select visits selected struct fields synchronously in constructor order.
// Only selected values are exposed; native parser nodes remain inside the session.
func (c Cursor) Select(target any, visit func(*Selected, Cursor) error) error {
	return c.selectFields(target, visit, c.claim)
}

// Select reuses the entered root's count once, like Decode.
func (e Entered) Select(target any, visit func(*Selected, Cursor) error) error {
	return e.cursor.selectFields(target, visit, e.claim)
}

func (c Cursor) selectFields(target any, visit func(*Selected, Cursor) error, claim *rootClaim) error {
	if err := c.check(); err != nil {
		return err
	}
	v := reflect.ValueOf(target)
	if v.Kind() != reflect.Pointer || v.IsNil() {
		return errors.New("YAML target must be a non-nil pointer")
	}
	if v.Elem().Kind() != reflect.Struct || v.Elem().Type() == reflect.TypeFor[Selected]() {
		return errors.New("YAML Select target must be a struct with Selected fields")
	}
	if err := checkDecodeType(v.Elem().Type(), true, map[decodeShape]bool{}); err != nil {
		return err
	}
	if visit == nil {
		return errors.New("YAML Select requires a visitor")
	}
	var schemaErrors []error
	handler := func(dst upstream.OccurrenceTarget, occurrence upstream.Occurrence) error {
		if err := c.check(); err != nil {
			return err
		}
		field, ok := dst.(*Selected)
		if !ok {
			return errors.New("YAML selection target must use codec.Selected")
		}
		node, ok := c.session.originals[occurrence.Node]
		if !ok {
			return errors.New("selected YAML node belongs to another source graph")
		}
		value := Cursor{session: c.session, node: node, via: c.via, claim: &rootClaim{}}
		for _, alias := range occurrence.Crossed {
			node, ok := c.session.originals[alias]
			if !ok {
				return errors.New("selected YAML alias belongs to another source graph")
			}
			value.via = &aliasCrossing{node: node, parent: value.via}
		}
		if err := visit(field, value); err != nil {
			schemaErrors = append(schemaErrors, c.session.Wrap(node, err))
		}
		if c.session.fatal != nil {
			return c.session.fatal
		}
		return c.check()
	}
	err := c.load(target, claim, upstream.WithOccurrenceFunc(handler))
	if c.session.fatal != nil {
		return c.session.fatal
	}
	return joinDecodeErrors(append(schemaErrors, err)...)
}
