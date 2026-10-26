// Package mapping resolves workflow mapping insertions and maps their keys to source.
package mapping

import (
	"maps"
	"slices"
	"strings"
)

// Resolver adapts source strings without importing the public AST. The zero
// value of S represents an absent source; NewText preserves source locations.
type Resolver[S comparable] struct {
	Text          func(S) string
	HasExpression func(S) bool
	Evaluate      func(S) (any, bool)
	NewText       func(string, S) S
}

type Input[S comparable] struct {
	Name, Value S
	Evaluated   bool
}

type Inputs[S comparable] struct {
	Values     map[string]Input[S]
	Expression S
	Insertions []*Inputs[S]
}

// KeyState distinguishes runtime uncertainty from a statically invalid name.
type KeyState uint8

const (
	UnknownKey KeyState = iota
	KnownKey
	InvalidKey
)

// ResolvedKey associates a canonical name with its source declaration.
// Evaluated names are data and must never be passed back to expression parsing.
type ResolvedKey[S comparable] struct {
	Name      S
	Source    S
	State     KeyState
	Evaluated bool
}

type Entry[S comparable] struct {
	ResolvedKey[S]
	Value S
}

type Checks[S comparable] struct {
	Mapping func(S)
	String  func(S)
	Script  func(S)
}

func (r Resolver[S]) Check(inputs *Inputs[S], action string, checks Checks[S]) {
	var zero S
	r.Walk(inputs, checks.Mapping, func(entry Entry[S]) {
		CheckSourceKey(entry.Source, entry.Evaluated, checks.String)
		if entry.Evaluated {
			return
		}
		if strings.HasPrefix(action, "actions/github-script@") && (entry.Name == zero || strings.EqualFold(r.Text(entry.Name), "script")) {
			checks.Script(entry.Value)
		} else {
			checks.String(entry.Value)
		}
	})
}

// ResolveKey is the shared key contract for parsing and every semantic consumer.
func (r Resolver[S]) ResolveKey(source S, evaluated bool) ResolvedKey[S] {
	result := ResolvedKey[S]{Source: source, Evaluated: evaluated}
	if evaluated || !r.HasExpression(source) {
		result.Name, result.State = source, KnownKey
	} else {
		literal, known := r.Evaluate(source)
		if !known {
			return result
		}
		name, scalar := ScalarString(literal)
		if !scalar {
			result.State = InvalidKey
			return result
		}
		result.Name, result.State = r.NewText(name, source), KnownKey
	}
	if r.Text(result.Name) == "" {
		result.State = InvalidKey
	}
	return result
}

func (r Resolver[S]) Key(source S) (S, bool) {
	key := r.ResolveKey(source, false)
	var zero S
	return key.Name, key.State != UnknownKey && key.Name != zero
}

// Walk presents the same resolved names and source provenance to every consumer.
func (r Resolver[S]) Walk(mapping *Inputs[S], expression func(S), visit func(Entry[S])) bool {
	var zero S
	complete := true
	for _, key := range slices.Sorted(maps.Keys(mapping.Values)) {
		input := mapping.Values[key]
		name := input.Name
		if name == zero {
			name = r.NewText(key, zero)
		}
		resolved := r.ResolveKey(name, input.Evaluated)
		resolved.Source = input.Name
		complete = complete && resolved.State == KnownKey
		visit(Entry[S]{ResolvedKey: resolved, Value: input.Value})
	}
	if source := mapping.Expression; source != zero {
		if expression != nil {
			expression(source)
		}
		literal, known := r.Evaluate(source)
		object, isObject := literal.(map[string]any)
		if !known || !isObject {
			complete = false
		} else {
			for _, name := range slices.Sorted(maps.Keys(object)) {
				candidate := object[name]
				var value S
				if text, scalar := ScalarString(candidate); scalar {
					value = r.NewText(text, source)
				}
				key := r.ResolveKey(r.NewText(name, source), true)
				complete = complete && key.State == KnownKey
				visit(Entry[S]{ResolvedKey: key, Value: value})
			}
		}
	}
	for _, insertion := range mapping.Insertions {
		complete = r.Walk(insertion, expression, visit) && complete
	}
	return complete
}

func (r Resolver[S]) Value(mapping *Inputs[S], name string) (string, bool) {
	var zero S
	var value string
	found, valueKnown := false, true
	complete := r.Walk(mapping, nil, func(entry Entry[S]) {
		if entry.Name == zero || !strings.EqualFold(r.Text(entry.Name), name) {
			return
		}
		var text string
		known := false
		if entry.Value != zero {
			if entry.Evaluated || !r.HasExpression(entry.Value) {
				text, known = r.Text(entry.Value), true
			} else {
				literal, evaluated := r.Evaluate(entry.Value)
				var scalar bool
				text, scalar = ScalarString(literal)
				known = evaluated && scalar
			}
		}
		valueKnown = valueKnown && known && !found
		found, value = true, text
	})
	return value, complete && valueKnown
}

func (r Resolver[S]) Names(mapping *Inputs[S]) (map[string]Input[S], bool) {
	var zero S
	inputs := map[string]Input[S]{}
	complete := r.Walk(mapping, nil, func(entry Entry[S]) {
		if entry.Name != zero {
			inputs[strings.ToLower(r.Text(entry.Name))] = Input[S]{Name: entry.Name, Value: entry.Value}
		}
	})
	return inputs, complete
}

// CheckSourceKey validates original mapping keys, excluding keys produced as data
// by an object expression. Evaluated strings must not be interpreted a second time.
func CheckSourceKey[S comparable](source S, evaluated bool, check func(S)) {
	var zero S
	if !evaluated && source != zero {
		check(source)
	}
}
