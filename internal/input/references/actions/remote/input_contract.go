package remote

import (
	"actionlint.kjanat.dev/v2/internal/input/references/actions"
	sourceid "actionlint.kjanat.dev/v2/internal/input/source/identity"
	actionmeta "actionlint.kjanat.dev/v2/internal/model/actions"
)

// bindInputContract supplements a valid manifest with reviewed implementation
// facts. Matching raw metadata as well as immutable identity prevents facts from
// being applied to a different manifest supplied under the same identity.
func bindInputContract(manifest actions.Manifest, definition actionmeta.Definition) actionmeta.Definition {
	identity := manifest.Identity()
	// This implementation collects every INPUT_* variable and forwards the
	// parameters beyond route to Octokit; it does not restrict them to inputs
	// declared in action.yml. The declared route requirement remains checked.
	// https://github.com/octokit/request-action/blob/3d6703deef41a82916a2e5ca87096f93035cef66/index.js#L13-L24
	// https://github.com/octokit/request-action/blob/3d6703deef41a82916a2e5ca87096f93035cef66/index.js#L56-L67
	// https://github.com/octokit/request-action/blob/3d6703deef41a82916a2e5ca87096f93035cef66/action.yml
	if identity.Host() == "github.com" && identity.RepositoryID() == 213507602 &&
		identity.Commit() == "3d6703deef41a82916a2e5ca87096f93035cef66" &&
		identity.Directory() == "" && manifest.Filename() == "action.yml" &&
		sourceid.Digest([]byte(manifest.Content())) == "bb286e504a0e382a926ee922bd1cd2dd15d390b60220cc9bf97e442db65ebd80" {
		return definition.WithAdditionalInputs()
	}
	return definition
}
