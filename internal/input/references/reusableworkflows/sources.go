package reusableworkflows

import (
	"context"
	"path/filepath"

	"actionlint.kjanat.dev/v2/internal/base/capture"
)

func (c *MetadataCache) SetRemote(remote *Remote) { c.remote = remote }

// SetSources must be called before the first cache lookup.
func (c *MetadataCache) SetSources(ctx context.Context, captures *capture.Store, workingDir string) {
	c.ctx, c.captures = ctx, captures
	c.sourceRoot = c.root
	if c.sourceRoot != "" && !filepath.IsAbs(c.sourceRoot) {
		c.sourceRoot = filepath.Join(workingDir, c.sourceRoot)
	}
}
