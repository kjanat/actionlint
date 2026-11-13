package workflows

import (
	"context"
	"errors"
	"maps"
	"os"
	"slices"

	"actionlint.kjanat.dev/v2/internal/model/calls/references"
	"actionlint.kjanat.dev/v2/internal/model/parsing"
	"golang.org/x/sync/errgroup"
)

const concurrentRemoteLoads = 8

// Prepare acquires metadata for direct workflow calls and nested local calls.
// Invalid YAML and missing manifests are cached for caller diagnostics.
// Other read failures return an error to the caller.
func (c *Cache) Prepare(ctx context.Context, snapshot parsing.SourceSnapshot) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if c == nil || snapshot.Kind() != parsing.KindWorkflow {
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

// LoadCalls fetches remote workflow files and reads local workflow dependencies.
// Rule callbacks use Resolved instead, which never fetches or reads files.
func (c *Cache) LoadCalls(ctx context.Context, referencesToLoad []string) error {
	if c == nil {
		return nil
	}
	var prefetch errgroup.Group
	prefetch.SetLimit(concurrentRemoteLoads)
	for _, spec := range referencesToLoad {
		if IsRemoteReference(spec) {
			prefetch.Go(func() error {
				_, err := c.remote.Load(ctx, spec)
				return err
			})
		}
	}
	if err := prefetch.Wait(); err != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	pending := slices.Clone(referencesToLoad)
	seen := make(map[string]bool)
	for index := 0; index < len(pending); index++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if IsRemoteReference(pending[index]) {
			if _, err := c.remote.Load(ctx, pending[index]); err != nil {
				if failure, ok := errors.AsType[metadataError](err); ok && failure.kind == failureParse {
					continue
				}
				return err
			}
			continue
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
