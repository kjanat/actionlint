// Package invocation owns static call occurrences and expansion state.
package invocation

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"unicode/utf8"

	"actionlint.kjanat.dev/v2/source"
)

// Errors returned for programming or input misuse.
var (
	ErrInvalidEntry       = errors.New("invocation: invalid entry")
	ErrInvalidSource      = errors.New("invocation: invalid source identity")
	ErrInvalidBudget      = errors.New("invocation: invalid budget")
	ErrInvalidID          = errors.New("invocation: invalid id")
	ErrInvalidScript      = errors.New("invocation: invalid script identity")
	ErrDuplicateScript    = errors.New("invocation: duplicate script traversal")
	ErrScriptBudget       = errors.New("invocation: script budget exhausted")
	ErrCallBudget         = errors.New("invocation: call budget exhausted")
	ErrUnknownParent      = errors.New("invocation: unknown parent")
	ErrParentNotOpen      = errors.New("invocation: parent is not an open expanded call")
	ErrDuplicateCall      = errors.New("invocation: duplicate call traversal")
	ErrUnknownCall        = errors.New("invocation: unknown call")
	ErrNotPending         = errors.New("invocation: call is not pending")
	ErrNotOpen            = errors.New("invocation: call body is not open")
	ErrUnfinished         = errors.New("invocation: unfinished calls")
	ErrAlreadyInterrupted = errors.New("invocation: entry already interrupted")
	ErrPublished          = errors.New("invocation: builder already published")
	ErrTraversal          = errors.New("invocation: unfinished traversal branch")
	ErrInterrupted        = errors.New("invocation: traversal interrupted")
	ErrInvalidCondition   = errors.New("invocation: invalid condition")
)

// EntryKind distinguishes workflow entries from standalone action entries.
type EntryKind string

// Entry kinds.
const (
	EntryWorkflow EntryKind = "workflow"
	EntryAction   EntryKind = "action"
)

// Valid reports whether k is a known entry kind.
func (k EntryKind) Valid() bool { return k == EntryWorkflow || k == EntryAction }

// Entry is the analysis root. It has no incoming call.
type Entry struct {
	Kind      EntryKind
	Reference source.Reference
}

// ID is a deterministic occurrence identifier (lowercase hex SHA-256).
type ID string

func (id ID) valid() bool {
	if len(id) != sha256.Size*2 {
		return false
	}
	for _, c := range id {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// Condition describes execution independently of declaration validation.
type Condition uint8

const (
	ConditionInvalid Condition = iota
	ConditionEnabled
	ConditionSkipped
	ConditionRuntimeDependent
)

func (c Condition) valid() bool {
	return c == ConditionEnabled || c == ConditionSkipped || c == ConditionRuntimeDependent
}

// Budget bounds expansion of one entry, including scripts in the entry itself.
type Budget struct {
	// MaxDepth is the maximum number of call edges from the entry.
	MaxDepth int
	// MaxCalls is the maximum number of recorded call occurrences.
	MaxCalls int
	// MaxScripts bounds run and action-input occurrences, including when no
	// script analyzer consumes them. Exhaustion stops the entry.
	MaxScripts int
}

// Application defaults per entry.
const (
	DefaultMaxDepth   = 10
	DefaultMaxCalls   = 10000
	DefaultMaxScripts = 10000
)

// DefaultBudget returns the application composition budget.
func DefaultBudget() Budget {
	return Budget{MaxDepth: DefaultMaxDepth, MaxCalls: DefaultMaxCalls, MaxScripts: DefaultMaxScripts}
}

func (b Budget) validate() error {
	if b.MaxDepth <= 0 || b.MaxCalls <= 0 || b.MaxScripts <= 0 {
		return fmt.Errorf("%w: limits must be positive (depth %d, calls %d, scripts %d)", ErrInvalidBudget, b.MaxDepth, b.MaxCalls, b.MaxScripts)
	}
	return nil
}

// ScriptKind identifies how a script is declared.
type ScriptKind string

// Script kinds.
const (
	ScriptRun         ScriptKind = "run"
	ScriptActionInput ScriptKind = "action-input"
)

const idPrefix = "actionlint:invocation:v1\x00"

// digest hashes the domain prefix and length-prefixed fields.
func digest(domain string, fields ...string) ID {
	buf := []byte(idPrefix + domain + "\x00")
	for _, f := range fields {
		buf = strconv.AppendInt(buf, int64(len(f)), 10)
		buf = append(buf, ':')
		buf = append(buf, f...)
	}
	sum := sha256.Sum256(buf)
	return ID(hex.EncodeToString(sum[:]))
}

func validateRef(name string, ref source.Reference) error {
	if ref.Document == "" || ref.Node == "" {
		return fmt.Errorf("%w: %s needs document and node", ErrInvalidSource, name)
	}
	for _, s := range [...]string{ref.Document, ref.Revision, ref.Node, ref.Address} {
		if !utf8.ValidString(s) {
			return fmt.Errorf("%w: %s is not valid UTF-8", ErrInvalidSource, name)
		}
	}
	return nil
}

// NewRootID returns the entry ID. Revision and address are not part of it.
func NewRootID(kind EntryKind, ref source.Reference) (ID, error) {
	if !kind.Valid() {
		return "", fmt.Errorf("%w: kind %q", ErrInvalidEntry, string(kind))
	}
	if err := validateRef("entry", ref); err != nil {
		return "", err
	}
	return digest("root", string(kind), ref.Document, ref.Node), nil
}

// NewCallID returns the call ID for a parent and call-site reference.
func NewCallID(parent ID, site source.Reference) (ID, error) {
	if !parent.valid() {
		return "", fmt.Errorf("%w: malformed parent", ErrInvalidID)
	}
	if err := validateRef("call site", site); err != nil {
		return "", err
	}
	return digest("call", string(parent), site.Document, site.Node), nil
}

// NewScriptID returns the script occurrence ID for a declaration within the
// entry or call identified by owner. Run scripts have no input name.
func NewScriptID(owner ID, decl source.Reference, kind ScriptKind, input string) (ID, error) {
	if !owner.valid() {
		return "", fmt.Errorf("%w: malformed owner", ErrInvalidID)
	}
	if err := validateRef("script declaration", decl); err != nil {
		return "", err
	}
	switch kind {
	case ScriptRun:
		if input != "" {
			return "", fmt.Errorf("%w: run script has input name", ErrInvalidScript)
		}
	case ScriptActionInput:
		if input == "" {
			return "", fmt.Errorf("%w: action input needs a name", ErrInvalidScript)
		}
	default:
		return "", fmt.Errorf("%w: kind %q", ErrInvalidScript, string(kind))
	}
	if !utf8.ValidString(input) {
		return "", fmt.Errorf("%w: input is not valid UTF-8", ErrInvalidScript)
	}
	return digest("script", string(owner), decl.Document, decl.Node, string(kind), input), nil
}

// Call is one recorded call occurrence. Caller and target declarations stay
// separate: Site is the caller's declaration, the target lives in Outcome.
type Call struct {
	Context
	Outcome Outcome
}

// Stop identifies the first declaration rejected by a call or script budget.
type Stop struct {
	Parent ID
	Site   source.Reference
	Limit  LimitKind
	Max    int
}

type budgetError struct{ stop Stop }

func (e budgetError) Error() string {
	return fmt.Sprintf("invocation: %s budget exhausted (maximum %d per entry)", e.stop.Limit, e.stop.Max)
}

func (e budgetError) Is(target error) bool {
	return e.stop.Limit == LimitTotal && target == ErrCallBudget || e.stop.Limit == LimitScripts && target == ErrScriptBudget
}

// Graph is an immutable published invocation graph. Accessors return copies.
type Graph struct {
	entry       Entry
	root        ID
	rootContext Context
	budget      Budget
	calls       []Call
	index       map[ID]int
	scripts     []Script
	scriptIndex map[ID]int
	stop        Stop
	hasStop     bool
	entryStop   Outcome
	incomplete  bool
}

// Entry returns the analysis entry.
func (g Graph) Entry() Entry { return g.entry }

// RootID returns the entry occurrence ID.
func (g Graph) RootID() ID { return g.root }

// Budget returns the limits the graph was built under.
func (g Graph) Budget() Budget { return g.budget }

// Calls returns all recorded calls in creation order.
func (g Graph) Calls() []Call { return slices.Clone(g.calls) }

// Call returns the call with the given ID.
func (g Graph) Call(id ID) (Call, bool) {
	i, ok := g.index[id]
	if !ok {
		return Call{}, false
	}
	return g.calls[i], true
}

// Has reports whether id names the entry or a recorded call.
func (g Graph) Has(id ID) bool {
	if id == "" {
		return false
	}
	_, ok := g.index[id]
	return ok || id == g.root
}

// Depth returns the number of call edges from the entry to id.
func (g Graph) Depth(id ID) (int, bool) {
	if id != "" && id == g.root {
		return 0, true
	}
	c, ok := g.Call(id)
	return c.Depth(), ok
}

// Children returns the direct calls of id in creation order.
func (g Graph) Children(id ID) []Call {
	var out []Call
	for _, c := range g.calls {
		if c.Parent() == id {
			out = append(out, c)
		}
	}
	return out
}

// Path returns the calls from the outermost call down to id, excluding the
// entry. The path of the entry is empty.
func (g Graph) Path(id ID) ([]Call, bool) {
	if id != "" && id == g.root {
		return nil, true
	}
	c, ok := g.Call(id)
	if !ok {
		return nil, false
	}
	path := make([]Call, c.Depth())
	for i := c.Depth() - 1; i >= 0; i-- {
		path[i] = c
		if i > 0 {
			c = g.calls[g.index[c.Parent()]]
		}
	}
	return path, true
}

// Chain returns the IDs from the entry through id inclusive.
func (g Graph) Chain(id ID) ([]ID, bool) {
	path, ok := g.Path(id)
	if !ok {
		return nil, false
	}
	chain := make([]ID, 0, len(path)+1)
	chain = append(chain, g.root)
	for _, c := range path {
		chain = append(chain, c.ID())
	}
	return chain, true
}

// Stop returns the first exhausted call or script budget boundary.
func (g Graph) Stop() (Stop, bool) { return g.stop, g.hasStop }

// EntryInterruption returns interruption outside an unfinished call.
func (g Graph) EntryInterruption() (Outcome, bool) {
	return g.entryStop, g.entryStop.Valid()
}

// Incomplete reports unavailable bodies, unresolved calls or interrupted expansion.
func (g Graph) Incomplete() bool { return g.incomplete }

type recordState uint8

const (
	statePending recordState = iota + 1
	stateOpen
	stateFinal
)

type record struct {
	call  Call
	state recordState
}

// Builder owns occurrence state while traversal runs. It is not safe for
// concurrent use.
type Builder struct {
	entry       Entry
	root        ID
	rootContext Context
	budget      Budget
	records     []record
	index       map[ID]int
	scripts     []Script
	scriptIndex map[ID]int
	stop        Stop
	hasStop     bool
	entryStop   Outcome
	published   bool
	current     ID
	interrupted bool
}

// NewBuilder starts a graph for an entry. Pass DefaultBudget for application
// limits.
func NewBuilder(kind EntryKind, ref source.Reference, budget Budget) (*Builder, error) {
	root, err := NewRootID(kind, ref)
	if err != nil {
		return nil, err
	}
	if err := budget.validate(); err != nil {
		return nil, err
	}
	return &Builder{
		entry:       Entry{Kind: kind, Reference: ref},
		root:        root,
		rootContext: Context{data: &contextData{id: root, site: ref}},
		current:     root,
		budget:      budget,
		index:       make(map[ID]int),
	}, nil
}

// RootID returns the entry occurrence ID, the parent of top-level calls.
func (b *Builder) RootID() ID { return b.root }

// IsBudgetStop accepts wrappers only when every cause is this builder's stop.
func (b *Builder) IsBudgetStop(err error) bool {
	if !b.hasStop || err == nil {
		return false
	}
	if group, ok := errors.AsType[interface {
		error
		Unwrap() []error
	}](err); ok {
		causes := group.Unwrap()
		if len(causes) == 0 {
			return false
		}
		for _, cause := range causes {
			if !b.IsBudgetStop(cause) {
				return false
			}
		}
		return true
	}
	exhausted, ok := errors.AsType[budgetError](err)
	return ok && exhausted.stop == b.stop
}

// Admission describes what Begin did with an attempted call.
type Admission uint8

// Admission results.
const (
	// Admitted means a pending record exists; resolve it after lookup.
	Admitted Admission = iota + 1
	// DepthLimited means a final non-expanded frontier record was stored.
	DepthLimited
	// Stopped means a call or script budget was exhausted; no call was allocated.
	Stopped
)

// Ticket is the result of Begin.
type Ticket struct {
	context Context
	status  Admission
	err     error
}

// ID returns the call ID, or empty when the attempt was Stopped.
func (t Ticket) ID() ID { return t.context.ID() }

func (t Ticket) Context() (Context, bool) {
	if t.status != Admitted {
		return Context{}, false
	}
	return t.context, true
}

// Status returns the admission result.
func (t Ticket) Status() Admission { return t.status }

// Err returns the original budget error for a stopped admission, otherwise nil.
func (t Ticket) Err() error { return t.err }

// Begin admits a call under the entry or an open expanded call context.
// It checks parent and site validity and duplicate traversal, then the total
// budget, then depth, all before metadata acquisition.
func (b *Builder) Begin(parentContext Context, site source.Reference, facts Facts) (Ticket, error) {
	if b.published {
		return Ticket{}, ErrPublished
	}
	if b.interrupted {
		return Ticket{}, ErrInterrupted
	}
	if !b.Owns(parentContext) {
		return Ticket{}, ErrForeignContext
	}
	parent := parentContext.ID()
	body := b.entry.Reference
	parentDepth := 0
	if parent != b.root {
		i, ok := b.index[parent]
		if !ok {
			return Ticket{}, fmt.Errorf("%w: %q", ErrUnknownParent, string(parent))
		}
		if b.records[i].state != stateOpen {
			return Ticket{}, fmt.Errorf("%w: %q", ErrParentNotOpen, string(parent))
		}
		parentDepth = b.records[i].call.Depth()
		body, _ = b.records[i].call.Outcome.Target()
	}
	if site.Document != body.Document || site.Revision != body.Revision {
		return Ticket{}, fmt.Errorf("%w: call site does not belong to its parent source snapshot", ErrInvalidSource)
	}
	id, err := NewCallID(parent, site)
	if err != nil {
		return Ticket{}, err
	}
	if _, dup := b.index[id]; dup {
		return Ticket{}, fmt.Errorf("%w: %q", ErrDuplicateCall, string(id))
	}
	if err := facts.validate(); err != nil {
		return Ticket{}, err
	}
	if parent != b.current {
		return Ticket{}, ErrTraversal
	}
	if b.hasStop {
		return Ticket{status: Stopped, err: budgetError{b.stop}}, nil
	}

	if len(b.records) >= b.budget.MaxCalls {
		b.stop = Stop{Parent: parent, Site: site, Limit: LimitTotal, Max: b.budget.MaxCalls}
		b.hasStop = true
		return Ticket{status: Stopped, err: budgetError{b.stop}}, nil
	}

	context := Context{data: &contextData{id: id, parent: parent, depth: parentDepth + 1, site: site, facts: facts}}
	call := Call{Context: context}
	if call.Depth() > b.budget.MaxDepth {
		call.Outcome = depthLimit(b.budget.MaxDepth)
		b.add(record{call: call, state: stateFinal})
		return Ticket{context: context, status: DepthLimited}, nil
	}
	b.add(record{call: call, state: statePending})
	b.current = id
	return Ticket{context: context, status: Admitted}, nil
}

func (b *Builder) add(r record) {
	b.index[r.call.ID()] = len(b.records)
	b.records = append(b.records, r)
}

// Resolve records the lookup outcome of a pending call. It returns the stored
// outcome: an expanded target that is already active becomes a cycle. Descend
// into the call only when the result is expanded.
func (b *Builder) Resolve(context Context, lookup Outcome) (Outcome, error) {
	if !b.Owns(context) {
		return Outcome{}, ErrForeignContext
	}
	id := context.ID()
	if b.published {
		return Outcome{}, ErrPublished
	}
	i, ok := b.index[id]
	if !ok {
		return Outcome{}, fmt.Errorf("%w: %q", ErrUnknownCall, string(id))
	}
	rec := &b.records[i]
	if rec.state != statePending {
		return Outcome{}, fmt.Errorf("%w: %q", ErrNotPending, string(id))
	}
	if !callerResolvable(lookup.kind) {
		return Outcome{}, fmt.Errorf("%w: %s cannot be resolved by a caller", ErrInvalidOutcome, lookup.kind)
	}
	if lookup.kind == OutcomeExpanded {
		if owner, active, found := b.activeOwner(rec.call.Parent(), lookup.target); found {
			lookup = cycleOutcome(lookup.target, active, owner, id)
		}
	}
	rec.call.Outcome = lookup
	if lookup.kind == OutcomeExpanded {
		rec.state = stateOpen
		return lookup, nil
	}
	rec.state = stateFinal
	b.current = rec.call.Parent()
	return lookup, nil
}

// callerResolvable reports whether a caller may settle a pending call with an
// outcome of kind.
func callerResolvable(kind OutcomeKind) bool {
	switch kind {
	case OutcomeExpanded, OutcomeOpaque, OutcomeUnavailable, OutcomeUnresolved, OutcomeDynamic:
		return true
	case OutcomeInvalid, OutcomeCycle, OutcomeDepthLimit, OutcomeInterrupted:
	}
	return false
}

func sameDeclaration(a, c source.Reference) bool {
	return a.Document == c.Document && a.Node == c.Node
}

// activeOwner finds the nearest ancestor whose expanded target declares the
// same document/node, including the standalone action entry itself.
func (b *Builder) activeOwner(parent ID, target source.Reference) (ID, source.Reference, bool) {
	for p := parent; p != b.root; {
		rec := b.records[b.index[p]]
		if t, ok := rec.call.Outcome.Target(); ok && rec.call.Outcome.kind == OutcomeExpanded && sameDeclaration(t, target) {
			return p, t, true
		}
		p = rec.call.Parent()
	}
	if b.entry.Kind == EntryAction && sameDeclaration(b.entry.Reference, target) {
		return b.root, b.entry.Reference, true
	}
	return "", source.Reference{}, false
}

// Close ends the body of an expanded call. Every child must be final. Closing
// an interrupted call is a no-op so callers can unwind uniformly.
func (b *Builder) Close(context Context) error {
	if !b.Owns(context) {
		return ErrForeignContext
	}
	id := context.ID()
	if b.published {
		return ErrPublished
	}
	i, ok := b.index[id]
	if !ok {
		return fmt.Errorf("%w: %q", ErrUnknownCall, string(id))
	}
	rec := &b.records[i]
	if rec.state == stateFinal && rec.call.Outcome.kind == OutcomeInterrupted {
		return nil
	}
	if rec.state != stateOpen {
		return fmt.Errorf("%w: %q", ErrNotOpen, string(id))
	}
	if b.current != id {
		return fmt.Errorf("%w: child call of %q", ErrUnfinished, string(id))
	}
	rec.state = stateFinal
	b.current = rec.call.Parent()
	return nil
}

// Interrupt marks the deepest unfinished call interrupted and returns its context.
// A pending call becomes interrupted; an open expanded call keeps its target
// on the interrupted outcome and is closed. Ancestors stay expanded and must
// still be closed.
func (b *Builder) Interrupt(cause Reason, detail string) (Context, error) {
	if b.published {
		return Context{}, ErrPublished
	}
	if b.interrupted {
		return Context{}, ErrInterrupted
	}
	if b.current == b.root {
		if err := b.InterruptEntry(cause, detail); err != nil {
			return Context{}, err
		}
		return b.rootContext, nil
	}
	rec := &b.records[b.index[b.current]]
	target, hasTarget := rec.call.Outcome.Target()
	o, err := interrupted(cause, detail, target, hasTarget)
	if err != nil {
		return Context{}, err
	}
	rec.call.Outcome = o
	rec.state = stateFinal
	b.current = rec.call.Parent()
	b.interrupted = true
	return rec.call.Context, nil
}

// InterruptEntry records interruption before or between top-level calls.
func (b *Builder) InterruptEntry(cause Reason, detail string) error {
	if b.published {
		return ErrPublished
	}
	if b.interrupted {
		return ErrAlreadyInterrupted
	}
	if b.current != b.root {
		return ErrUnfinished
	}
	o, err := interrupted(cause, detail, source.Reference{}, false)
	if err != nil {
		return err
	}
	b.entryStop = o
	b.interrupted = true
	return nil
}

// Publish seals the builder and returns the immutable graph. It fails while
// any reservation is pending or any expanded body is open.
func (b *Builder) Publish() (Graph, error) {
	if b.published {
		return Graph{}, ErrPublished
	}
	pending, open := 0, 0
	for i := range b.records {
		switch b.records[i].state {
		case statePending:
			pending++
		case stateOpen:
			open++
		case stateFinal:
		}
	}
	if pending > 0 || open > 0 {
		return Graph{}, fmt.Errorf("%w: %d pending, %d open", ErrUnfinished, pending, open)
	}

	g := Graph{
		entry:       b.entry,
		root:        b.root,
		rootContext: b.rootContext,
		budget:      b.budget,
		calls:       make([]Call, len(b.records)),
		index:       make(map[ID]int, len(b.records)),
		stop:        b.stop,
		hasStop:     b.hasStop,
		entryStop:   b.entryStop,
	}
	g.incomplete = b.hasStop || b.entryStop.Valid()
	if len(b.scripts) != 0 {
		g.scripts = slices.Clone(b.scripts)
		g.scriptIndex = make(map[ID]int, len(g.scripts))
		for index, script := range g.scripts {
			g.scriptIndex[script.ID] = index
		}
	}
	for i := range b.records {
		c := b.records[i].call
		g.calls[i] = c
		g.index[c.ID()] = i
		if k := c.Outcome.kind; k != OutcomeExpanded && k != OutcomeOpaque {
			g.incomplete = true
		}
	}
	b.published = true
	return g, nil
}
