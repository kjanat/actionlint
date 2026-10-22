// Package calls parses and caches reusable-workflow contracts and permissions.
package calls

import (
	"path/filepath"

	"actionlint.kjanat.dev/internal/fsread"
	"actionlint.kjanat.dev/internal/semantics/permission"
)

var allPermissionScopes = permission.Scopes()

func absPath(path string) string {
	if p, err := filepath.Abs(path); err == nil {
		return p
	}
	return path
}

// SetReader binds dependency recording before the cache is shared with analysis workers.
func (c *Cache) SetReader(reader *fsread.Reader) { c.reader = reader }

// IgnoreInvalid prevents a syntax error from turning into a second filesystem diagnostic.
func (c *Cache) IgnoreInvalid(spec string) { c.Store(spec, nil, nil) }
