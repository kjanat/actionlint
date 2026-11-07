package workflows

import (
	"context"
	"errors"
	"maps"
	"os"
	"slices"

	"actionlint.kjanat.dev/v2/internal/model/calls/references"
	"actionlint.kjanat.dev/v2/internal/model/parsing"
)

// Prepare acquires direct and transitive local workflow metadata before checks
// run. Invalid YAML and missing manifests are cached for caller diagnostics.
// Other read failures stop preparation and preserve their causes.
func (c *Cache) Prepare(ctx context.Context, snapshot parsing.SourceSnapshot) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if c == nil || c.root == "" || snapshot.Kind() != parsing.KindWorkflow {
		return nil
	}
	if err := c.Seed(snapshot); err != nil {
		return err
	}
	var pending []string
	_, dependencies := snapshot.Declarations()
	for _, dependency := range dependencies {
		if dependency.Kind == "workflow" {
			pending = append(pending, dependency.Uses)
		}
	}
	return c.LoadCalls(ctx, pending)
}

// LoadCalls acquires metadata for the supplied call references and their local
// dependencies. It is a loading operation, not a lookup for rule callbacks.
func (c *Cache) LoadCalls(ctx context.Context, referencesToLoad []string) error {
	if c == nil || c.root == "" {
		return nil
	}
	pending := slices.Clone(referencesToLoad)
	seen := make(map[string]bool)
	for index := 0; index < len(pending); index++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		spec, local := references.LocalWorkflow(pending[index])
		if !local || seen[spec] {
			continue
		}
		seen[spec] = true
		metadata, err := c.LoadForCall(ctx, spec)
		if err != nil {
			if failure, ok := errors.AsType[metadataError](err); ok && (failure.kind == failureParse || errors.Is(failure, os.ErrNotExist)) {
				continue
			}
			return err
		}
		if metadata == nil {
			continue
		}
		for _, id := range slices.Sorted(maps.Keys(metadata.JobCacheAccess)) {
			if uses := metadata.JobCacheAccess[id].SourceUses; uses != "" {
				pending = append(pending, uses)
			}
		}
	}
	return ctx.Err()
}
