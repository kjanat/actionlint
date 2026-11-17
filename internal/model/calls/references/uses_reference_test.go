package references

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestUsesReference(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
			got := Classify(tc.raw)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %+v (%T), want %+v", got, got, tc.want)
			}
			data, err := json.Marshal(got)
			if err != nil {
				t.Fatal(err)
			}
			roundTrip, err := DecodeUsesReference(data)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, roundTrip) {
				t.Fatalf("round trip changed reference: %s", data)
			}
		})
	}
}

func TestWorkflowCallReference(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		raw  string
		want UsesReference
	}{
		{"./.github/workflows/deploy.yml", SelfRepositoryReference{Path: ".github/workflows/deploy.yml"}},
		{"$/.github/workflows/deploy.yml", SelfRepositoryReference{Path: ".github/workflows/deploy.yml"}},
		{".//.github/workflows/deploy.yml", SelfRepositoryReference{Path: ".github/workflows/deploy.yml"}},
		{"./.github/workflows/deploy.yml@main", UnknownReference{}},
		{"owner/repo/.github/workflows/deploy.yml@v1", RepositoryReference{Owner: "owner", Repo: "repo", Subpath: ".github/workflows/deploy.yml", Ref: "v1", HostSource: "default"}},
		{"${{ inputs.workflow }}", UnknownReference{}},
		{"", nil},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			t.Parallel()
			if got := ClassifyWorkflow(tc.raw); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %+v (%T), want %+v", got, got, tc.want)
			}
		})
	}
}

// TestFormOfMatchesPrefixChecks proves FormOf agrees with the prefix tests it
// replaced at each caller, for spellings with and without a valid remainder.
func TestFormOfMatchesPrefixChecks(t *testing.T) {
	t.Parallel()
	for _, spec := range []string{
		"", " ", ".", "./", "./a", "./a@v", ".//a", "./a ${{ x }}", "../a", ".@v", ".x/r@v",
		"$", "$/", "$/a", "$/a@v", "$//a", "$@v", "$x/r@v", "$$/r@v",
		"docker://", "docker://x", "docker://x:1", "docker://x@sha256:ab", "docker://${{ x }}", "Docker://x", "DOCKER://x", "docker:/x", "docker:x",
		"builtin:", "builtin:x", "builtin:o/r@v", "Builtin:x",
		"o/r@v", "o/r", "self:o/r@v", "https://h/o/r@v", " o/r@v", "o/r@v ${{ x }}", "${{ x }}",
	} {
		form := FormOf(spec)
		if got, want := form == FormWorkspace, strings.HasPrefix(spec, "./"); got != want {
			t.Errorf("FormOf(%q) workspace = %v, want %v", spec, got, want)
		}
		if got, want := form == FormSelfRepository, strings.HasPrefix(spec, "$/"); got != want {
			t.Errorf("FormOf(%q) self-repository = %v, want %v", spec, got, want)
		}
		if got, want := form == FormContainer, strings.HasPrefix(spec, "docker://"); got != want {
			t.Errorf("FormOf(%q) container = %v, want %v", spec, got, want)
		}
		if got, want := form == FormBuiltin, strings.HasPrefix(spec, "builtin:"); got != want {
			t.Errorf("FormOf(%q) builtin = %v, want %v", spec, got, want)
		}
		image, hasImage := ContainerImage(spec)
		wantImage, wantHasImage := strings.CutPrefix(spec, "docker://")
		if image != wantImage || hasImage != wantHasImage {
			t.Errorf("ContainerImage(%q) = %q, %v; want %q, %v", spec, image, hasImage, wantImage, wantHasImage)
		}
		other := strings.HasPrefix(spec, "./") || strings.HasPrefix(spec, "$/") || strings.HasPrefix(spec, "docker://") || strings.HasPrefix(spec, "builtin:")
		if got := form == FormRepository; got == other {
			t.Errorf("FormOf(%q) repository = %v, want %v", spec, got, !other)
		}
	}
}
