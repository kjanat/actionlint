package artifact

import (
	"errors"

	"actionlint.kjanat.dev/v2/internal/acquisitionhttp"
)

var (
	errRedirectAddress   = errors.New("plugin acquisition requires public destination addresses")
	errRedirectTransport = errors.New("plugin address checks require a standard HTTP transport without a custom TLS dialer")
	errRedirectProxy     = errors.New("plugin address checks do not support HTTPS/SOCKS proxies or plain HTTP through a forward proxy")
	errRedirectDNS       = errors.New("plugin acquisition DNS lookup failed")
)

// acquisitionError preserves established bounded diagnostics without retaining
// transport errors that may contain signed URLs or proxy credentials.
func acquisitionError(err error) error {
	failure, known := acquisitionhttp.Classify(err)
	if !known {
		return err
	}
	switch failure {
	case acquisitionhttp.ErrPrivateDestination:
		return errRedirectAddress
	case acquisitionhttp.ErrUnsupportedTransport:
		return errRedirectTransport
	case acquisitionhttp.ErrUnsupportedProxy:
		return errRedirectProxy
	case acquisitionhttp.ErrDNSFailed:
		return errRedirectDNS
	case acquisitionhttp.ErrInvalidEndpoint, acquisitionhttp.ErrConnectionFailed:
		return failure
	}
	return failure
}
