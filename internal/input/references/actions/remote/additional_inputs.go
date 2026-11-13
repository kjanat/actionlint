package remote

import (
	"strings"

	"actionlint.kjanat.dev/v2/internal/input/references/actions"
	sourceid "actionlint.kjanat.dev/v2/internal/input/source/identity"
	actionmeta "actionlint.kjanat.dev/v2/internal/model/actions"
)

// allowReviewedAdditionalInputs permits undeclared input names for an identified implementation.
// The commit and manifest digest must match the reviewed source. A known
// repository ID must match too; otherwise the repository name must.
func allowReviewedAdditionalInputs(manifest actions.Manifest, definition actionmeta.Definition) actionmeta.Definition {
	identity := manifest.Identity()
	repositoryMatches := strings.EqualFold(identity.Repository(), "octokit/request-action")
	if id, known := identity.RepositoryID().Value(); known {
		repositoryMatches = id == 213507602
	}
	// This implementation collects every INPUT_* variable and forwards the
	// parameters beyond route to Octokit; it does not restrict them to inputs
	// declared in action.yml. The declared route input is still required.
	// https://github.com/octokit/request-action/blob/3d6703deef41a82916a2e5ca87096f93035cef66/index.js#L14-L26
	// https://github.com/octokit/request-action/blob/3d6703deef41a82916a2e5ca87096f93035cef66/index.js#L61-L73
	// https://github.com/octokit/request-action/blob/3d6703deef41a82916a2e5ca87096f93035cef66/action.yml
	if identity.Host() == "github.com" && repositoryMatches &&
		identity.Commit() == "3d6703deef41a82916a2e5ca87096f93035cef66" &&
		identity.Directory() == "" && manifest.Filename() == "action.yml" &&
		sourceid.Digest([]byte(manifest.Content())) == "bb286e504a0e382a926ee922bd1cd2dd15d390b60220cc9bf97e442db65ebd80" {
		return definition.WithAdditionalInputs()
	}
	return definition
}
