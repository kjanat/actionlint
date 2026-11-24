package remote

import (
	"strings"

	"actionlint.kjanat.dev/v2/internal/base/sha256hex"
	"actionlint.kjanat.dev/v2/internal/input/references/actions"
	"actionlint.kjanat.dev/v2/internal/model/actionmeta"
)

func allowReviewedAdditionalInputs(manifest actions.Manifest, definition actionmeta.Definition) actionmeta.Definition {
	identity := manifest.Identity()
	repositoryMatches := strings.EqualFold(identity.Repository(), "octokit/request-action")
	if id, known := identity.RepositoryID().Value(); known {
		repositoryMatches = id == 213507602
	}
	// This implementation forwards every INPUT_* variable beyond route to
	// Octokit. The declared route input is still required.
	// https://github.com/octokit/request-action/blob/3d6703deef41a82916a2e5ca87096f93035cef66/index.js#L14-L26
	// https://github.com/octokit/request-action/blob/3d6703deef41a82916a2e5ca87096f93035cef66/index.js#L61-L73
	// https://github.com/octokit/request-action/blob/3d6703deef41a82916a2e5ca87096f93035cef66/action.yml
	if identity.Host() == "github.com" && repositoryMatches &&
		identity.Commit() == "3d6703deef41a82916a2e5ca87096f93035cef66" &&
		identity.Directory() == "" && manifest.Filename() == "action.yml" &&
		sha256hex.Digest([]byte(manifest.Content())) == "bb286e504a0e382a926ee922bd1cd2dd15d390b60220cc9bf97e442db65ebd80" {
		return definition.WithAdditionalInputs()
	}
	return definition
}
