package results

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

// UnavailableSource snippets are display-only and prove no revision.
// E7/M07 delete this marker when reported-source admission replaces it.
type UnavailableSource struct {
	state    unavailableState
	provider string
	address  string
	document string
}

func ReportedSourceUnavailable(provider, address string, readFailed bool) UnavailableSource {
	state := sourceNotCaptured
	if readFailed {
		state = sourceReadFailed
	}
	return UnavailableSource{state: state, provider: provider, address: address}
}

func (s UnavailableSource) Present() bool {
	return (s.state == sourceNotCaptured || s.state == sourceReadFailed) && s.provider != "" && s.address != "" && !strings.ContainsRune(s.address, 0)
}

func (s UnavailableSource) Provider() string { return s.provider }

func (s UnavailableSource) Reason() string {
	switch s.state {
	case sourceNotCaptured:
		return "not-captured"
	case sourceReadFailed:
		return "read-failed"
	case sourceNotUnavailable:
		fallthrough
	default:
		return ""
	}
}

func (s UnavailableSource) Document() string { return s.document }
func (s UnavailableSource) Address() string  { return s.address }

func (s UnavailableSource) Equal(other UnavailableSource) bool { return s == other }

// BindDocument grants no revision, fix or coordinate evidence.
func (s UnavailableSource) BindDocument(document string) (UnavailableSource, error) {
	if !s.Present() || document == "" || strings.ContainsRune(document, 0) {
		return UnavailableSource{}, errors.New("unavailable reported source requires a marker and canonical document")
	}
	s.document = document
	return s, nil
}
