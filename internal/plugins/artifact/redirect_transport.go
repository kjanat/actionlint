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
	case acquisitionhttp.PrivateDestination:
		return errRedirectAddress
	case acquisitionhttp.UnsupportedTransport:
		return errRedirectTransport
	case acquisitionhttp.UnsupportedProxy:
		return errRedirectProxy
	case acquisitionhttp.DNSFailed:
		return errRedirectDNS
	case acquisitionhttp.InvalidEndpoint, acquisitionhttp.ConnectionFailed:
		return failure
	}
	return failure
}
