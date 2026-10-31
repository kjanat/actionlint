// Package tempdir provides test directories spelled the way source capture
// spells documents.
package tempdir

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/fsread"
)

// Physical returns t.TempDir with symlinked and short-name ancestors resolved.
// macOS places temporary directories under the /var -> /private/var symlink and
// Windows runners can report 8.3 names such as RUNNER~1. Source capture keys
// documents by physical path, so fixtures that compare read paths against the
// root must start from the same spelling.
func Physical(t testing.TB) string {
	t.Helper()
	return fsread.PhysicalPath(t.TempDir())
}
