package invocation

import (
	"errors"
	"fmt"
	"unicode/utf8"

	"actionlint.kjanat.dev/v2/source"
)

// ErrInvalidOutcome reports an outcome that is zero, malformed or not allowed
// in the requested position.
var ErrInvalidOutcome = errors.New("invocation: invalid outcome")

// OutcomeKind is the closed set of call resolution variants.
type OutcomeKind uint8

// Outcome kinds. The zero value is invalid.
const (
	OutcomeInvalid OutcomeKind = iota
	// OutcomeExpanded is a composite with a resolved target declaration.
	OutcomeExpanded
	// OutcomeOpaque is a supported action whose body is not expanded.
	OutcomeOpaque
	// OutcomeUnavailable is an external or unavailable body.
	OutcomeUnavailable
	// OutcomeUnresolved is a lookup or parse failure.
	OutcomeUnresolved
	// OutcomeDynamic is a target that depends on runtime inputs.
	OutcomeDynamic
	// OutcomeCycle is a target already active among the ancestors.
	OutcomeCycle
	// OutcomeDepthLimit is the frontier record of the depth budget.
	OutcomeDepthLimit
	// OutcomeInterrupted is an expansion interrupted by cancellation or failure.
	OutcomeInterrupted
)

// String returns a stable name for the kind.
func (k OutcomeKind) String() string {
	switch k {
	case OutcomeExpanded:
		return "expanded"
	case OutcomeOpaque:
		return "opaque"
	case OutcomeUnavailable:
		return "unavailable"
	case OutcomeUnresolved:
		return "unresolved"
	case OutcomeDynamic:
		return "dynamic"
	case OutcomeCycle:
		return "cycle"
	case OutcomeDepthLimit:
		return "depth-limit"
	case OutcomeInterrupted:
		return "interrupted"
	case OutcomeInvalid:
	}
	return "invalid"
}

// LimitKind names an exhausted budget.
type LimitKind uint8

// Limit kinds. The zero value is invalid.
const (
	LimitInvalid LimitKind = iota
	LimitDepth
	LimitTotal
	LimitScripts
)

// String returns a stable name for the limit.
func (k LimitKind) String() string {
	switch k {
	case LimitDepth:
		return "depth"
	case LimitTotal:
		return "total"
	case LimitScripts:
		return "scripts"
	case LimitInvalid:
	}
	return "invalid"
}

// Reason is a stable machine-readable explanation for an outcome.
type Reason string

// Stable reasons, grouped by the outcome kinds that accept them.
const (
	// Opaque.
	ReasonDockerfileOnly Reason = "dockerfile-only"
	ReasonNonComposite   Reason = "non-composite"

	// Unavailable.
	ReasonNonLocal           Reason = "non-local"
	ReasonNotFetched         Reason = "not-fetched"
	ReasonLookupDisabled     Reason = "lookup-disabled"
	ReasonPlacementUncertain Reason = "placement-uncertain"

	// Unresolved.
	ReasonInvalidCall     Reason = "invalid-call"
	ReasonMissingManifest Reason = "missing-manifest"
	ReasonReadFailed      Reason = "read-failed"
	ReasonParseFailed     Reason = "parse-failed"
	ReasonMappingFailed   Reason = "mapping-failed"

	// Dynamic.
	ReasonExpression Reason = "expression"

	// Interrupted.
	ReasonCanceled    Reason = "canceled"
	ReasonOperational Reason = "operational-failure"

	// Builder-owned outcomes.
	ReasonCycle      Reason = "cycle"
	ReasonDepthLimit Reason = "depth-limit"
)

func reasonAllowed(kind OutcomeKind, r Reason) bool {
	switch kind {
	case OutcomeOpaque:
		return r == ReasonDockerfileOnly || r == ReasonNonComposite
	case OutcomeUnavailable:
		return r == ReasonNonLocal || r == ReasonNotFetched || r == ReasonLookupDisabled || r == ReasonPlacementUncertain
	case OutcomeUnresolved:
		return r == ReasonInvalidCall || r == ReasonMissingManifest || r == ReasonReadFailed ||
			r == ReasonParseFailed || r == ReasonMappingFailed
	case OutcomeDynamic:
		return r == ReasonExpression
	case OutcomeInterrupted:
		return r == ReasonCanceled || r == ReasonOperational
	case OutcomeInvalid, OutcomeExpanded, OutcomeCycle, OutcomeDepthLimit:
	}
	return false
}

// CycleInfo identifies the active declaration and the call that closes it.
type CycleInfo struct {
	// Active is the ancestor's target declaration (the entry for a standalone
	// action).
	Active source.Reference
	// Owner is the entry or call whose target is Active.
	Owner ID
	// Closing is the call that re-enters Active.
	Closing ID
}

// Outcome is an immutable typed call resolution. The zero value is invalid.
type Outcome struct {
	kind      OutcomeKind
	reason    Reason
	detail    string
	target    source.Reference
	hasTarget bool
	cycle     CycleInfo
	limit     int
}

// Kind returns the outcome variant.
func (o Outcome) Kind() OutcomeKind { return o.kind }

// Valid reports whether o is a constructed outcome.
func (o Outcome) Valid() bool { return o.kind != OutcomeInvalid }

// Reason returns the stable reason; empty for expanded outcomes.
func (o Outcome) Reason() Reason { return o.reason }

// Detail returns the outcome's explanatory message.
func (o Outcome) Detail() string { return o.detail }

// Target returns the resolved target declaration for expanded and cycle
// outcomes, and for interruptions of an expanded call.
func (o Outcome) Target() (source.Reference, bool) { return o.target, o.hasTarget }

// Cycle returns cycle details for cycle outcomes.
func (o Outcome) Cycle() (CycleInfo, bool) { return o.cycle, o.kind == OutcomeCycle }

// Limit returns the exhausted budget for depth-limit outcomes.
func (o Outcome) Limit() (LimitKind, int, bool) {
	if o.kind != OutcomeDepthLimit {
		return LimitInvalid, 0, false
	}
	return LimitDepth, o.limit, true
}

// Expanded reports a composite whose body is expanded into target.
func Expanded(target source.Reference) (Outcome, error) {
	if err := validateRef("target", target); err != nil {
		return Outcome{}, err
	}
	return Outcome{kind: OutcomeExpanded, target: target, hasTarget: true}, nil
}

// Opaque reports a supported action whose body is outside composite expansion.
func Opaque(reason Reason, detail string) (Outcome, error) {
	return reasoned(OutcomeOpaque, reason, detail)
}

// Unavailable reports an external or unavailable body.
func Unavailable(reason Reason, detail string) (Outcome, error) {
	return reasoned(OutcomeUnavailable, reason, detail)
}

// Unresolved constructs a metadata lookup failure with its reason and detail.
func Unresolved(reason Reason, detail string) (Outcome, error) {
	return reasoned(OutcomeUnresolved, reason, detail)
}

// Dynamic reports a target that depends on runtime inputs.
func Dynamic(detail string) (Outcome, error) {
	return reasoned(OutcomeDynamic, ReasonExpression, detail)
}

func reasoned(kind OutcomeKind, reason Reason, detail string) (Outcome, error) {
	if !reasonAllowed(kind, reason) {
		return Outcome{}, fmt.Errorf("%w: reason %q not allowed for %s", ErrInvalidOutcome, string(reason), kind)
	}
	if !utf8.ValidString(detail) {
		return Outcome{}, fmt.Errorf("%w: detail is not valid UTF-8", ErrInvalidOutcome)
	}
	return Outcome{kind: kind, reason: reason, detail: detail}, nil
}

// interrupted keeps an expanded call's target when it has one.
func interrupted(reason Reason, detail string, target source.Reference, hasTarget bool) (Outcome, error) {
	o, err := reasoned(OutcomeInterrupted, reason, detail)
	if err != nil {
		return Outcome{}, err
	}
	o.target, o.hasTarget = target, hasTarget
	return o, nil
}

func cycleOutcome(target, active source.Reference, owner, closing ID) Outcome {
	return Outcome{
		kind:      OutcomeCycle,
		reason:    ReasonCycle,
		target:    target,
		hasTarget: true,
		cycle:     CycleInfo{Active: active, Owner: owner, Closing: closing},
	}
}

func depthLimit(maximum int) Outcome {
	return Outcome{kind: OutcomeDepthLimit, reason: ReasonDepthLimit, limit: maximum}
}
