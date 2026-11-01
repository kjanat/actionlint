package acquisitionhttp

import "errors"

// Failure classifies acquisition policy failures without retaining URLs or
// DNS/transport errors that can contain signed queries or proxy credentials.
type Failure uint8

const (
	InvalidEndpoint Failure = iota + 1
	PrivateDestination
	UnsupportedTransport
	UnsupportedProxy
	DNSFailed
	ConnectionFailed
)

func (f Failure) Error() string {
	switch f {
	case InvalidEndpoint:
		return "invalid HTTP acquisition endpoint"
	case PrivateDestination:
		return "HTTP acquisition requires public destination addresses"
	case UnsupportedTransport:
		return "HTTP acquisition pinning requires a standard transport without a custom TLS dialer"
	case UnsupportedProxy:
		return "HTTP acquisition pinning does not support HTTPS/SOCKS proxies or HTTP forward proxies"
	case DNSFailed:
		return "HTTP acquisition DNS lookup failed"
	case ConnectionFailed:
		return "HTTP acquisition connection failed"
	default:
		return "HTTP acquisition failed"
	}
}

// Classify finds a policy failure through wrappers such as net/url.Error.
// Context cancellation is returned separately by the transport, unchanged.
func Classify(err error) (Failure, bool) {
	var failure Failure
	if !errors.As(err, &failure) || failure < InvalidEndpoint || failure > ConnectionFailed {
		return 0, false
	}
	return failure, true
}
