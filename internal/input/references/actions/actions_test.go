package actions_test

import (
	"encoding/json"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/references/actions"
	"actionlint.kjanat.dev/v2/internal/model/repositoryid"
)

func knownRepositoryID(t *testing.T, value uint64) repositoryid.ID {
	t.Helper()
	id, err := repositoryid.Known(value)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestRawTagObjectPreservesRequestAndTerminalCommit(t *testing.T) {
	object, commit := strings.Repeat("a", 40), strings.Repeat("b", 40)
	request, err := actions.ParseReference("github.com", "example/action@"+object)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := actions.NewIdentity("github.com", "example/action", knownRepositoryID(t, 42), commit, "")
	if err != nil {
		t.Fatal(err)
	}
	selection, err := actions.NewSelection(request, identity, object, actions.ObjectRoute(), "observation")
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("name: example\nruns: {using: composite, steps: []}\n")
	manifest, err := actions.NewManifest(selection, content)
	if err != nil {
		t.Fatal(err)
	}
	clear(content)
	if manifest.Selection().Object() != object || manifest.Identity().Commit() != commit || manifest.Selection().Request().Ref() != object || !strings.HasPrefix(manifest.Content(), "name: example") {
		t.Fatal("request, object, content or ownership was lost")
	}
}

func TestInvalidSelectionCannotBecomeManifest(t *testing.T) {
	if _, err := actions.NewManifest(actions.Selection{}, []byte("name: example")); err == nil {
		t.Fatal("zero selection became a manifest")
	}
	request, err := actions.ParseReference("github.com", "example/action@"+strings.Repeat("a", 40))
	if err != nil {
		t.Fatal(err)
	}
	identity, err := actions.NewIdentity("github.com", "example/action", knownRepositoryID(t, 42), strings.Repeat("c", 40), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := actions.NewSelection(request, identity, strings.Repeat("b", 40), actions.ObjectRoute(), ""); err == nil {
		t.Fatal("a raw object request selected another original object")
	}
}

func TestHeadSelectionKeepsSyntheticLookupKey(t *testing.T) {
	route, err := actions.RefRoute("HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if route.Reference() != "HEAD" || route.Object() {
		t.Fatal("HEAD lookup was normalized into another route")
	}
}

func TestIdentityWithoutRepositoryID(t *testing.T) {
	commit := strings.Repeat("d", 40)
	identity, err := actions.NewIdentity("github.com", "example/action", repositoryid.ID{}, commit, "")
	if err != nil {
		t.Fatal(err)
	}
	if !identity.RepositoryID().IsZero() || identity.Repository() != "example/action" || identity.Commit() != commit {
		t.Fatalf("identity = %+v", identity)
	}
	request, err := actions.ParseReference("github.com", "example/action@v1")
	if err != nil {
		t.Fatal(err)
	}
	route, err := actions.RefRoute("refs/tags/v1")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		id   repositoryid.ID
		want string
	}{
		{repositoryid.ID{}, `{"reference":"example/action@v1","state":"available","host":"github.com","repository":"example/action","commit":"` + commit + `","observed_at":"observation","manifest":"action.yml","selected_object":"` + commit + `","selected_ref":"refs/tags/v1"}`},
		{knownRepositoryID(t, 42), `{"reference":"example/action@v1","state":"available","host":"github.com","repository":"example/action","repository_id":42,"commit":"` + commit + `","observed_at":"observation","manifest":"action.yml","selected_object":"` + commit + `","selected_ref":"refs/tags/v1"}`},
	} {
		identity, err := actions.NewIdentity("github.com", "example/action", test.id, commit, "")
		if err != nil {
			t.Fatal(err)
		}
		selection, err := actions.NewSelection(request, identity, commit, route, "observation")
		if err != nil {
			t.Fatal(err)
		}
		manifest, err := actions.NewManifest(selection, []byte("name: example\n"))
		if err != nil {
			t.Fatal(err)
		}
		data, err := json.Marshal(actions.Coverage{Reference: "example/action@v1", Availability: actions.AvailableMetadata(manifest)}.Report())
		if err != nil || string(data) != test.want {
			t.Fatalf("report = %s, %v\nwant %s", data, err, test.want)
		}
	}
}
