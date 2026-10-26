package analyzer

import (
	"errors"
	"fmt"
	"slices"

	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/source"
)

var ErrInvalidCallGraph = errors.New("analyzer: invalid invocation graph")

// OccurrenceID is an opaque identity supplied by the host.
type OccurrenceID string

// Optional distinguishes absence from a present zero value.
type Optional[T any] struct {
	value   T
	present bool
}

func Present[T any](value T) Optional[T] { return Optional[T]{value: value, present: true} }
func (value Optional[T]) Get() (T, bool) { return value.value, value.present }

type InvocationEntryKind string

const (
	WorkflowEntry InvocationEntryKind = "workflow"
	ActionEntry   InvocationEntryKind = "action"
)

type InvocationEntry struct {
	ID        OccurrenceID
	Kind      InvocationEntryKind
	Reference source.Reference
}

type InvocationBudget struct{ MaxDepth, MaxCalls, MaxScripts int }

type CallCondition string

const (
	CallEnabled          CallCondition = "enabled"
	CallSkipped          CallCondition = "skipped"
	CallRuntimeDependent CallCondition = "runtime-dependent"
)

type ScriptKind string

const (
	RunScript         ScriptKind = "run"
	ActionInputScript ScriptKind = "action-input"
)

// ScriptOccurrence identifies one execution of a declaration. Ancestry runs
// from the entry through Owner; Depth is the owner's call depth.
type ScriptOccurrence struct {
	ID, Owner   OccurrenceID
	Ancestry    []OccurrenceID
	Depth       int
	Declaration source.Reference
	Kind        ScriptKind
	Input       string
}

// Call ancestry runs from the entry through Parent, excluding the call itself.
type Call struct {
	ID, Parent OccurrenceID
	Ancestry   []OccurrenceID
	Depth      int
	Site       source.Reference
	Step       diagnostic.Position
	Uses       string
	HasUses    bool
	Condition  CallCondition
	Outcome    CallOutcome
}

type StopLimit string

const (
	CallLimit   StopLimit = "total"
	ScriptLimit StopLimit = "scripts"
)

// InvocationStop names the rejected call site or script declaration for Limit.
type InvocationStop struct {
	Parent OccurrenceID
	Site   source.Reference
	Limit  StopLimit
	Max    int
}

// CallGraph carries all registered occurrences, including when script text was
// not collected. Complete describes expansion, independently of text collection.
type CallGraph struct {
	Entry             InvocationEntry
	Budget            InvocationBudget
	Calls             []Call
	Scripts           []ScriptOccurrence
	Stop              Optional[InvocationStop]
	EntryInterruption Optional[Interruption]
	Complete          bool
}

// Clone detaches every ancestry range, including ranges shared by the input.
func (graph CallGraph) Clone() CallGraph {
	graph.Calls = slices.Clone(graph.Calls)
	graph.Scripts = slices.Clone(graph.Scripts)
	total := 0
	for _, call := range graph.Calls {
		total += len(call.Ancestry)
	}
	for _, script := range graph.Scripts {
		total += len(script.Ancestry)
	}
	storage := make([]OccurrenceID, total)
	copyAncestry := func(original []OccurrenceID) []OccurrenceID {
		if original == nil {
			return nil
		}
		length := len(original)
		cloned := storage[:length:length]
		copy(cloned, original)
		storage = storage[length:]
		return cloned
	}
	for i := range graph.Calls {
		graph.Calls[i].Ancestry = copyAncestry(graph.Calls[i].Ancestry)
	}
	for i := range graph.Scripts {
		graph.Scripts[i].Ancestry = copyAncestry(graph.Scripts[i].Ancestry)
	}
	return graph
}

func invalidGraph(format string, values ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidCallGraph, fmt.Sprintf(format, values...))
}
