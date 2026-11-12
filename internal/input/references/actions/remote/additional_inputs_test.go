package remote

import (
	"strings"
	"testing"

	actionfixtures "actionlint.kjanat.dev/v2/internal/development/tests/integration/actions/fixtures"

	"actionlint.kjanat.dev/v2/internal/input/references/actions"
	actionmeta "actionlint.kjanat.dev/v2/internal/model/actions"
	"actionlint.kjanat.dev/v2/internal/model/repositoryid"
)

func requestActionSource(t *testing.T) string {
	t.Helper()
	fixture, err := actionfixtures.ReadManifest("request-action.yml")
	if err != nil {
		t.Fatal(err)
	}
	_, content, found := strings.Cut(string(fixture), "\n---\n")
	if !found {
		t.Fatal("request-action fixture has no provenance header")
	}
	return content
}

func TestAdditionalInputsRequireReviewedManifest(t *testing.T) {
	content := requestActionSource(t)
	const commit = "3d6703deef41a82916a2e5ca87096f93035cef66"
	for _, test := range []struct {
		name, host, repository, commit, directory, filename, content string
		repositoryID                                                 uint64
		unknownID                                                    bool
		additional                                                   bool
		invalid                                                      bool
	}{
		{name: "reviewed", additional: true},
		{name: "reviewed under another name", repository: "renamed/request-action", additional: true},
		{name: "reviewed without repository ID", unknownID: true, additional: true},
		{name: "reviewed without repository ID in another case", repository: "Octokit/Request-Action", unknownID: true, additional: true},
		{name: "another host", host: "other.example"},
		{name: "another repository", repositoryID: 42},
		{name: "another repository without repository ID", repository: "other/request-action", unknownID: true},
		{name: "another commit", commit: strings.Repeat("a", 40)},
		{name: "another directory", directory: "sub"},
		{name: "another filename", filename: "action.yaml"},
		{name: "changed bytes", content: content + "# changed\n"},
		{name: "invalid manifest", content: "name: [", invalid: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			host, repository, revision, filename, metadata, repositoryID := test.host, test.repository, test.commit, test.filename, test.content, test.repositoryID
			if host == "" {
				host = "github.com"
			}
			if repository == "" {
				repository = "octokit/request-action"
			}
			if revision == "" {
				revision = commit
			}
			if filename == "" {
				filename = "action.yml"
			}
			if metadata == "" {
				metadata = content
			}
			if repositoryID == 0 {
				repositoryID = 213507602
			}
			var id repositoryid.ID
			if !test.unknownID {
				known, err := repositoryid.Known(repositoryID)
				if err != nil {
					t.Fatal(err)
				}
				id = known
			}
			identity, err := actions.NewIdentity(host, repository, id, revision, test.directory)
			if err != nil {
				t.Fatal(err)
			}
			location := repository
			if test.directory != "" {
				location += "/" + test.directory
			}
			request, err := actions.ParseReference(host, location+"@"+revision)
			if err != nil {
				t.Fatal(err)
			}
			selection, err := actions.NewSelection(request, identity, revision, actions.ObjectRoute(), "")
			if err != nil {
				t.Fatal(err)
			}
			manifest, err := actions.NewManifestFile(selection, filename, []byte(metadata))
			if err != nil {
				t.Fatal(err)
			}
			result := inspected(manifest, actionmeta.InspectDefinition([]byte(metadata)))
			if test.invalid {
				if result.Availability().State() != actions.MetadataInvalid || result.Definition().Present() {
					t.Fatal("invalid manifest acquired call-site facts")
				}
				return
			}
			if result.Availability().State() != actions.MetadataAvailable || result.Definition().AllowsAdditionalInputs() != test.additional {
				t.Fatalf("allows additional inputs=%t availability=%s", result.Definition().AllowsAdditionalInputs(), result.Availability().State())
			}
		})
	}
}
