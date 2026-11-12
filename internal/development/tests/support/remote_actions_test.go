package testutil

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/model/repositoryid"
)

func TestFixtureRepositoryIDIsOptional(t *testing.T) {
	t.Parallel()
	id, err := fixtureRepositoryID(map[string]string{})
	if err != nil || !id.IsZero() {
		t.Fatalf("absent header = %+v, %v", id, err)
	}
	want, err := repositoryid.Known(42)
	if err != nil {
		t.Fatal(err)
	}
	if id, err := fixtureRepositoryID(map[string]string{"repository-id": "42"}); err != nil || id != want {
		t.Fatalf("present header = %+v, %v", id, err)
	}
	for _, text := range []string{"0", "-1", "forty-two", ""} {
		if _, err := fixtureRepositoryID(map[string]string{"repository-id": text}); err == nil {
			t.Errorf("header %q was accepted", text)
		}
	}
}
