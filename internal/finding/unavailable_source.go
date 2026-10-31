package finding

import (
	"errors"
	"strings"
)

type unavailableState uint8

const (
	sourceNotUnavailable unavailableState = iota
	sourceNotCaptured
	sourceReadFailed
)

// UnavailableSource is temporary provenance for files script tools report
// findings in. Its snippet is display-only, never revision evidence. E7/M07
// delete this marker, legacy source snippets and CompleteDiagnostic's
// allowance when reported-source admission replaces them. The producer is
// the tool provider that reported the file, named by its rule.
type UnavailableSource struct {
	state    unavailableState
	provider string
	address  string
	document string
}

// ReportedSourceUnavailable records a file the tool provider named provider
// reported a finding in, and whether the optional display reread failed.
// Neither state is evidence of the revision the tool analyzed.
func ReportedSourceUnavailable(provider, address string, readFailed bool) UnavailableSource {
	state := sourceNotCaptured
	if readFailed {
		state = sourceReadFailed
	}
	return UnavailableSource{state: state, provider: provider, address: address}
}

// Present reports whether a tool provider recorded a reported file.
func (s UnavailableSource) Present() bool {
	return (s.state == sourceNotCaptured || s.state == sourceReadFailed) && s.provider != "" && s.address != "" && !strings.ContainsRune(s.address, 0)
}

// Provider names the tool provider that reported the file.
func (s UnavailableSource) Provider() string { return s.provider }

func (s UnavailableSource) Reason() string {
	switch s.state {
	case sourceNotCaptured:
		return "not-captured"
	case sourceReadFailed:
		return "read-failed"
	case sourceNotUnavailable:
		// Available sources have no unavailable reason.
		fallthrough
	default:
		return ""
	}
}

func (s UnavailableSource) Document() string { return s.document }
func (s UnavailableSource) Address() string  { return s.address }

// Equal compares the complete unavailable provenance value.
func (s UnavailableSource) Equal(other UnavailableSource) bool { return s == other }

// BindDocument records the Store's metadata-only canonicalization of the
// reported alias. It grants no revision, fix or coordinate evidence.
func (s UnavailableSource) BindDocument(document string) (UnavailableSource, error) {
	if !s.Present() || document == "" || strings.ContainsRune(document, 0) {
		return UnavailableSource{}, errors.New("unavailable reported source requires a marker and canonical document")
	}
	s.document = document
	return s, nil
}
