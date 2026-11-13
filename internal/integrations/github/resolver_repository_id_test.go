package github

import (
	"context"
	"encoding/json"
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/references"
	"actionlint.kjanat.dev/v2/internal/input/references/actions"
	"actionlint.kjanat.dev/v2/internal/model/repositoryid"
)

func TestResolverRepositoryIDSurvivesAcquisition(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"action", "workflow"} {
		for _, tc := range []struct {
			name                   string
			cached, resolved, want uint64
			cache                  bool
			fetch                  bool
		}{
			{"cold known", 0, 99, 99, false, true},
			{"cold unknown", 0, 0, 0, false, true},
			{"conflicting IDs", 42, 99, 99, true, true},
			{"matching IDs", 42, 42, 42, true, false},
			{"resolver learns ID", 0, 99, 99, true, false},
			{"cache knows ID", 42, 0, 42, true, false},
			{"both unknown", 0, 0, 0, true, false},
		} {
			t.Run(kind+"/"+tc.name, func(t *testing.T) {
				t.Parallel()
				id := func(value uint64) repositoryid.ID {
					if value == 0 {
						return repositoryid.ID{}
					}
					return knownRepositoryID(t, value)
				}
				request := ref(t, "example/action@v1")
				filename := "action.yml"
				if kind == "workflow" {
					request = ref(t, "example/action/.github/workflows@v1")
					filename = "build.yml"
				}
				path := "/example/action/" + sha + "/"
				if request.Directory() != "" {
					path += request.Directory() + "/"
				}
				path += filename
				hosts := gitAndRawHosts(t, map[string]string{path: "fresh metadata"})
				client, err := New(Options{CacheDir: t.TempDir(), Transport: hosts, Resolver: func() references.Resolver {
					return resolveFunc(func(context.Context, references.Request) (references.Result, error) {
						return references.Result{Version: 1, Repository: request.Repository(), RepositoryID: id(tc.resolved), Commit: sha, SelectedObject: sha, SelectedRef: "refs/tags/v1", ObservedAt: "2026-11-12T00:00:00Z", FetchLog: json.RawMessage(`{}`)}, nil
					})
				}})
				if err != nil {
					t.Fatal(err)
				}
				if tc.cache {
					route, err := actions.RefRoute("refs/tags/v1")
					if err != nil {
						t.Fatal(err)
					}
					cached, valid := selectedFile(request, request.Repository(), kind, id(tc.cached), sha, sha, route, "2026-11-11T00:00:00Z")
					if !valid {
						t.Fatal("could not construct cached selection")
					}
					cached.filename, cached.content = filename, "cached metadata"
					if err := client.store(t.Context(), cached); err != nil {
						t.Fatal(err)
					}
				}
				file, err := client.lookupFile(t.Context(), request, kind, []string{filename})
				if err != nil || !file.Available() {
					t.Fatalf("lookup: %v, %v", file, err)
				}
				if got := file.Identity().RepositoryID(); got != id(tc.want) {
					t.Errorf("repository ID = %v, want %v", got, id(tc.want))
				}
				wantContent, wantRaw := "cached metadata", 0
				if tc.fetch {
					wantContent, wantRaw = "fresh metadata", 1
				}
				if file.Content() != wantContent || hosts.count("raw.githubusercontent.com") != wantRaw || hosts.count("github.com") != 1 || len(hosts.recorded()) != 1+wantRaw {
					t.Errorf("content = %q, want %q; requests = %v, want one Git listing and %d raw requests", file.Content(), wantContent, hosts.recorded(), wantRaw)
				}
				record, exists, err := client.cached(t.Context(), request, id(tc.want), sha, []string{filename})
				if err != nil || !exists || record.RepositoryID != id(tc.want) || record.Content != wantContent {
					t.Errorf("stored cache = %+v, exists = %t, error = %v; want ID %v and content %q", record, exists, err, id(tc.want), wantContent)
				}
			})
		}
	}
}
