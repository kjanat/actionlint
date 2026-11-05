// Package availability describes unavailable facts and optional scalar values.
package availability

// Reason explains why a fact is unavailable, without credentials or fetched values.
type Reason string

const (
	NotFetchedReason     Reason = "not-fetched"
	OfflineReason        Reason = "offline"
	LookupDisabledReason Reason = "lookup-disabled"
	DeniedReason         Reason = "denied"
	FailedReason         Reason = "failed"
	NotFoundReason       Reason = "not-found"
	UnsupportedReason    Reason = "unsupported"
	CacheMissReason      Reason = "cache-miss"
	InaccessibleReason   Reason = "not-found-or-inaccessible"
	LimitReason          Reason = "limit-exceeded"
)

// Unavailability is a closed set of missing-fact outcomes. Its zero value is
// not-fetched. An available value cannot be represented as Unavailability.
type Unavailability struct{ cause Reason }

func NotFetched() Unavailability     { return Unavailability{} }
func Offline() Unavailability        { return Unavailability{cause: OfflineReason} }
func LookupDisabled() Unavailability { return Unavailability{cause: LookupDisabledReason} }
func Denied() Unavailability         { return Unavailability{cause: DeniedReason} }
func Failed() Unavailability         { return Unavailability{cause: FailedReason} }
func NotFound() Unavailability       { return Unavailability{cause: NotFoundReason} }
func Unsupported() Unavailability    { return Unavailability{cause: UnsupportedReason} }
func CacheMiss() Unavailability      { return Unavailability{cause: CacheMissReason} }
func Inaccessible() Unavailability   { return Unavailability{cause: InaccessibleReason} }
func Limit() Unavailability          { return Unavailability{cause: LimitReason} }
func (u Unavailability) Reason() Reason {
	if u.cause == "" {
		return NotFetchedReason
	}
	return u.cause
}

// String contains either a known string, including an empty string, or an
// unavailable reason. It contains no interface or mutable payload.
type String struct {
	value   string
	known   bool
	missing Unavailability
}

func Available(value string) String                  { return String{value: value, known: true} }
func UnavailableString(reason Unavailability) String { return String{missing: reason} }
func (s String) Available() bool                     { return s.known }
func (s String) Value() (string, bool)               { return s.value, s.known }
func (s String) Reason() Reason {
	if s.known {
		return ""
	}
	return s.missing.Reason()
}
