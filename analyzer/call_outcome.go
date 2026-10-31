package analyzer

import (
	"unicode/utf8"

	"actionlint.kjanat.dev/v2/source"
)

type OpaqueReason string

const (
	DockerfileOnly OpaqueReason = "dockerfile-only"
	NonComposite   OpaqueReason = "non-composite"
)

type UnavailableReason string

const (
	NonLocal           UnavailableReason = "non-local"
	NotFetched         UnavailableReason = "not-fetched"
	LookupDisabled     UnavailableReason = "lookup-disabled"
	PlacementUncertain UnavailableReason = "placement-uncertain"
)

type UnresolvedReason string

const (
	InvalidCall     UnresolvedReason = "invalid-call"
	MissingManifest UnresolvedReason = "missing-manifest"
	ReadFailed      UnresolvedReason = "read-failed"
	ParseFailed     UnresolvedReason = "parse-failed"
	MappingFailed   UnresolvedReason = "mapping-failed"
)

type InterruptionReason string

const (
	Canceled           InterruptionReason = "canceled"
	OperationalFailure InterruptionReason = "operational-failure"
)

type Interruption struct {
	Reason InterruptionReason
	Detail string
}

type ExpandedCall struct{ Target source.Reference }
type OpaqueCall struct {
	Reason OpaqueReason
	Detail string
}
type UnavailableCall struct {
	Reason UnavailableReason
	Detail string
}
type UnresolvedCall struct {
	Reason UnresolvedReason
	Detail string
}
type DynamicCall struct{ Detail string }
type CycleCall struct {
	Target, Active source.Reference
	Owner, Closing OccurrenceID
}
type DepthLimitedCall struct{ Max int }
type InterruptedCall struct {
	Interruption
	Target Optional[source.Reference]
}

// CallResolution is a closed set of value payloads. NewCallOutcome validates it.
type CallResolution interface{ callResolution() }

func (ExpandedCall) callResolution()     {}
func (OpaqueCall) callResolution()       {}
func (UnavailableCall) callResolution()  {}
func (UnresolvedCall) callResolution()   {}
func (DynamicCall) callResolution()      {}
func (CycleCall) callResolution()        {}
func (DepthLimitedCall) callResolution() {}
func (InterruptedCall) callResolution()  {}

// CallOutcome retains a validated value payload. Its zero value is invalid.
type CallOutcome struct{ value CallResolution }

// Value returns the payload, or nil for the zero outcome. Every variant holds
// only strings, integers and Optional values, so the copy shares no storage.
func (outcome CallOutcome) Value() CallResolution { return outcome.value }

// NewCallOutcome accepts value variants only, so the stored payload is immutable.
func NewCallOutcome(value CallResolution) (CallOutcome, error) {
	valid, detail := false, ""
	switch value := value.(type) {
	case ExpandedCall:
		valid = validSource(value.Target)
	case OpaqueCall:
		valid, detail = value.Reason == DockerfileOnly || value.Reason == NonComposite, value.Detail
	case UnavailableCall:
		valid, detail = value.Reason == NonLocal || value.Reason == NotFetched || value.Reason == LookupDisabled || value.Reason == PlacementUncertain, value.Detail
	case UnresolvedCall:
		valid, detail = value.Reason == InvalidCall || value.Reason == MissingManifest || value.Reason == ReadFailed || value.Reason == ParseFailed || value.Reason == MappingFailed, value.Detail
	case DynamicCall:
		valid, detail = true, value.Detail
	case CycleCall:
		valid = validSource(value.Target) && validSource(value.Active) && validOccurrence(value.Owner) && validOccurrence(value.Closing)
	case DepthLimitedCall:
		valid = value.Max > 0
	case InterruptedCall:
		valid, detail = validInterruption(value.Interruption), value.Detail
		if target, present := value.Target.Get(); present {
			valid = valid && validSource(target)
		}
	}
	if !valid || !utf8.ValidString(detail) {
		return CallOutcome{}, invalidGraph("invalid outcome payload %T", value)
	}
	return CallOutcome{value: value}, nil
}

func validInterruption(value Interruption) bool {
	return (value.Reason == Canceled || value.Reason == OperationalFailure) && utf8.ValidString(value.Detail)
}

func validSource(ref source.Reference) bool {
	return ref.Document != "" && ref.Node != "" && utf8.ValidString(ref.Document) && utf8.ValidString(ref.Revision) && utf8.ValidString(ref.Node) && utf8.ValidString(ref.Address)
}

func validOccurrence(id OccurrenceID) bool {
	if len(id) != 64 {
		return false
	}
	for _, char := range id {
		if char < '0' || char > '9' && char < 'a' || char > 'f' {
			return false
		}
	}
	return true
}
