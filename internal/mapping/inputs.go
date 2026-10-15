// Package mapping resolves workflow mapping insertions while retaining source provenance.
package mapping

import "strings"

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
}

type Inputs[S comparable] struct {
	Values     map[string]Input[S]
	Expression S
	Insertions []*Inputs[S]
}

type Entry[S comparable] struct {
	Input[S]
	SourceName S
	Evaluated  bool
}

type Checks[S comparable] struct {
	Mapping func(S)
	String  func(S)
	Script  func(S)
}

func (r Resolver[S]) Check(inputs *Inputs[S], action string, checks Checks[S]) {
	var zero S
	r.Walk(inputs, checks.Mapping, func(entry Entry[S]) {
		checks.String(entry.SourceName)
		if entry.Evaluated {
			return
		}
		if strings.HasPrefix(action, "actions/github-script@") && entry.Name != zero && strings.EqualFold(r.Text(entry.Name), "script") {
			checks.Script(entry.Value)
		} else {
			checks.String(entry.Value)
		}
	})
}

func (r Resolver[S]) Key(key S) (S, bool) {
	if !r.HasExpression(key) {
		return key, true
	}
	literal, known := r.Evaluate(key)
	name, scalar := ScalarString(literal)
	if !known || !scalar {
		var zero S
		return zero, false
	}
	return r.NewText(name, key), true
}

// Walk presents the same resolved names and source provenance to every consumer.
func (r Resolver[S]) Walk(mapping *Inputs[S], expression func(S), visit func(Entry[S])) bool {
	var zero S
	complete := true
	for key, input := range mapping.Values {
		name := input.Name
		if name == zero {
			name = r.NewText(key, zero)
		}
		resolved, known := r.Key(name)
		complete = complete && known
		visit(Entry[S]{Input: Input[S]{Name: resolved, Value: input.Value}, SourceName: input.Name})
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
			for name, candidate := range object {
				var value S
				if text, scalar := ScalarString(candidate); scalar {
					value = r.NewText(text, source)
				}
				visit(Entry[S]{Input: Input[S]{Name: r.NewText(name, source), Value: value}, Evaluated: true})
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
			inputs[strings.ToLower(r.Text(entry.Name))] = entry.Input
		}
	})
	return inputs, complete
}
