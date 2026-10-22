package local

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path"
	"strings"
	"sync"

	actionmeta "actionlint.kjanat.dev/internal/action"
	"actionlint.kjanat.dev/internal/localaction"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
	"actionlint.kjanat.dev/internal/workflow/execution"
	"actionlint.kjanat.dev/internal/workflow/references"
	runnerlang "actionlint.kjanat.dev/internal/workflow/runner"
)

// MetadataLookup supplies metadata to checks without exposing cache or resolver state.
type MetadataLookup interface {
	FindMetadata(string) (*actionmeta.Metadata, bool, error)
}

// Resolver interprets local action references in one workflow's execution scope.
// Metadata is shared through base; checkout, mappings and environment state stay local.
type Resolver struct {
	base                 *Cache
	mu                   sync.RWMutex
	sourceMappings       map[string]string
	usedSources          map[string][]byte
	checkout             *execution.CheckoutPlacement
	platform             runnerlang.Platform
	caseInsensitive      bool
	checkoutEnvUnknown   bool
	persistentEnvUnknown bool
	shellEnvUnknown      bool
	checkoutShell        execution.Shell
}

// NewResolver starts a document scope over shared repository metadata.
func NewResolver(cache *Cache, mappings map[string]string) *Resolver {
	return &Resolver{base: cache, sourceMappings: mappings, usedSources: make(map[string][]byte)}
}

// FindMetadata translates a reference using this invocation's checkout and source mappings.
func (c *Resolver) FindMetadata(spec string) (*actionmeta.Metadata, bool, error) {
	if c == nil || c.base == nil {
		return nil, false, nil
	}
	if local, ok := references.SelfRepositoryLocal(spec); ok {
		return c.findRepositoryMetadata(local, true)
	}
	local, ok := c.localSpec(spec)
	if !ok {
		return nil, false, nil
	}
	certain := true
	if state := c.CheckoutState(); state != nil {
		placement := state.Matching(spec)
		if placement == nil {
			placement = state
		}
		certain = placement.Directory.Kind != execution.DirectoryUnknown
	}
	return c.findRepositoryMetadata(local, certain)
}

// Sources returns raw metadata consulted by this document, including parse failures.
func (c *Resolver) Sources() map[string][]byte {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return maps.Clone(c.usedSources)
}

// ApplyCheckout adapts shared script paths to this invocation's checkout placement.
func (c *Resolver) ApplyCheckout(paths execution.Paths) execution.Paths {
	checkout := c.currentCheckout()
	paths.Checkout, paths.CheckoutUnknown = checkout.Path, checkout.Kind == execution.DirectoryUnknown
	paths.Placements = c.CheckoutState()
	paths.CaseInsensitive = c.caseInsensitive
	return paths
}

// Platform returns the runner platform inferred for this workflow job.
func (c *Resolver) Platform() runnerlang.Platform { return c.platform }

// InvalidateCheckout discards assumptions after opaque or concurrent execution.
func (c *Resolver) InvalidateCheckout() {
	c.setCheckout(execution.Directory{Kind: execution.DirectoryUnknown})
}

// UnresolvedError identifies a missing or ambiguous local action reference.
type UnresolvedError string

func (e UnresolvedError) Error() string { return string(e) }

type MappingError struct{ Cause error }

func (e MappingError) Error() string { return e.Cause.Error() }
func (e MappingError) Unwrap() error { return e.Cause }

func missingLocalActionMetadata(spec string) error {
	return UnresolvedError(fmt.Sprintf("local action %q has no action.yml or action.yaml; check the path and ensure the action is available when linting", spec))
}

func (c *Resolver) findRepositoryMetadata(spec string, certain bool) (*actionmeta.Metadata, bool, error) {
	original := spec
	mapped := false
	if normalized, err := localaction.Normalize(spec); err == nil {
		if source, exists := c.sourceMappings[normalized]; exists {
			spec, mapped = source, true
		}
		if c.caseInsensitive && execution.ASCIIPath(normalized) {
			matches := 0
			for alias, source := range c.sourceMappings {
				if execution.ASCIIPath(alias) && strings.EqualFold(alias, normalized) {
					matches++
					spec, mapped = source, true
				}
			}
			if matches > 1 {
				return nil, false, MappingError{fmt.Errorf("source mapping for local action %q is ambiguous under case-insensitive lookup", original)}
			}
		}
	}
	explicit := c.base.hasExplicitAction(spec)
	if c.caseInsensitive && c.base.root != "" && !c.base.customReader && !explicit {
		local, err := execution.CaseInsensitiveRepositoryPath(c.base.root, spec, c.base.reader)
		if err != nil {
			if mapped {
				if errors.Is(err, os.ErrNotExist) {
					return nil, false, missingMappedLocalAction(original, spec)
				}
				return nil, false, MappingError{fmt.Errorf("source mapping for local action %q: cannot resolve source %q under case-insensitive lookup", original, spec)}
			}
			if !certain && errors.Is(err, os.ErrNotExist) {
				return nil, false, nil
			}
			return nil, false, UnresolvedError(fmt.Sprintf("cannot resolve local action %q: its path is missing or ambiguous under case-insensitive lookup", spec))
		}
		spec = local
	}
	if mapped && c.base.root != "" && !c.base.customReader && !explicit {
		if err := localaction.CheckSource(c.base.root, spec); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil, false, missingMappedLocalAction(original, spec)
			}
			return nil, false, MappingError{fmt.Errorf("source mapping for local action %q: %w", original, err)}
		}
	}
	metadata, cached, err := c.base.FindMetadata(spec)
	if _, missing := errors.AsType[UnresolvedError](err); missing {
		if mapped {
			err = missingMappedLocalAction(original, spec)
		} else if !certain {
			err = nil
		}
	}
	if mapped && metadata != nil {
		mappedMetadata := *metadata
		mappedMetadata.SourceMapped = true
		metadata = &mappedMetadata
	}
	if c.usedSources != nil {
		c.base.mu.RLock()
		source, exists := c.base.sources[spec]
		c.base.mu.RUnlock()
		if exists {
			c.mu.Lock()
			c.usedSources[source.path] = source.content
			c.mu.Unlock()
		}
	}
	return metadata, cached, err
}

func missingMappedLocalAction(alias, source string) error {
	return UnresolvedError(fmt.Sprintf("source mapping for local action %q: %s", alias, missingLocalActionMetadata(source)))
}

// The workflow-scoped view translates runner workspace paths before consulting
// the shared repository cache. Its placement never leaks between jobs or files.
func (c *Resolver) currentCheckout() execution.Directory {
	if placement := c.CheckoutState(); placement != nil {
		return placement.Directory
	}
	return execution.Directory{}
}

// CheckoutState returns the immutable placement chain for this invocation.
func (c *Resolver) CheckoutState() *execution.CheckoutPlacement {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.checkout
}

func (c *Resolver) setCheckout(checkout execution.Directory) {
	var placement *execution.CheckoutPlacement
	if checkout.Kind != execution.DirectoryUnspecified {
		placement = &execution.CheckoutPlacement{Directory: checkout}
	}
	c.restoreCheckout(placement)
}

func (c *Resolver) restoreCheckout(placement *execution.CheckoutPlacement) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.checkout = placement
}

func (c *Resolver) localSpec(spec string) (string, bool) {
	if !strings.HasPrefix(spec, "./") {
		return "", false
	}
	state := c.CheckoutState()
	if state == nil {
		return spec, true
	}
	placement := state.Matching(spec)
	if placement == nil {
		return spec, state.Directory.Kind == execution.DirectoryUnknown
	}
	checkout := placement.Directory
	if placement.Foreign || placement.Redirected {
		return "", false
	}
	if checkout.Kind == execution.DirectoryUnknown {
		// Retain best-effort validation of literal local metadata. Script rules
		// still receive the unknown placement and cannot assume its runtime paths.
		return spec, true
	}
	if checkout.Path == "" || checkout.Path == "." {
		return spec, true
	}
	if relative, ok := placement.Relative(spec); ok {
		return "./" + relative, true
	}
	return "", false
}

// ObserveCheckout updates action resolution after a checkout step.
func (c *Resolver) ObserveCheckout(step *workflowlang.Step) {
	enabled, known := execution.Condition(step.If)
	if known && !enabled {
		return
	}
	if _, parallel := step.Exec.(*workflowlang.ExecParallel); parallel {
		c.setCheckout(execution.Directory{Kind: execution.DirectoryUnknown})
		return
	}
	action, ok := step.Exec.(*workflowlang.ExecAction)
	if !ok || action.Uses == nil {
		return
	}
	name, _, versioned := strings.Cut(action.Uses.Value, "@")
	if !versioned || !strings.EqualFold(name, "actions/checkout") {
		return
	}
	if c.checkoutEnvUnknown || c.persistentEnvUnknown || execution.CheckoutEnvironmentUnknown(step.Env, c.platform) {
		// Git may write outside the requested path or execute a wrapper. Literal
		// metadata fallback is not evidence of the resulting repository contents.
		c.restoreCheckout(&execution.CheckoutPlacement{Directory: execution.Directory{Kind: execution.DirectoryUnknown}, Redirected: true})
		return
	}
	checkoutPath, pathKnown := execution.CheckoutInput(action, "path")
	checkoutPath, representable := execution.RunnerRelativePath(checkoutPath, c.platform)
	if !pathKnown || !representable {
		c.setCheckout(execution.Directory{Kind: execution.DirectoryUnknown})
		return
	}
	checkout := execution.Directory{Kind: execution.DirectoryKnown, Path: checkoutPath}
	if checkout.Path != "" {
		checkout.Path = path.Clean(checkout.Path)
		if checkout.Path == ".." || strings.HasPrefix(checkout.Path, "../") {
			c.setCheckout(execution.Directory{Kind: execution.DirectoryUnknown})
			return
		}
	}
	certain := known && !execution.BoolMayBeTrue(step.ContinueOnError) && !execution.BoolMayBeTrue(step.Background)
	if !certain {
		checkout.Kind = execution.DirectoryUnknown
	}
	foreign, self := false, true
	for _, key := range []string{"repository", "ref", "sparse-checkout", "github-server-url"} {
		value, inputKnown := execution.CheckoutInput(action, key)
		if !inputKnown || value != "" {
			checkout.Kind = execution.DirectoryUnknown
			self = false
		}
		if certain && (key == "repository" || key == "github-server-url" || key == "ref") && inputKnown && value != "" {
			// A specified revision need not contain the analyzed working-tree
			// metadata. No revision identity is available to prove equivalence.
			foreign = true
		}
	}
	previous := c.CheckoutState()
	if self && !known && !execution.BoolMayBeTrue(step.ContinueOnError) && !execution.BoolMayBeTrue(step.Background) && !execution.CanRunAfterFailure(step.If) {
		// Skipping or successfully refreshing the same self checkout both
		// preserve its placement. A failed refresh cannot reach ordinary steps.
		if previous.SameSelfCheckout(checkout.Path) {
			checkout.Kind = execution.DirectoryKnown
		}
	}
	c.restoreCheckout(&execution.CheckoutPlacement{Directory: checkout, Previous: previous, CaseInsensitive: c.caseInsensitive, Foreign: foreign})
}
