package source

import "net/url"

// RemoteAddress reports whether address is an HTTPS source URL without credentials.
func RemoteAddress(address string) bool {
	u, err := url.Parse(address)
	return err == nil && u.Scheme == "https" && u.Host != "" && u.User == nil && u.RawQuery == "" && u.Fragment == ""
}
