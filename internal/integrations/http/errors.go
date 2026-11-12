package acquisitionhttp

import "errors"

// PolicyError classifies acquisition policy failures without retaining URLs or
// DNS/transport errors that can contain signed queries or proxy credentials.
type PolicyError uint8

const (
	ErrInvalidEndpoint PolicyError = iota + 1
	ErrPrivateDestination
	ErrUnsupportedTransport
	ErrUnsupportedProxy
	ErrDNSFailed
	ErrConnectionFailed
)

func (f PolicyError) Error() string {
	switch f {
	case ErrInvalidEndpoint:
		return "invalid HTTP acquisition endpoint"
	case ErrPrivateDestination:
		return "HTTP acquisition requires public destination addresses"
	case ErrUnsupportedTransport:
		return "HTTP acquisition pinning requires a standard transport without custom TLS dialing or protocol handlers"
	case ErrUnsupportedProxy:
		return "HTTP acquisition pinning does not support HTTPS/SOCKS proxies or HTTP forward proxies"
	case ErrDNSFailed:
		return "HTTP acquisition DNS lookup failed"
	case ErrConnectionFailed:
		return "HTTP acquisition connection failed"
	default:
		return "HTTP acquisition failed"
	}
}

// Classify finds a policy failure through wrappers such as net/url.Error.
// Context cancellation is returned separately by the transport, unchanged.
func Classify(err error) (PolicyError, bool) {
	var failure PolicyError
	if !errors.As(err, &failure) || failure < ErrInvalidEndpoint || failure > ErrConnectionFailed {
		return 0, false
	}
	return failure, true
}
