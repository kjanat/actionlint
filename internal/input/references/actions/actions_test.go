package actions_test

import (
	"encoding/json"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/references/actions"
	"actionlint.kjanat.dev/v2/internal/model/repository"
)

func TestParseReference(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ spec, host, repository, directory, ref, err string }{
		{"o/r@v", "github.com", "o/r", "", "v", ""},
		{"o/r/p/q@v", "github.com", "o/r", "p/q", "v", ""},
		{"O/R/p@V", "github.com", "O/R", "p", "V", ""},
		{"o/r@v@w", "", "", "", "", `action reference contains more than one "@"`},
		{"o//r@v", "github.com", "o/r", "", "v", ""},
		{`o\r\p@v`, "github.com", "o/r", "p", "v", ""},
		{"o/r", "", "", "", "", "action ref is missing"},
		{"o/r@", "", "", "", "", "action ref is missing"},
		{"./a", "", "", "", "", "action ref is missing"},
		{"./a@v", "", "", "", "", "action needs an owner and repository"},
		{"./o/r@v", "", "", "", "", "action needs an owner and repository"},
		{"$/a@v", "", "", "", "", "action needs an owner and repository"},
		{"o/@v", "", "", "", "", "action needs an owner and repository"},
		{"@v", "", "", "", "", "action needs an owner and repository"},
		{"o/r/p/@v", "github.com", "o/r", "p", "v", ""},
		{"o/r//p@v", "github.com", "o/r", "p", "v", ""},
		{"o/r/../p@v", "", "", "", "", "invalid action directory \"../p\""},
		{"o/r/./p@v", "", "", "", "", "invalid action directory \"./p\""},
		{"o/r/\\p@v", "github.com", "o/r", "p", "v", ""},
	} {
		t.Run(tc.spec, func(t *testing.T) {
			t.Parallel()
			ref, err := actions.ParseReference("GitHub.com", tc.spec)
			message := ""
			if err != nil {
				message = err.Error()
			}
			if message != tc.err {
				t.Fatalf("ParseReference(%q) error = %q, want %q", tc.spec, message, tc.err)
			}
			if ref.Host() != tc.host || ref.Repository() != tc.repository || ref.Directory() != tc.directory || ref.Ref() != tc.ref {
				t.Fatalf("ParseReference(%q) = %q %q %q %q, want %q %q %q %q", tc.spec, ref.Host(), ref.Repository(), ref.Directory(), ref.Ref(), tc.host, tc.repository, tc.directory, tc.ref)
			}
		})
	}
}

func knownRepositoryID(t *testing.T, value uint64) repository.ID {
	t.Helper()
	id, err := repository.KnownID(value)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestRawTagObjectPreservesRequestAndTerminalCommit(t *testing.T) {
	t.Parallel()
	object, commit := strings.Repeat("a", 40), strings.Repeat("b", 40)
	request, err := actions.ParseReference("github.com", "example/action@"+object)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := actions.NewIdentity("github.com", "example/action", knownRepositoryID(t, 42), commit, "")
	if err != nil {
		t.Fatal(err)
	}
	selection, err := actions.NewReferenceSelection(request, identity, object, actions.ObjectRoute(), "observation")
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
		t.Fatal("manifest lost its request, object or content after the caller cleared content")
	}
}

func TestInvalidSelectionCannotBecomeManifest(t *testing.T) {
	t.Parallel()
	if _, err := actions.NewManifest(actions.ReferenceSelection{}, []byte("name: example")); err == nil {
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
	if _, err := actions.NewReferenceSelection(request, identity, strings.Repeat("b", 40), actions.ObjectRoute(), ""); err == nil {
		t.Fatal("a raw object request selected another original object")
	}
}

func TestHeadSelectionKeepsSyntheticLookupKey(t *testing.T) {
	t.Parallel()
	route, err := actions.RefRoute("HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if route.Reference() != "HEAD" || route.Object() {
		t.Fatal("HEAD lookup was normalized into another route")
	}
}

func TestIdentityWithoutRepositoryID(t *testing.T) {
	t.Parallel()
	commit := strings.Repeat("d", 40)
	identity, err := actions.NewIdentity("github.com", "example/action", repository.ID{}, commit, "")
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
		id   repository.ID
		want string
	}{
		{repository.ID{}, `{"reference":"example/action@v1","state":"available","host":"github.com","repository":"example/action","commit":"` + commit + `","observed_at":"observation","manifest":"action.yml","selected_object":"` + commit + `","selected_ref":"refs/tags/v1"}`},
		{knownRepositoryID(t, 42), `{"reference":"example/action@v1","state":"available","host":"github.com","repository":"example/action","repository_id":42,"commit":"` + commit + `","observed_at":"observation","manifest":"action.yml","selected_object":"` + commit + `","selected_ref":"refs/tags/v1"}`},
	} {
		identity, err := actions.NewIdentity("github.com", "example/action", test.id, commit, "")
		if err != nil {
			t.Fatal(err)
		}
		selection, err := actions.NewReferenceSelection(request, identity, commit, route, "observation")
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
