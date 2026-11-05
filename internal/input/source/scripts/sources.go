package scriptcoords

import (
	"io/fs"
	"strings"
)

// ReportedSource reports whether a sandboxed engine may name a file it read
// as name: a relative slash-separated path below the request directory, with
// no "." or ".." elements and no NUL. Backslashes and colons are rejected
// because a Windows host would read the name as a different path than the
// engine opened.
func ReportedSource(name string) bool {
	return name != "." && fs.ValidPath(name) && !strings.ContainsAny(name, "\\:\x00")
}
