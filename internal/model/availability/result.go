// Package availability describes unavailable facts and optional scalar values.
package availability

// Reason never carries credentials or fetched values.
type Reason string

const (
	NotFetchedReason              Reason = "not-fetched"
	OfflineReason                 Reason = "offline"
	LookupDisabledReason          Reason = "lookup-disabled"
	DeniedReason                  Reason = "denied"
	FailedReason                  Reason = "failed"
	NotFoundReason                Reason = "not-found"
	UnsupportedReason             Reason = "unsupported"
	UnsupportedHostReason         Reason = "unsupported-host"
	ResolverNotConfiguredReason   Reason = "resolver-not-configured"
	UnsupportedObjectFormatReason Reason = "unsupported-object-format"
	InvalidResolverResultReason   Reason = "invalid-resolver-result"
	CacheMissReason               Reason = "cache-miss"
	InaccessibleReason            Reason = "not-found-or-inaccessible"
	LimitReason                   Reason = "limit-exceeded"
)

// Unavailability has the zero value not-fetched.
type Unavailability struct{ cause Reason }

func NotFetched() Unavailability      { return Unavailability{} }
func Offline() Unavailability         { return Unavailability{cause: OfflineReason} }
func LookupDisabled() Unavailability  { return Unavailability{cause: LookupDisabledReason} }
func Denied() Unavailability          { return Unavailability{cause: DeniedReason} }
func Failed() Unavailability          { return Unavailability{cause: FailedReason} }
func NotFound() Unavailability        { return Unavailability{cause: NotFoundReason} }
func Unsupported() Unavailability     { return Unavailability{cause: UnsupportedReason} }
func UnsupportedHost() Unavailability { return Unavailability{cause: UnsupportedHostReason} }
func ResolverNotConfigured() Unavailability {
	return Unavailability{cause: ResolverNotConfiguredReason}
}

func UnsupportedObjectFormat() Unavailability {
	return Unavailability{cause: UnsupportedObjectFormatReason}
}

func InvalidResolverResult() Unavailability {
	return Unavailability{cause: InvalidResolverResultReason}
}
func CacheMiss() Unavailability    { return Unavailability{cause: CacheMissReason} }
func Inaccessible() Unavailability { return Unavailability{cause: InaccessibleReason} }
func Limit() Unavailability        { return Unavailability{cause: LimitReason} }
func (u Unavailability) Reason() Reason {
	if u.cause == "" {
		return NotFetchedReason
	}
	return u.cause
}

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
