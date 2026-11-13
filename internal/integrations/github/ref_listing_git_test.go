package github

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/development/tests/support/gitwire"
	"actionlint.kjanat.dev/v2/internal/input/references"
	"actionlint.kjanat.dev/v2/internal/input/references/repository"
	gitcommand "actionlint.kjanat.dev/v2/internal/integrations/git"
)

func TestGitAdvertisementDoesNotIdentifyTagTargetTypes(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	git := func(input string, args ...string) string {
		t.Helper()
		command := gitcommand.Command(t.Context(), "git", directory, append([]string{"-c", "user.name=Fixture", "-c", "user.email=fixture@example.test", "-c", "tag.gpgsign=false"}, args...)...)
		command.Stdin = strings.NewReader(input)
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, output)
		}
		return string(output)
	}
	git("", "init", "--initial-branch=main")
	blob := strings.TrimSpace(git("contents", "hash-object", "-w", "--stdin"))
	tree := strings.TrimSpace(git("100644 blob "+blob+"\tfile\n", "mktree"))
	commit := strings.TrimSpace(git("initial\n", "commit-tree", tree))
	git("", "update-ref", "refs/heads/main", commit)
	for _, target := range []struct{ name, id string }{{"tree", tree}, {"blob", blob}} {
		git("", "tag", target.name, target.id)
		git("", "tag", "-a", target.name+"-annotated", target.id, "-m", target.name)
	}
	advertisement := fmt.Sprintf("%04x%s0000", len(serviceAnnouncement)+4, serviceAnnouncement) + git("", "upload-pack", "--advertise-refs", ".")
	parsed, err := parseRefAdvertisement([]byte(advertisement))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.objects[commit] != (repository.Commit{}) {
		t.Fatalf("branch tip was not recognized as a commit: %v", parsed.objects[commit])
	}
	for _, name := range []string{"tree", "blob", "tree-annotated", "blob-annotated"} {
		id := parsed.refs["refs/tags/"+name]
		if object, exists := parsed.objects[id]; exists {
			t.Errorf("Git tag %s points to %s, but the advertisement was interpreted as %T: %+v", name, strings.TrimSpace(git("", "cat-file", "-t", id)), object, object)
		}
	}
}

func TestListingSessionVerifiesNonBranchObjects(t *testing.T) {
	t.Parallel()
	for _, target := range []struct {
		kind   string
		object repository.Object
		peeled repository.Object
	}{
		{"commit", repository.Commit{}, repository.PeeledTag{Commit: sha}},
		{"tree", repository.Tree{}, repository.PeeledTreeTag{Tree: sha}},
		{"blob", nil, nil},
		{"missing", nil, nil},
	} {
		for _, annotated := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/annotated=%t", target.kind, annotated), func(t *testing.T) {
				t.Parallel()
				selected := sha
				tag := gitwire.Ref{Name: "refs/tags/example", Object: sha}
				want := target.object
				if annotated {
					selected, tag.Object, tag.Peeled, want = tagObject, tagObject, sha, target.peeled
				}
				parsed, err := parseRefAdvertisement([]byte(listing(branchTip, gitwire.Ref{Name: "refs/heads/main", Object: branchTip}, tag)))
				if err != nil {
					t.Fatal(err)
				}
				hosts := &recorder{handle: func(r *http.Request) (*http.Response, error) {
					if r.URL.Host != "api.github.com" {
						t.Fatalf("object verification contacted %s", r.URL)
					}
					switch r.URL.Path {
					case "/repos/example/action":
						return respond(r, http.StatusOK, "application/json", `{"id":42,"full_name":"example/action","default_branch":"main"}`), nil
					case "/repos/example/action/contents":
						return respond(r, http.StatusOK, "application/json", `[]`), nil
					case "/repos/example/action/git/" + target.kind + "s/" + sha:
						return respond(r, http.StatusOK, "application/json", fmt.Sprintf(`{"sha":%q}`, sha)), nil
					case "/repos/example/action/git/tags/" + tagObject:
						return respond(r, http.StatusOK, "application/json", fmt.Sprintf(`{"sha":%q,"object":{"sha":%q,"type":%q}}`, tagObject, sha, target.kind)), nil
					}
					return respond(r, http.StatusNotFound, "application/json", `{}`), nil
				}}
				client, err := New(Options{Transport: hosts})
				if err != nil {
					t.Fatal(err)
				}
				session := newListingSession(client, parsed, references.Request{Version: 1, Repository: "example/action", Ref: "example", Kind: "action"})
				object, found, err := session.Object(t.Context(), selected)
				switch {
				case target.kind == "missing" && !annotated:
					if err != nil || found || object != nil {
						t.Fatalf("missing object=%v found=%t error=%v", object, found, err)
					}
				case want == nil:
					if err == nil || found || object != nil {
						t.Fatalf("blob accepted as %T: %v, found=%t error=%v", object, object, found, err)
					}
				case err != nil || !found || object != want:
					t.Fatalf("object=%T %+v found=%t error=%v, want %T %+v", object, object, found, err, want, want)
				}
				if hosts.count("api.github.com") == 0 {
					t.Fatal("object type was inferred without verification")
				}
				if want != nil {
					log, err := session.Finish(t.Context())
					if err != nil || !strings.Contains(string(log), "/git/") {
						t.Fatalf("object verification was not recorded: %s, error=%v", log, err)
					}
				}
			})
		}
	}
}
