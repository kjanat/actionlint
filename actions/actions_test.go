package actions_test

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/actions"
)

func TestRawTagObjectPreservesRequestAndTerminalCommit(t *testing.T) {
	object, commit := strings.Repeat("a", 40), strings.Repeat("b", 40)
	request, err := actions.ParseReference("github.com", "example/action@"+object)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := actions.NewIdentity("github.com", "example/action", 42, commit, "")
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
	identity, err := actions.NewIdentity("github.com", "example/action", 42, strings.Repeat("c", 40), "")
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
