// Package value owns immutable workflow semantic values without depending on
// workflow syntax trees.
package value

import (
	"errors"
	"fmt"
	"iter"
	"math"
	"sort"
	"strconv"
	"strings"

	"actionlint.kjanat.dev/v2/diagnostic"
	exprlang "actionlint.kjanat.dev/v2/internal/expression"
	"actionlint.kjanat.dev/v2/internal/yaml/schema"
)

type State uint8

const (
	Missing State = iota
	Literal
	Evaluated
	Dynamic
	Invalid
)

type Kind uint8

const (
	KindUnknown Kind = iota
	Null
	Bool
	Number
	String
	Array
	Object
)

type Span struct {
	Start diagnostic.Position
	End   diagnostic.Position
}

type Origin struct {
	Span           Span
	FromExpression bool
}

type Prop struct {
	Name  string
	Value Value
}

type Value struct{ p *node }

type node struct {
	state            State
	kind             Kind
	origin           Origin
	boolean          bool
	number           float64
	text             string
	spelling         string
	items            []Value
	props            []Prop
	source           string
	err              error
	templateFragment bool
}

func makeValue(state State, kind Kind, origin Origin) Value {
	return Value{p: &node{state: state, kind: kind, origin: origin}}
}

func (v Value) State() State {
	if v.p == nil {
		return Missing
	}
	return v.p.state
}

func (v Value) Kind() Kind {
	if v.p == nil {
		return KindUnknown
	}
	if v.Known() || (v.p.state == Dynamic && v.p.templateFragment) {
		return v.p.kind
	}
	return KindUnknown
}

func (v Value) Known() bool {
	return v.p != nil && (v.p.state == Literal || v.p.state == Evaluated)
}

func (v Value) Bool() (bool, bool) {
	if !v.Known() || v.p.kind != Bool {
		return false, false
	}
	return v.p.boolean, true
}

func (v Value) Number() (float64, bool) {
	if !v.Known() || v.p.kind != Number {
		return 0, false
	}
	return v.p.number, true
}

func (v Value) Text() (string, bool) {
	if !v.Known() || v.p.kind != String {
		return "", false
	}
	return v.p.text, true
}

func (v Value) ScalarString() (string, bool) {
	if !v.Known() {
		return "", false
	}
	switch v.p.kind {
	case Null:
		return "", true
	case Bool:
		return strconv.FormatBool(v.p.boolean), true
	case Number:
		return strconv.FormatFloat(v.p.number, 'g', -1, 64), true
	case String:
		return v.p.text, true
	default:
		return "", false
	}
}

// Spelling returns the source text a YAML literal scalar was written with,
// such as "TRUE", "~", or "+1.2e2". Diagnostics quote it so users see their
// own input rather than its normalized semantic value. Values not built from a
// YAML scalar (evaluated, dynamic, or composite values) have no spelling.
func (v Value) Spelling() (string, bool) {
	if v.p == nil || v.p.state != Literal {
		return "", false
	}
	switch v.p.kind {
	case Null, Bool, Number, String:
		return v.p.spelling, true
	default:
		return "", false
	}
}

func (v Value) Len() int {
	if !v.Known() {
		return 0
	}
	switch v.p.kind {
	case Array:
		return len(v.p.items)
	case Object:
		return len(v.p.props)
	default:
		return 0
	}
}

func (v Value) Index(index int) (Value, bool) {
	if !v.Known() || v.p.kind != Array || index < 0 || index >= len(v.p.items) {
		return Value{}, false
	}
	return v.p.items[index], true
}

func (v Value) Items() iter.Seq[Value] {
	return func(yield func(Value) bool) {
		if !v.Known() || v.p.kind != Array {
			return
		}
		for _, item := range v.p.items {
			if !yield(item) {
				return
			}
		}
	}
}

func (v Value) Prop(name string) (Value, bool) {
	if !v.Known() || v.p.kind != Object {
		return Value{}, false
	}
	for _, prop := range v.p.props {
		if prop.Name == name {
			return prop.Value, true
		}
	}
	for _, prop := range v.p.props {
		if strings.EqualFold(prop.Name, name) {
			return prop.Value, true
		}
	}
	return Value{}, false
}

func (v Value) Props() iter.Seq2[string, Value] {
	return func(yield func(string, Value) bool) {
		if !v.Known() || v.p.kind != Object {
			return
		}
		for _, prop := range v.p.props {
			if !yield(prop.Name, prop.Value) {
				return
			}
		}
	}
}

func (v Value) Source() string {
	if v.p == nil || (v.p.state != Dynamic && v.p.state != Invalid) {
		return ""
	}
	return v.p.source
}

func (v Value) Err() error {
	if v.p == nil {
		return nil
	}
	return v.p.err
}

func (v Value) Origin() Origin {
	if v.p == nil {
		return Origin{}
	}
	return v.p.origin
}

// Equal applies the workflow raw-YAML equality rule to known semantic values.
// Unknown values never compare equal, even to another unknown value.
func (v Value) Equal(other Value) bool {
	if !v.Known() || !other.Known() || v.p.kind != other.p.kind {
		return false
	}
	switch v.p.kind {
	case Null:
		return true
	case Bool:
		return v.p.boolean == other.p.boolean
	case Number:
		return v.p.number == other.p.number || math.IsNaN(v.p.number) && math.IsNaN(other.p.number)
	case String:
		return v.p.text == other.p.text
	case Array:
		if len(v.p.items) != len(other.p.items) {
			return false
		}
		for i := range v.p.items {
			if !v.p.items[i].Equal(other.p.items[i]) {
				return false
			}
		}
		return true
	case Object:
		if len(v.p.props) != len(other.p.props) {
			return false
		}
		for _, prop := range v.p.props {
			value, ok := other.Prop(prop.Name)
			if !ok || !prop.Value.Equal(value) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func YAMLScalar(text, tag string, origin Origin) Value {
	value := makeValue(Literal, String, origin)
	value.p.text = text
	value.p.spelling = text
	switch tag {
	case "!!bool":
		if schema.IsCoreSchemaBool(text) {
			value.p.kind = Bool
			value.p.boolean = strings.EqualFold(text, "true")
		}
	case "!!null":
		if schema.IsCoreSchemaNull(text) {
			value.p.kind = Null
			value.p.text = ""
		}
	case "!!int", "!!float":
		if !schema.IsCoreSchemaNumber(text) {
			break
		}
		value.p.kind = Number
		switch strings.ToLower(text) {
		case ".inf", "+.inf":
			value.p.number = math.Inf(1)
		case "-.inf":
			value.p.number = math.Inf(-1)
		case ".nan":
			value.p.number = math.NaN()
		default:
			if strings.HasPrefix(text, "0x") || strings.HasPrefix(text, "0o") {
				n, err := strconv.ParseUint(text, 0, 32)
				if err != nil {
					value.p.kind = String
					break
				}
				if n > math.MaxInt32 {
					value.p.number = float64(n) - (1 << 32)
				} else {
					value.p.number = float64(n)
				}
			} else if n, err := strconv.ParseFloat(text, 64); err == nil {
				value.p.number = n
			} else {
				value.p.kind = String
			}
		}
	}
	return value
}

// DynamicValue constructs a runtime-dependent semantic value. source is the
// original expression or template text when one exists; kind may be
// KindUnknown when the runtime result shape is not known.
func DynamicValue(source string, kind Kind, origin Origin) Value {
	result := makeValue(Dynamic, kind, origin)
	result.p.source = source
	return result
}

func Template(text string, origin Origin) Value {
	if !strings.Contains(text, "${{") {
		return YAMLScalar(text, "!!str", origin)
	}
	expressionOrigin := Origin{Span: origin.Span, FromExpression: true}
	expression := exprlang.ParseAssignedExpression(text)
	if expression == nil {
		_, parseErr, _ := exprlang.ParseConditionExpression(text)
		if parseErr != nil {
			return invalid(text, expressionOrigin, parseErr)
		}
		value := makeValue(Dynamic, String, expressionOrigin)
		value.p.source = text
		value.p.templateFragment = true
		return value
	}
	constant, known := exprlang.ConditionConstantValue(expression)
	if !known {
		value := makeValue(Dynamic, KindUnknown, expressionOrigin)
		value.p.source = text
		return value
	}
	value := FromJSON(constant, expressionOrigin)
	if value.State() == Invalid {
		return invalid(text, expressionOrigin, value.Err())
	}
	return value
}

func Condition(text string, origin Origin) Value {
	expressionOrigin := Origin{Span: origin.Span, FromExpression: true}
	expression, parseErr, _ := exprlang.ParseConditionExpression(text)
	if parseErr != nil {
		return invalid(text, expressionOrigin, parseErr)
	}
	if expression == nil {
		value := makeValue(Dynamic, KindUnknown, expressionOrigin)
		value.p.source = text
		return value
	}
	constant, known := exprlang.ConditionConstantValue(expression)
	if !known {
		value := makeValue(Dynamic, KindUnknown, expressionOrigin)
		value.p.source = text
		return value
	}
	value := FromJSON(constant, expressionOrigin)
	if value.State() == Invalid {
		return invalid(text, expressionOrigin, value.Err())
	}
	return value
}

func FromJSON(input any, origin Origin) Value {
	switch value := input.(type) {
	case nil:
		return makeValue(Evaluated, Null, origin)
	case bool:
		result := makeValue(Evaluated, Bool, origin)
		result.p.boolean = value
		return result
	case float64:
		result := makeValue(Evaluated, Number, origin)
		result.p.number = value
		return result
	case float32:
		result := makeValue(Evaluated, Number, origin)
		result.p.number = float64(value)
		return result
	case int:
		result := makeValue(Evaluated, Number, origin)
		result.p.number = float64(value)
		return result
	case int64:
		result := makeValue(Evaluated, Number, origin)
		result.p.number = float64(value)
		return result
	case string:
		result := makeValue(Evaluated, String, origin)
		result.p.text = value
		return result
	case []any:
		items := make([]Value, len(value))
		for i, item := range value {
			items[i] = FromJSON(item, origin)
			if items[i].State() == Invalid {
				return items[i]
			}
		}
		return sequence(Evaluated, origin, items...)
	case map[string]any:
		props := make([]Prop, 0, len(value))
		names := make([]string, 0, len(value))
		for name := range value {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			item := value[name]
			converted := FromJSON(item, origin)
			if converted.State() == Invalid {
				return converted
			}
			props = append(props, Prop{Name: name, Value: converted})
		}
		return object(Evaluated, origin, props...)
	default:
		return invalid("", origin, fmt.Errorf("unsupported JSON value type %T", input))
	}
}

func Seq(origin Origin, items ...Value) Value {
	return sequence(Literal, origin, items...)
}

func sequence(state State, origin Origin, items ...Value) Value {
	result := makeValue(state, Array, origin)
	result.p.items = append([]Value(nil), items...)
	return result
}

func Map(origin Origin, props ...Prop) Value {
	return object(Literal, origin, props...)
}

func object(state State, origin Origin, props ...Prop) Value {
	result := makeValue(state, Object, origin)
	result.p.props = append([]Prop(nil), props...)
	return result
}

func invalid(source string, origin Origin, err error) Value {
	if err == nil {
		err = errors.New("invalid semantic value")
	} else {
		err = errors.New(err.Error())
	}
	result := makeValue(Invalid, KindUnknown, origin)
	result.p.source = source
	result.p.err = err
	return result
}
