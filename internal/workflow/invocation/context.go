package invocation

import (
	"errors"
	"fmt"
	"unicode/utf8"

	"actionlint.kjanat.dev/v2/source"
)

var (
	ErrInvalidFacts   = errors.New("invocation: invalid call facts")
	ErrForeignContext = errors.New("invocation: context is not owned by this graph")
)

// Position uses one-based Unicode character coordinates copied together from YAML.
// Missing source positions use (0,0); partially specified positions are invalid.
type Position struct{ Line, Column int }

// Facts are resolved once per call, including platform-dependent effects.
type Facts struct {
	Step                                                 Position
	Uses                                                 string
	HasUses                                              bool
	Condition                                            Condition
	AfterFailure, ContinueOnError, Background            bool
	ShellEnvironmentUnknown, ShellPathUnknown            bool
	CheckoutEnvironmentUnknown, LookupEnvironmentUnknown bool
}

func (facts Facts) validate() error {
	if !facts.Condition.valid() {
		return fmt.Errorf("%w: %w", ErrInvalidFacts, ErrInvalidCondition)
	}
	if !utf8.ValidString(facts.Uses) || !facts.HasUses && facts.Uses != "" {
		return fmt.Errorf("%w: invalid uses value", ErrInvalidFacts)
	}
	if facts.Step.Line < 0 || facts.Step.Column < 0 || (facts.Step.Line == 0) != (facts.Step.Column == 0) {
		return fmt.Errorf("%w: invalid source position", ErrInvalidFacts)
	}
	return nil
}

// Context is an immutable owner-issued handle. Its zero value is invalid.
type Context struct{ data *contextData }

type contextData struct {
	id, parent ID
	depth      int
	site       source.Reference
	facts      Facts
}

func (context Context) Valid() bool   { return context.data != nil }
func (context Context) IsEntry() bool { return context.Valid() && context.data.parent == "" }
func (context Context) ID() ID {
	if !context.Valid() {
		return ""
	}
	return context.data.id
}
func (context Context) Parent() ID {
	if !context.Valid() {
		return ""
	}
	return context.data.parent
}
func (context Context) Depth() int {
	if !context.Valid() {
		return 0
	}
	return context.data.depth
}
func (context Context) Site() source.Reference {
	if !context.Valid() {
		return source.Reference{}
	}
	return context.data.site
}
func (context Context) Facts() (Facts, bool) {
	if !context.Valid() || context.IsEntry() {
		return Facts{}, false
	}
	return context.data.facts, true
}
func (context Context) Uses() string         { facts, _ := context.Facts(); return facts.Uses }
func (context Context) Condition() Condition { facts, _ := context.Facts(); return facts.Condition }

func (b *Builder) RootContext() Context { return b.rootContext }
func (b *Builder) Context(id ID) (Context, bool) {
	if id != "" && id == b.root {
		return b.rootContext, true
	}
	i, exists := b.index[id]
	if !exists {
		return Context{}, false
	}
	return b.records[i].call.Context, true
}
func (b *Builder) Owns(context Context) bool {
	stored, exists := b.Context(context.ID())
	return exists && stored == context
}
func (g Graph) RootContext() Context { return g.rootContext }
func (g Graph) Context(id ID) (Context, bool) {
	if id != "" && id == g.root {
		return g.rootContext, true
	}
	call, exists := g.Call(id)
	return call.Context, exists
}
func (g Graph) Owns(context Context) bool {
	stored, exists := g.Context(context.ID())
	return exists && stored == context
}
