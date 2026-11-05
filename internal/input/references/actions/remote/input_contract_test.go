package remote

import (
	"strings"
	"testing"

	actionfixtures "actionlint.kjanat.dev/v2/internal/development/tests/integration/actions/fixtures"

	"actionlint.kjanat.dev/v2/internal/input/references/actions"
	actionmeta "actionlint.kjanat.dev/v2/internal/model/actions"
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

func TestAdditionalInputContractRequiresReviewedManifest(t *testing.T) {
	content := requestActionSource(t)
	const commit = "3d6703deef41a82916a2e5ca87096f93035cef66"
	for _, test := range []struct {
		name, host, commit, directory, filename, content string
		repositoryID                                     uint64
		additional                                       bool
		invalid                                          bool
	}{
		{name: "reviewed", additional: true},
		{name: "another host", host: "other.example"},
		{name: "another repository", repositoryID: 42},
		{name: "another commit", commit: strings.Repeat("a", 40)},
		{name: "another directory", directory: "sub"},
		{name: "another filename", filename: "action.yaml"},
		{name: "changed bytes", content: content + "# changed\n"},
		{name: "invalid manifest", content: "name: [", invalid: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			host, revision, filename, metadata, repositoryID := test.host, test.commit, test.filename, test.content, test.repositoryID
			if host == "" {
				host = "github.com"
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
			identity, err := actions.NewIdentity(host, "octokit/request-action", repositoryID, revision, test.directory)
			if err != nil {
				t.Fatal(err)
			}
			location := "octokit/request-action"
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
				t.Fatalf("additional-input contract=%v availability=%s", result.Definition().AllowsAdditionalInputs(), result.Availability().State())
			}
		})
	}
}
