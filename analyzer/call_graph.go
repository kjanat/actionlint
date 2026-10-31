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

// OptionalValue lists the invocation graph records that may be absent. Each
// holds only strings and integers, so an Optional copy shares no storage.
type OptionalValue interface {
	InvocationStop | Interruption | source.Reference
}

// Optional distinguishes absence from a present zero value.
type Optional[T OptionalValue] struct {
	value   T
	present bool
}

func Present[T OptionalValue](value T) Optional[T] { return Optional[T]{value: value, present: true} }
func (value Optional[T]) Get() (T, bool)           { return value.value, value.present }

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

// CallGraphData supplies records for validation and immutable storage.
// Complete describes expansion independently of script text collection.
type CallGraphData struct {
	Entry             InvocationEntry
	Budget            InvocationBudget
	Calls             []Call
	Scripts           []ScriptOccurrence
	Stop              Optional[InvocationStop]
	EntryInterruption Optional[Interruption]
	Complete          bool
}

func (graph CallGraphData) clone() CallGraphData {
	graph.Calls = detachedCalls(graph.Calls)
	graph.Scripts = detachedScripts(graph.Scripts)
	return graph
}

// detached copies call with its own Ancestry. Outcome is an immutable value
// handle and stays shared.
func (call Call) detached() Call {
	call.Ancestry = slices.Clone(call.Ancestry)
	return call
}

// detached copies script with its own Ancestry.
func (script ScriptOccurrence) detached() ScriptOccurrence {
	script.Ancestry = slices.Clone(script.Ancestry)
	return script
}

// detachedCalls copies calls and each call's Ancestry, keeping nil as nil.
func detachedCalls(calls []Call) []Call {
	if calls == nil {
		return nil
	}
	out := make([]Call, 0, len(calls))
	for _, call := range calls {
		out = append(out, call.detached())
	}
	return out
}

// detachedScripts copies scripts and each script's Ancestry, keeping nil as nil.
func detachedScripts(scripts []ScriptOccurrence) []ScriptOccurrence {
	if scripts == nil {
		return nil
	}
	out := make([]ScriptOccurrence, 0, len(scripts))
	for _, script := range scripts {
		out = append(out, script.detached())
	}
	return out
}

// CallGraph is a value handle to validated, immutable occurrence records.
// Accessors detach mutable slices; copying the handle shares only private storage.
type CallGraph struct{ data *callGraphData }

type callGraphData struct {
	records CallGraphData
	calls   map[OccurrenceID]int
	scripts map[OccurrenceID]int
}

const MaxCallGraphPageSize = 256

func NewCallGraph(records CallGraphData) (CallGraph, error) {
	records = records.clone()
	if err := records.validate(); err != nil {
		return CallGraph{}, err
	}
	data := &callGraphData{
		records: records,
		calls:   make(map[OccurrenceID]int, len(records.Calls)),
		scripts: make(map[OccurrenceID]int, len(records.Scripts)),
	}
	for index, call := range data.records.Calls {
		data.calls[call.ID] = index
	}
	for index, script := range data.records.Scripts {
		data.scripts[script.ID] = index
	}
	return CallGraph{data: data}, nil
}

func (graph CallGraph) Valid() bool { return graph.data != nil }
func (graph CallGraph) Entry() InvocationEntry {
	if !graph.Valid() {
		return InvocationEntry{}
	}
	return graph.data.records.Entry
}
func (graph CallGraph) Budget() InvocationBudget {
	if !graph.Valid() {
		return InvocationBudget{}
	}
	return graph.data.records.Budget
}
func (graph CallGraph) Complete() bool { return graph.Valid() && graph.data.records.Complete }
func (graph CallGraph) Stop() Optional[InvocationStop] {
	if !graph.Valid() {
		return Optional[InvocationStop]{}
	}
	return graph.data.records.Stop
}
func (graph CallGraph) EntryInterruption() Optional[Interruption] {
	if !graph.Valid() {
		return Optional[Interruption]{}
	}
	return graph.data.records.EntryInterruption
}
func (graph CallGraph) CallCount() int {
	if !graph.Valid() {
		return 0
	}
	return len(graph.data.records.Calls)
}
func (graph CallGraph) ScriptCount() int {
	if !graph.Valid() {
		return 0
	}
	return len(graph.data.records.Scripts)
}
func (graph CallGraph) Call(id OccurrenceID) (Call, bool) {
	if !graph.Valid() {
		return Call{}, false
	}
	index, found := graph.data.calls[id]
	if !found {
		return Call{}, false
	}
	return graph.data.records.Calls[index].detached(), true
}
func (graph CallGraph) script(id OccurrenceID) (ScriptOccurrence, bool) {
	if !graph.Valid() {
		return ScriptOccurrence{}, false
	}
	index, found := graph.data.scripts[id]
	if !found {
		return ScriptOccurrence{}, false
	}
	return graph.data.records.Scripts[index], true
}
func (graph CallGraph) Script(id OccurrenceID) (ScriptOccurrence, bool) {
	script, found := graph.script(id)
	return script.detached(), found
}

// ValidateOccurrence checks every supplied field against the registered execution.
func (graph CallGraph) ValidateOccurrence(actual ScriptOccurrence) error {
	want, exists := graph.script(actual.ID)
	if !exists || actual.Owner != want.Owner || actual.Depth != want.Depth || actual.Declaration != want.Declaration || actual.Kind != want.Kind || actual.Input != want.Input || !slices.Equal(actual.Ancestry, want.Ancestry) {
		return invalidGraph("script %q disagrees with registered occurrence", actual.ID)
	}
	return nil
}

func (graph CallGraph) Snapshot() CallGraphData {
	if !graph.Valid() {
		return CallGraphData{}
	}
	return graph.data.records.clone()
}
func (graph CallGraph) pageEnd(offset, limit, length int) (int, error) {
	if !graph.Valid() || offset < 0 || offset > length || limit < 1 || limit > MaxCallGraphPageSize {
		return 0, invalidGraph("invalid page offset %d or limit %d", offset, limit)
	}
	return offset + min(limit, length-offset), nil
}

// CallsPage returns up to limit records in registration order; an end page is nil.
func (graph CallGraph) CallsPage(offset, limit int) ([]Call, error) {
	end, err := graph.pageEnd(offset, limit, graph.CallCount())
	if err != nil {
		return nil, err
	}
	if offset == end {
		return nil, nil
	}
	return detachedCalls(graph.data.records.Calls[offset:end]), nil
}

// ScriptsPage returns up to limit records in registration order; an end page is nil.
func (graph CallGraph) ScriptsPage(offset, limit int) ([]ScriptOccurrence, error) {
	end, err := graph.pageEnd(offset, limit, graph.ScriptCount())
	if err != nil {
		return nil, err
	}
	if offset == end {
		return nil, nil
	}
	return detachedScripts(graph.data.records.Scripts[offset:end]), nil
}

func invalidGraph(format string, values ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidCallGraph, fmt.Sprintf(format, values...))
}
