package local

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path"
	"strings"
	"sync"

	actionmeta "actionlint.kjanat.dev/v2/internal/action"
	"actionlint.kjanat.dev/v2/internal/action/reference"
	"actionlint.kjanat.dev/v2/internal/documentparse"
	"actionlint.kjanat.dev/v2/internal/mapping"
	"actionlint.kjanat.dev/v2/internal/source/capture"
	workflowdecl "actionlint.kjanat.dev/v2/internal/workflow/declaration"
	"actionlint.kjanat.dev/v2/internal/workflow/execution"
	"actionlint.kjanat.dev/v2/internal/workflow/references"
	runnerlang "actionlint.kjanat.dev/v2/internal/workflow/runner"
	"actionlint.kjanat.dev/v2/internal/workflow/value"
)

// MetadataLookup supplies metadata to checks without exposing cache or resolver state.
type MetadataLookup interface {
	FindMetadata(string) (*actionmeta.Metadata, bool, error)
}

// MetadataDocumentLookup supplies metadata and its captured parse provenance
// together, including the source of a failed parse.
type MetadataDocumentLookup interface {
	FindMetadataDocument(string) (*actionmeta.Metadata, bool, SourceDocument, error)
}

// Resolver interprets local action references in one workflow's execution scope.
// Metadata and consulted sources are shared with composite children. Checkout
// and environment traversal state belongs to each resolver frame.
type Resolver struct {
	runners              *runnerlang.Knowledge
	base                 *Cache
	mu                   sync.RWMutex
	sourceMappings       map[string]string
	documents            *resolverDocuments
	activeChild          *Resolver
	branch               *compositeBranch
	retired              bool
	checkout             execution.CheckoutPlacement
	platform             runnerlang.Platform
	caseInsensitive      bool
	checkoutEnvUnknown   bool
	persistentEnvUnknown bool
	shellEnvUnknown      bool
	checkoutShell        execution.Shell
}

// resolverDocuments owns the consulted-source map and its synchronization.
// Forks share this pointer, never a copied mutex protecting the same map.
type resolverDocuments struct {
	mu   sync.RWMutex
	used map[string]SourceDocument
}

// SourceDocument carries immutable source bytes and the parse record that owns
// their canonical references, independently of metadata lookup success.
type SourceDocument struct {
	path     string
	address  string
	captured capture.Snapshot
	document documentparse.Document
}

func (d SourceDocument) Path() string { return d.path }

// Address is the absolute alias passed to the capture owner, separate from the
// caller's metadata display spelling.
func (d SourceDocument) Address() string { return d.address }

func (d SourceDocument) Content() []byte { return d.captured.Bytes() }

func (d SourceDocument) Capture() capture.Snapshot { return d.captured }

func (d SourceDocument) Document() documentparse.Document { return d.document }

// NewResolver starts a document scope over shared repository metadata and takes
// its own immutable copy of the source mappings.
func NewResolver(cache *Cache, mappings map[string]string) *Resolver {
	return &Resolver{base: cache, sourceMappings: maps.Clone(mappings), documents: &resolverDocuments{used: make(map[string]SourceDocument)}}
}

// SetRunners installs the workflow's runner knowledge before the resolver is
// used; composite children inherit it.
func (c *Resolver) SetRunners(runners *runnerlang.Knowledge) { c.runners = runners }

func (c *Resolver) documentOwner() *resolverDocuments {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.documents == nil {
		c.documents = &resolverDocuments{used: make(map[string]SourceDocument)}
	}
	return c.documents
}

// FindMetadata translates a reference using this invocation's checkout and source mappings.
func (c *Resolver) FindMetadata(spec string) (*actionmeta.Metadata, bool, error) {
	metadata, cached, _, err := c.FindMetadataDocument(spec)
	return metadata, cached, err
}

// FindMetadataDocument retains the capture belonging to this invocation's
// translated lookup without resolving or reading the metadata a second time.
func (c *Resolver) FindMetadataDocument(spec string) (*actionmeta.Metadata, bool, SourceDocument, error) {
	result, cached := c.LookupMetadata(spec)
	document, _ := c.SourceDocument(result)
	if c == nil {
		return result.Metadata(), cached, document, result.Err()
	}
	return result.Metadata(), cached, document, c.base.reportMetadataError(result)
}

// LookupMetadata looks up action metadata without emitting validation diagnostics.
func (c *Resolver) LookupMetadata(spec string) (MetadataResult, bool) {
	if c == nil || c.base == nil {
		return MetadataResult{status: MetadataLookupDisabled}, false
	}
	if local, ok := references.SelfRepositoryLocal(spec); ok {
		return c.findRepositoryMetadata(local, true)
	}
	local, ok := c.localSpec(spec)
	if !ok {
		status := MetadataNonLocal
		if strings.HasPrefix(spec, "./") {
			status = MetadataNotFetched
		}
		return MetadataResult{status: status}, false
	}
	certain := true
	if state := c.CheckoutState(); state.Present() {
		placement := state.Matching(spec)
		if !placement.Present() {
			placement = state
		}
		certain = placement.Details().Directory.Kind != execution.DirectoryUnknown
	}
	return c.findRepositoryMetadata(local, certain)
}

// Sources returns raw metadata consulted by this document, including parse failures.
func (c *Resolver) Sources() map[string][]byte {
	owner := c.documentOwner()
	owner.mu.RLock()
	defer owner.mu.RUnlock()
	cloned := make(map[string][]byte, len(owner.used))
	for path, document := range owner.used {
		cloned[path] = document.Content()
	}
	return cloned
}

// SourceDocuments returns provenance for every consulted local metadata file,
// including manifests whose semantic metadata lookup failed.
func (c *Resolver) SourceDocuments() map[string]SourceDocument {
	owner := c.documentOwner()
	owner.mu.RLock()
	defer owner.mu.RUnlock()
	return maps.Clone(owner.used)
}

// SourceDocument returns the cached parse record associated with a lookup.
func (c *Resolver) SourceDocument(result MetadataResult) (SourceDocument, bool) {
	if c == nil || c.base == nil {
		return SourceDocument{}, false
	}
	return c.base.SourceDocument(result)
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
	return UnresolvedError(fmt.Sprintf("local action %q has no action.yml, action.yaml, Dockerfile or dockerfile; check the path and ensure the action is available when linting", spec))
}

func (c *Resolver) findRepositoryMetadata(spec string, certain bool) (MetadataResult, bool) {
	original := spec
	mapped := false
	if normalized, err := reference.NormalizeLocalDirectory(spec); err == nil {
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
				return MetadataResult{status: MetadataMappingFailed, cause: MappingError{fmt.Errorf("source mapping for local action %q is ambiguous under case-insensitive lookup", original)}}, false
			}
		}
	}
	explicit, err := c.base.hasExplicitAction(spec)
	if err != nil {
		return MetadataResult{status: MetadataReadFailed, cause: err}, false
	}
	if c.caseInsensitive && c.base.root != "" && !c.base.customReader && !explicit {
		local, err := execution.CaseInsensitiveRepositoryPath(c.base.sourceRoot, spec, c.base.reader)
		if err != nil {
			_, ambiguous := errors.AsType[execution.AmbiguousPathError](err)
			if !errors.Is(err, os.ErrNotExist) && !ambiguous {
				return MetadataResult{status: MetadataReadFailed, cause: fmt.Errorf("resolve local action %q: %w", original, err)}, false
			}
			if mapped {
				if errors.Is(err, os.ErrNotExist) {
					return MetadataResult{status: MetadataMappingFailed, cause: missingMappedLocalAction(original, spec)}, false
				}
				return MetadataResult{status: MetadataMappingFailed, cause: MappingError{fmt.Errorf("source mapping for local action %q: cannot resolve source %q under case-insensitive lookup: %w", original, spec, err)}}, false
			}
			if !certain && errors.Is(err, os.ErrNotExist) {
				return MetadataResult{status: MetadataPlacementUncertain}, false
			}
			return MetadataResult{status: MetadataMissingManifest, cause: UnresolvedError(fmt.Sprintf("cannot resolve local action %q: its path is missing or ambiguous under case-insensitive lookup", spec))}, false
		}
		spec = local
	}
	if mapped && c.base.root != "" && !c.base.customReader && !explicit {
		if err := checkMappedSource(c.base.sourceRoot, spec); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return MetadataResult{status: MetadataMappingFailed, cause: missingMappedLocalAction(original, spec)}, false
			}
			return MetadataResult{status: MetadataMappingFailed, cause: MappingError{fmt.Errorf("source mapping for local action %q: %w", original, err)}}, false
		}
	}
	result, cached := c.base.LookupMetadata(spec)
	if _, missing := errors.AsType[UnresolvedError](result.Err()); missing {
		if mapped {
			result.status, result.cause = MetadataMappingFailed, missingMappedLocalAction(original, spec)
		} else if !certain {
			result.status, result.cause = MetadataPlacementUncertain, nil
		}
	}
	if mapped && result.metadata != nil {
		mappedMetadata := *result.metadata
		mappedMetadata.SourceMapped = true
		result.metadata = &mappedMetadata
	}
	c.base.mu.RLock()
	source, exists := c.base.sources[spec]
	c.base.mu.RUnlock()
	if exists {
		owner := c.documentOwner()
		owner.mu.Lock()
		owner.used[source.path] = SourceDocument(source)
		owner.mu.Unlock()
	}
	return result, cached
}

func missingMappedLocalAction(alias, source string) error {
	return UnresolvedError(fmt.Sprintf("source mapping for local action %q: %s", alias, missingLocalActionMetadata(source)))
}

// The workflow-scoped view translates runner workspace paths before consulting
// the shared repository cache. Its placement never leaks between jobs or files.
func (c *Resolver) currentCheckout() execution.Directory {
	return c.CheckoutState().Details().Directory
}

// CheckoutState returns an immutable placement value for this invocation.
func (c *Resolver) CheckoutState() execution.CheckoutPlacement {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.checkout
}

func (c *Resolver) setCheckout(checkout execution.Directory) {
	var placement execution.CheckoutPlacement
	if checkout.Kind != execution.DirectoryUnspecified {
		placement = execution.NewCheckoutPlacement(execution.Checkout{Directory: checkout}, execution.CheckoutPlacement{})
	}
	c.restoreCheckout(placement)
}

func (c *Resolver) restoreCheckout(placement execution.CheckoutPlacement) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.checkout = placement
}

func (c *Resolver) localSpec(spec string) (string, bool) {
	if !strings.HasPrefix(spec, "./") {
		return "", false
	}
	state := c.CheckoutState()
	if !state.Present() {
		return spec, true
	}
	placement := state.Matching(spec)
	if !placement.Present() {
		return spec, state.Details().Directory.Kind == execution.DirectoryUnknown
	}
	details := placement.Details()
	checkout := details.Directory
	if details.Foreign || details.Redirected {
		return "", false
	}
	if checkout.Kind == execution.DirectoryUnknown {
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
func (c *Resolver) ObserveCheckout(step workflowdecl.StepView) {
	enabled, known := execution.Condition(step.Condition())
	if known && !enabled {
		return
	}
	if _, parallel := step.Parallel(); parallel {
		c.setCheckout(execution.Directory{Kind: execution.DirectoryUnknown})
		return
	}
	action, ok := step.Action()
	if !ok || !action.Uses().Present() {
		return
	}
	name, _, versioned := strings.Cut(action.Uses().Value(), "@")
	if !versioned || !strings.EqualFold(name, "actions/checkout") {
		return
	}
	if c.checkoutEnvUnknown || c.persistentEnvUnknown || execution.CheckoutEnvironmentUnknown(step.Env().Mapping(), c.platform) {
		// Git may write outside the requested path or execute a wrapper. Literal
		// metadata fallback is not evidence of the resulting repository contents.
		c.restoreCheckout(execution.NewCheckoutPlacement(execution.Checkout{Directory: execution.Directory{Kind: execution.DirectoryUnknown}, Redirected: true}, execution.CheckoutPlacement{}))
		return
	}
	checkoutPath, pathKnown := checkoutInput(action.ResolvedInputs(), "path")
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
	certain := known && !execution.BoolMayBeTrue(step.ContinueOnError().Semantic()) && !execution.BoolMayBeTrue(step.Background().Semantic())
	if !certain {
		checkout.Kind = execution.DirectoryUnknown
	}
	foreign, self := false, true
	for _, key := range []string{"repository", "ref", "sparse-checkout", "github-server-url"} {
		item, inputKnown := checkoutInput(action.ResolvedInputs(), key)
		if !inputKnown || item != "" {
			checkout.Kind = execution.DirectoryUnknown
			self = false
		}
		if certain && (key == "repository" || key == "github-server-url" || key == "ref") && inputKnown && item != "" {
			// A specified revision need not contain the analyzed working-tree
			// metadata. No revision identity is available to prove equivalence.
			foreign = true
		}
	}
	previous := c.CheckoutState()
	if self && !known && !execution.BoolMayBeTrue(step.ContinueOnError().Semantic()) && !execution.BoolMayBeTrue(step.Background().Semantic()) && !execution.CanRunAfterFailure(step.Condition()) {
		// Skipping or successfully refreshing the same self checkout both
		// preserve its placement. A failed refresh cannot reach ordinary steps.
		if previous.SameSelfCheckout(checkout.Path) {
			checkout.Kind = execution.DirectoryKnown
		}
	}
	c.restoreCheckout(execution.NewCheckoutPlacement(execution.Checkout{Directory: checkout, CaseInsensitive: c.caseInsensitive, Foreign: foreign}, previous))
}

func checkoutInput(inputs mapping.Mapping, name string) (string, bool) {
	item := inputs.Lookup(name)
	if item.State() == value.Missing {
		return "", true
	}
	text, known := item.ScalarString()
	return strings.TrimSpace(text), known
}
