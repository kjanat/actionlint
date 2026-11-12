package composites

import (
	"errors"
	"strings"

	"actionlint.kjanat.dev/v2/internal/input/references/actions"
	workspaceactions "actionlint.kjanat.dev/v2/internal/input/references/actions/workspace"
	"actionlint.kjanat.dev/v2/internal/input/source"
	actionmeta "actionlint.kjanat.dev/v2/internal/model/actions"
	"actionlint.kjanat.dev/v2/internal/model/availability"
	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"
	"actionlint.kjanat.dev/v2/internal/model/parsing"
)

type remoteDocument struct {
	parsing.SourceSnapshot
	document parsing.Document
	identity actions.Identity
	valid    bool
}

func (d remoteDocument) Provenance() parsing.Provenance { return d.document.Provenance }

func (analysis *compositeAnalyzer) resolveRemoteAction(spec string) (*actionmeta.Metadata, string, invocation.Outcome, error) {
	result, err := analysis.remoteActions.Inspect(analysis.ctx, spec)
	if err != nil {
		return nil, "", invocation.Outcome{}, err
	}
	manifest, available := result.Manifest()
	if !available {
		reason := invocation.ReasonNotFetched
		if result.Availability().Reason() == availability.LookupDisabledReason {
			reason = invocation.ReasonLookupDisabled
		}
		outcome, err := invocation.Unavailable(reason, string(result.Availability().Reason()))
		return nil, "", outcome, err
	}
	if analysis.captures == nil {
		return nil, "", invocation.Outcome{}, errors.New("remote action analysis requires source captures")
	}
	address := manifest.URL()
	document, found := analysis.remoteDocuments[address]
	if !found {
		captured, err := analysis.captures.CaptureRemote(analysis.ctx, address, []byte(manifest.Content()), source.YAML)
		if err != nil {
			return nil, address, invocation.Outcome{}, err
		}
		parsed := parsing.Action(address, captured.Bytes())
		snapshot, err := analysis.sourceSnapshots.Document(captured, parsed)
		if err != nil {
			return nil, address, invocation.Outcome{}, err
		}
		document = remoteDocument{snapshot, parsed, manifest.Identity(), result.Availability().State() != actions.MetadataInvalid}
		analysis.remoteDocuments[address] = document
	}
	if !document.valid || !document.document.CanAnalyze() {
		outcome, err := invocation.Unresolved(invocation.ReasonParseFailed, "remote action manifest is invalid")
		return nil, address, outcome, err
	}
	metadata := document.document.Metadata
	view, available := document.ActionView()
	if !available {
		return nil, address, invocation.Outcome{}, errors.New("remote action source has no action view")
	}
	if !strings.EqualFold(view.Using(), "composite") {
		outcome, err := invocation.Opaque(invocation.ReasonNonComposite, view.Using())
		return metadata, address, outcome, err
	}
	outcome, err := invocation.Expanded(document.Reference())
	return metadata, address, outcome, err
}

func (analysis *compositeAnalyzer) repositorySource(identity actions.Identity) func(string) (workspaceactions.RepositoryDocument, error) {
	return func(local string) (workspaceactions.RepositoryDocument, error) {
		spec := identity.Repository() + "/" + strings.TrimPrefix(local, "./") + "@" + identity.Commit()
		_, address, _, err := analysis.resolveRemoteAction(spec)
		if err != nil {
			return workspaceactions.RepositoryDocument{}, err
		}
		if document, found := analysis.remoteDocuments[address]; found {
			return workspaceactions.RepositoryDocument{Parsed: document.document, Captured: document.Capture(), Valid: document.valid}, nil
		}
		return workspaceactions.RepositoryDocument{}, nil
	}
}
