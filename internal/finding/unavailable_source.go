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

type unavailableProducer uint8

const (
	nativeShellcheck unavailableProducer = iota + 1
	pluginShellcheck
)

// UnavailableSource is temporary provenance for ShellCheck's reported foreign
// files. Its snippet is display-only, never revision evidence. E7/M07 delete
// this marker, legacy source snippets and CompleteDiagnostic's allowance when
// reported-source admission replaces them. The static gate permits two producers.
type UnavailableSource struct {
	state    unavailableState
	producer unavailableProducer
	address  string
	document string
}

// NativeShellcheckUnavailable records whether the optional display reread failed.
// Both states deliberately lack evidence of the revision ShellCheck analyzed.
func NativeShellcheckUnavailable(address string, readFailed bool) UnavailableSource {
	state := sourceNotCaptured
	if readFailed {
		state = sourceReadFailed
	}
	return UnavailableSource{state: state, producer: nativeShellcheck, address: address}
}

// PluginShellcheckUnavailable preserves the transitional grant-source contract.
// M07 replaces this with Store-captured grants acquired before guest execution.
func PluginShellcheckUnavailable(address string) UnavailableSource {
	return UnavailableSource{state: sourceNotCaptured, producer: pluginShellcheck, address: address}
}

func (s UnavailableSource) Present() bool {
	return (s.state == sourceNotCaptured || s.state == sourceReadFailed) && (s.producer == nativeShellcheck || s.producer == pluginShellcheck) && s.address != "" && !strings.ContainsRune(s.address, 0)
}

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
