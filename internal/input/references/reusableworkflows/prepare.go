package reusableworkflows

import (
	"context"
	"errors"
	"maps"
	"os"
	"slices"

	"actionlint.kjanat.dev/v2/internal/model/calls/callreferences"
	"actionlint.kjanat.dev/v2/internal/model/parsing"
	"golang.org/x/sync/errgroup"
)

const concurrentRemoteLoads = 8

// Prepare caches invalid YAML and missing manifests for caller diagnostics and returns other read failures.
func (c *MetadataCache) Prepare(ctx context.Context, snapshot parsing.SourceSnapshot) error {
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
	_, dependencies := snapshot.BoundDocument().Declarations()
	for _, dependency := range dependencies {
		if dependency.Kind == "workflow" {
			pending = append(pending, dependency.Uses)
		}
	}
	return c.LoadCalls(ctx, pending)
}

// LoadCalls does all file reads, and Resolved in rule callbacks reads none.
func (c *MetadataCache) LoadCalls(ctx context.Context, referencesToLoad []string) error {
	if c == nil {
		return nil
	}
	var prefetch errgroup.Group
	prefetch.SetLimit(concurrentRemoteLoads)
	for _, spec := range referencesToLoad {
		if IsRemoteReference(spec) {
			prefetch.Go(func() error {
				_, err := c.remote.LoadWorkflowMetadata(ctx, spec)
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
			if _, err := c.remote.LoadWorkflowMetadata(ctx, pending[index]); err != nil {
				if failure, ok := errors.AsType[metadataError](err); ok && failure.kind == failureParse {
					continue
				}
				return err
			}
			continue
		}
		spec, local := callreferences.LocalWorkflow(pending[index])
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
