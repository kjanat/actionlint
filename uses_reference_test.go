package actionlint

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestUsesReference(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want UsesReference
	}{
		{"actions/checkout@v7", RepositoryReference{Owner: "actions", Repo: "checkout", Ref: "v7", HostSource: "default"}},
		{"owner/repo/path@feature/name", RepositoryReference{Owner: "owner", Repo: "repo", Subpath: "path", Ref: "feature/name", HostSource: "default"}},
		{"https://codeberg.org/owner/repo/path@v1", RepositoryReference{Owner: "owner", Repo: "repo", Subpath: "path", Ref: "v1", Host: "codeberg.org", Scheme: "https", HostSource: "explicit"}},
		{"https://forge.example:3000/owner/repo", RepositoryReference{Owner: "owner", Repo: "repo", Host: "forge.example:3000", Scheme: "https", HostSource: "explicit"}},
		{"self:owner/repo/.gitea/workflows/ci.yml@main", RepositoryReference{Owner: "owner", Repo: "repo", Subpath: ".gitea/workflows/ci.yml", Ref: "main", HostSource: "self"}},
		{"./", WorkspaceReference{Path: ""}},
		{"./build", WorkspaceReference{Path: "build"}},
		{"$///", SelfRepositoryReference{Path: ""}},
		{"$/build", SelfRepositoryReference{Path: "build"}},
		{"docker://ghcr.io/owner/image:tag@sha256:abcd", ContainerReference{Image: "ghcr.io/owner/image:tag@sha256:abcd"}},
		{"builtin:checkout", BuiltinReference{Name: "checkout"}},
		{"owner/repo", UnknownReference{}},
		{"owner/repo@", UnknownReference{}},
		{"${{ matrix.action }}", UnknownReference{}},
		{"https://user:password@host/owner/repo@v1", UnknownReference{}},
		{"$/@main", UnknownReference{}},
		{"docker://", UnknownReference{}},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			got := ParseUsesReference(tc.raw)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %+v (%T), want %+v", got, got, tc.want)
			}
			data, err := json.Marshal(got)
			if err != nil {
				t.Fatal(err)
			}
			roundTrip, err := decodeUsesReference(data)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, roundTrip) {
				t.Fatalf("round trip changed reference: %s", data)
			}
		})
	}
}
