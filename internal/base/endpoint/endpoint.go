// Package endpoint gives each HTTP(S) authority one canonical spelling, so
// equal endpoints share one identity, cache key and network grant.
package endpoint

import (
	"errors"
	"net/netip"
	"net/url"
	"strconv"
	"strings"

	"golang.org/x/net/idna"
)

// ErrInvalid reports a URL that names no usable HTTP(S) authority.
var ErrInvalid = errors.New("invalid HTTP endpoint")

// Authority is a canonical scheme, host and port. Port is always explicit.
type Authority struct{ Scheme, Host, Port string }

// Of returns the canonical authority of an http or https URL without credentials.
func Of(u *url.URL) (Authority, error) {
	if u == nil || u.User != nil || u.Opaque != "" || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return Authority{}, ErrInvalid
	}
	// Callers may construct url.URL directly; reject malformed authority
	// syntax before Port can mistake an invalid explicit port for no port.
	if _, err := url.Parse(u.String()); err != nil {
		return Authority{}, ErrInvalid
	}
	host, err := Host(u.Hostname())
	if err != nil {
		return Authority{}, err
	}
	port := u.Port()
	if port == "" {
		port = defaultPort(u.Scheme)
	}
	port, err = Port(port)
	if err != nil {
		return Authority{}, err
	}
	return Authority{Scheme: u.Scheme, Host: host, Port: port}, nil
}

// Host returns the canonical form of a host name or IP address, without brackets.
func Host(name string) (string, error) {
	if ip, err := netip.ParseAddr(name); err == nil {
		return ip.String(), nil
	}
	ascii, err := idna.Lookup.ToASCII(name)
	if err != nil {
		return "", ErrInvalid
	}
	// Preserve a trailing dot: an absolute DNS name must not grant the
	// corresponding search-relative name access to private addresses.
	return strings.ToLower(ascii), nil
}

// Port returns a decimal TCP port without leading zeros.
func Port(port string) (string, error) {
	number, err := strconv.ParseUint(port, 10, 16)
	if err != nil {
		return "", ErrInvalid
	}
	return strconv.FormatUint(number, 10), nil
}

// URLHost returns the url.URL Host field for a, omitting the scheme's default port.
func (a Authority) URLHost() string {
	if a.Port == defaultPort(a.Scheme) {
		return HostPort(a.Host, "")
	}
	return HostPort(a.Host, a.Port)
}

// HostPort brackets an IPv6 host and appends port when it is not empty.
func HostPort(host, port string) string {
	if ip, err := netip.ParseAddr(host); err == nil && ip.Is6() {
		host = "[" + host + "]"
	}
	if port == "" {
		return host
	}
	return host + ":" + port
}

func defaultPort(scheme string) string {
	if scheme == "https" {
		return "443"
	}
	return "80"
}
