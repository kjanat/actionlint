package github

import (
	"net/http"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/development/tests/support/gitwire"
	"actionlint.kjanat.dev/v2/internal/input/references"
)

func TestMetadataMirrorsRespectVisibility(t *testing.T) {
	t.Parallel()
	for _, policy := range []struct {
		name   string
		access references.MirrorAccess
	}{
		{"default", references.PublicMirrorsOnly},
		{"allow unknown", references.AllowUnknownMirrors},
		{"disabled", references.NoMirrors},
	} {
		for _, visibility := range []struct {
			name    string
			private *bool
		}{
			{"unknown", nil}, {"public", new(false)}, {"private", new(true)},
		} {
			for _, kind := range []string{"action", "workflow"} {
				t.Run(policy.name+"/"+visibility.name+"/"+kind, func(t *testing.T) {
					t.Parallel()
					hosts := mirrorHosts(t, reply(http.StatusServiceUnavailable, rawText, ""), reply(http.StatusOK, jsDelivrYAML, actionSource))
					if kind == "workflow" {
						actionHandler := hosts.handle
						hosts.handle = func(r *http.Request) (*http.Response, error) {
							if strings.HasPrefix(r.URL.Path, "/repos/example/action/contents/") {
								return respond(r, http.StatusOK, "", "on: workflow_call\njobs: {}\n"), nil
							}
							return actionHandler(r)
						}
					}
					client, err := New(Options{Transport: hosts, Token: "fixture-token", Mirrors: policy.access})
					if err != nil {
						t.Fatal(err)
					}
					client.listings.observeVisibility("EXAMPLE/Action", visibility.private)
					if kind == "action" {
						manifest, err := client.Lookup(t.Context(), ref(t, "example/action@"+sha))
						if err != nil || !manifest.Available() {
							t.Fatalf("action lookup: %v, %v", manifest, err)
						}
					} else {
						file, err := client.LookupWorkflow(t.Context(), "example/action/.github/workflows/build.yml@"+sha)
						if err != nil || !file.Available() {
							t.Fatalf("workflow lookup: %v, %v", file, err)
						}
					}
					allowed := policy.access != references.NoMirrors && (visibility.private != nil && !*visibility.private || visibility.private == nil && policy.access == references.AllowUnknownMirrors)
					wantMirror, wantREST := 0, len(restRequests)
					if allowed {
						wantMirror, wantREST = 1, 0
					}
					if hosts.count("cdn.jsdelivr.net") != wantMirror || hosts.count("api.github.com") != wantREST {
						t.Fatalf("requests = %v; want %d mirror and %d REST requests", hosts.recorded(), wantMirror, wantREST)
					}
				})
			}
		}
	}
}

func TestOnlyAnonymousGitListingEstablishesPublicVisibility(t *testing.T) {
	t.Parallel()
	for _, token := range []string{"", "fixture-token"} {
		t.Run("token="+token, func(t *testing.T) {
			t.Parallel()
			client, err := New(Options{Token: token, Transport: transport(reply(http.StatusOK, gitwire.ContentType, actionListing()))})
			if err != nil {
				t.Fatal(err)
			}
			if _, missing, err := client.fetchRefListing(t.Context(), "example/action"); err != nil || missing != nil {
				t.Fatalf("listing: %v, %v", missing, err)
			}
			if got, want := client.mirrorAllowed("example/action"), token == ""; got != want {
				t.Fatalf("mirror allowed = %t, want %t", got, want)
			}
		})
	}
}

func TestRawFailuresWithUnknownVisibilityUseREST(t *testing.T) {
	t.Parallel()
	for _, failure := range environmentFailures() {
		t.Run(failure.name, func(t *testing.T) {
			t.Parallel()
			hosts := mirrorHosts(t, failure.respond, func(r *http.Request) (*http.Response, error) {
				t.Fatalf("unknown repository disclosed to %s", r.URL)
				return nil, nil
			})
			client, err := New(Options{Transport: hosts})
			if err != nil {
				t.Fatal(err)
			}
			manifest, err := client.Lookup(t.Context(), ref(t, "example/action@"+sha))
			if err != nil || !manifest.Available() || hosts.count("api.github.com") != len(restRequests) {
				t.Fatalf("lookup = %v, %v; requests = %v", manifest, err, hosts.recorded())
			}
		})
	}
}

func TestRepositoryVisibilityComesFromGitHubResponses(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, field string
		allowed     bool
	}{
		{"public", `,"private":false`, true},
		{"private", `,"private":true`, false},
		{"omitted", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			client, err := New(Options{Transport: transport(func(r *http.Request) (*http.Response, error) {
				return respond(r, http.StatusOK, "application/json", `{"id":42,"full_name":"example/action"`+tc.field+`}`), nil
			})})
			if err != nil {
				t.Fatal(err)
			}
			if _, _, missing, err := client.repository(t.Context(), ref(t, "example/action@"+sha)); missing != nil || err != nil {
				t.Fatalf("repository = %v, %v", missing, err)
			}
			if got := client.mirrorAllowed("EXAMPLE/action"); got != tc.allowed {
				t.Fatalf("mirror allowed = %t, want %t", got, tc.allowed)
			}
		})
	}
}

func TestPrivateRepositoryCannotBeReclassifiedByAnonymousSuccess(t *testing.T) {
	t.Parallel()
	client, err := New(Options{Mirrors: references.AllowUnknownMirrors})
	if err != nil {
		t.Fatal(err)
	}
	client.listings.observeVisibility("example/action", new(true))
	client.listings.observeVisibility("example/action", new(false))
	if client.mirrorAllowed("example/action") {
		t.Fatal("known private repository became eligible for a mirror")
	}
}
