package workflows

import (
	"context"
	"path/filepath"

	"actionlint.kjanat.dev/v2/internal/input/source/capture"
)

// SetSources installs the result-owned source capture before any cache lookups.
func (c *Cache) SetSources(ctx context.Context, captures *capture.Store, workingDir string) {
	c.ctx, c.captures = ctx, captures
	c.sourceRoot = c.root
	if c.sourceRoot != "" && !filepath.IsAbs(c.sourceRoot) {
		c.sourceRoot = filepath.Join(workingDir, c.sourceRoot)
	}
}

// IgnoreInvalid prevents a syntax error from turning into a second filesystem diagnostic.
func (c *Cache) IgnoreInvalid(spec string) { c.Store(spec, nil) }
