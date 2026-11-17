package sourcetext

import (
	"net/url"
	"path/filepath"
	"strings"
)

// RemoteAddress reports whether address is an HTTPS source URL without credentials.
func RemoteAddress(address string) bool {
	u, err := url.Parse(address)
	return err == nil && u.Scheme == "https" && u.Host != "" && u.User == nil && u.RawQuery == "" && u.Fragment == ""
}

// FileURI converts an absolute local path to a file URI, following
// https://www.rfc-editor.org/rfc/rfc8089. On Windows, drive paths become
// file:///C:/... and UNC paths become file://host/share/..., including their
// \\?\ spellings. It returns "" for a path no file URI names: a relative
// path, or a Windows device path such as \\.\NUL.
func FileURI(path string) string {
	path = filepath.ToSlash(path)
	u := url.URL{Scheme: "file"}
	if filepath.Separator == '\\' {
		switch {
		case strings.HasPrefix(strings.ToUpper(path), "//?/UNC/"):
			path = "//" + path[len("//?/UNC/"):]
		case strings.HasPrefix(path, "//?/"):
			path = path[len("//?/"):]
		case strings.HasPrefix(path, "//./"):
			return ""
		}
		switch {
		case strings.HasPrefix(path, "//"):
			var rest string
			u.Host, rest, _ = strings.Cut(path[2:], "/")
			path = "/" + rest
		case len(path) >= 2 && path[1] == ':':
			path = "/" + path
		default:
			return ""
		}
	} else if !strings.HasPrefix(path, "/") {
		return ""
	}
	u.Path = path
	return u.String()
}
