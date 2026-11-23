package invocation

import (
	"errors"
	"fmt"
	"unicode/utf8"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
)

var ErrInvalidOutcome = errors.New("invocation: invalid outcome")

type OutcomeKind uint8

const (
	OutcomeInvalid OutcomeKind = iota
	OutcomeExpanded
	OutcomeOpaque
	OutcomeUnavailable
	OutcomeUnresolved
	OutcomeDynamic
	OutcomeCycle
	OutcomeDepthLimit
	OutcomeInterrupted
)

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

type LimitKind uint8

const (
	LimitInvalid LimitKind = iota
	LimitDepth
	LimitTotal
	LimitScripts
)

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

type Reason string

const (
	ReasonDockerfileOnly Reason = "dockerfile-only"
	ReasonNonComposite   Reason = "non-composite"

	ReasonNonLocal           Reason = "non-local"
	ReasonNotFetched         Reason = "not-fetched"
	ReasonLookupDisabled     Reason = "lookup-disabled"
	ReasonPlacementUncertain Reason = "placement-uncertain"

	ReasonInvalidCall     Reason = "invalid-call"
	ReasonMissingManifest Reason = "missing-manifest"
	ReasonReadFailed      Reason = "read-failed"
	ReasonParseFailed     Reason = "parse-failed"
	ReasonMappingFailed   Reason = "mapping-failed"

	ReasonExpression Reason = "expression"

	ReasonCanceled    Reason = "canceled"
	ReasonOperational Reason = "operational-failure"

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

type CycleInfo struct {
	// Active is the ancestor's target declaration (the entry for a standalone
	// action).
	Active  sourcetext.Reference
	Owner   ID
	Closing ID
}

// Outcome is invalid at its zero value.
type Outcome struct {
	kind      OutcomeKind
	reason    Reason
	detail    string
	target    sourcetext.Reference
	hasTarget bool
	cycle     CycleInfo
	limit     int
}

func (o Outcome) Kind() OutcomeKind { return o.kind }

func (o Outcome) Valid() bool { return o.kind != OutcomeInvalid }

func (o Outcome) Reason() Reason { return o.reason }

func (o Outcome) Detail() string { return o.detail }

// Target returns the resolved target declaration for expanded and cycle
// outcomes, and for interruptions of an expanded call.
func (o Outcome) Target() (sourcetext.Reference, bool) { return o.target, o.hasTarget }

func (o Outcome) Cycle() (CycleInfo, bool) { return o.cycle, o.kind == OutcomeCycle }

func (o Outcome) Limit() (LimitKind, int, bool) {
	if o.kind != OutcomeDepthLimit {
		return LimitInvalid, 0, false
	}
	return LimitDepth, o.limit, true
}

func Expanded(target sourcetext.Reference) (Outcome, error) {
	if err := validateRef("target", target); err != nil {
		return Outcome{}, err
	}
	return Outcome{kind: OutcomeExpanded, target: target, hasTarget: true}, nil
}

func Opaque(reason Reason, detail string) (Outcome, error) {
	return reasoned(OutcomeOpaque, reason, detail)
}

func Unavailable(reason Reason, detail string) (Outcome, error) {
	return reasoned(OutcomeUnavailable, reason, detail)
}

func Unresolved(reason Reason, detail string) (Outcome, error) {
	return reasoned(OutcomeUnresolved, reason, detail)
}

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

func interrupted(reason Reason, detail string, target sourcetext.Reference, hasTarget bool) (Outcome, error) {
	o, err := reasoned(OutcomeInterrupted, reason, detail)
	if err != nil {
		return Outcome{}, err
	}
	o.target, o.hasTarget = target, hasTarget
	return o, nil
}

func cycleOutcome(target, active sourcetext.Reference, owner, closing ID) Outcome {
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
