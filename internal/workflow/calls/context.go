// Package calls parses and caches reusable-workflow contracts and permissions.
package calls

import (
	"context"
	"path/filepath"

	"actionlint.kjanat.dev/v2/internal/fsread"
	"actionlint.kjanat.dev/v2/internal/semantics/permission"
	"actionlint.kjanat.dev/v2/internal/source/capture"
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

// SetSources installs the result-owned source capture before any cache lookups.
func (c *Cache) SetSources(ctx context.Context, captures *capture.Store, workingDir string) {
	c.ctx, c.captures, c.sourceBase = ctx, captures, workingDir
}

// IgnoreInvalid prevents a syntax error from turning into a second filesystem diagnostic.
func (c *Cache) IgnoreInvalid(spec string) { c.Store(spec, nil, nil) }
