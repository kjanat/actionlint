package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	actionmeta "actionlint.kjanat.dev/v2/internal/action"
	"actionlint.kjanat.dev/v2/internal/actioncatalog"
)

func TestPinnedMetadataUsesImmutableSource(t *testing.T) {
	const sha = "0123456789012345678901234567890123456789"
	g := newTestGen(io.Discard, io.Discard, io.Discard)
	g.rawRegistry = []byte(`[{"slug":"owner/action","path":"/sub","tags":["v1"],"pins":{"v1":"` + sha + `"}}]`)
	g.client = &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() != "https://raw.githubusercontent.com/owner/action/"+sha+"/sub/action.yml" {
			t.Errorf("mutable metadata request: %s", req.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("name: pinned\ninputs: {token: {required: true}}\noutputs: {result: {}}\nruns: {using: node24, main: index.js}\n"))}, nil
	})}
	actions, err := g.fetchRemote()
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range []string{"v1", sha} {
		meta := actions["owner/action/sub@"+ref]
		if meta == nil || !meta.Inputs["token"].Required || meta.Outputs["result"] == nil {
			t.Fatalf("missing pinned metadata for %s: %+v", ref, meta)
		}
	}
	var out bytes.Buffer
	if err := g.writeCatalog(&out, actions); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "owner/action/sub@"+sha) {
		t.Fatal("catalogue lost pin")
	}
}

func TestServiceRetirementAppliesToTagsAndPins(t *testing.T) {
	const sha = "0123456789012345678901234567890123456789"
	for _, tc := range []struct {
		slug    string
		tag     string
		retired bool
	}{
		{"actions/upload-artifact", "v1", true}, {"actions/upload-artifact", "v2", true},
		{"actions/upload-artifact", "v3", true}, {"actions/upload-artifact", "v3-node20", true},
		{"actions/download-artifact", "v1", true}, {"actions/download-artifact", "v2", true},
		{"actions/download-artifact", "v3", true}, {"actions/download-artifact", "v3-node20", true},
		{"actions/cache", "v1", true}, {"actions/cache", "v2", true},
		{"actions/upload-artifact", "v4", false}, {"actions/download-artifact", "v4", false},
		{"actions/cache", "v3", false},
	} {
		t.Run(tc.slug+"@"+tc.tag, func(t *testing.T) {
			g := newGen(io.Discard, io.Discard, io.Discard, map[string]actionmeta.Runtime{
				"node20": {Removed: true, ExecutionRuntime: "Node24"}, "node24": {},
			})
			g.rawRegistry = []byte(`[{"slug":"` + tc.slug + `","tags":["` + tc.tag + `"],"pins":{"` + tc.tag + `":"` + sha + `"}}]`)
			g.client = &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("name: action\nruns: {using: node20, main: index.js}\n"))}, nil
			})}
			actions, err := g.fetchRemote()
			if err != nil {
				t.Fatal(err)
			}
			var out bytes.Buffer
			if err := g.writeCatalog(&out, actions); err != nil {
				t.Fatal(err)
			}
			catalog, err := actioncatalog.Decode(out.Bytes())
			if err != nil {
				t.Fatal(err)
			}
			for _, ref := range []string{tc.tag, sha} {
				spec := tc.slug + "@" + ref
				_, active := catalog.References[spec]
				if active == tc.retired {
					t.Errorf("%s active=%t, want %t", spec, active, !tc.retired)
				}
				if slices.Contains(catalog.Retired, spec) != tc.retired {
					t.Errorf("%s retirement mismatch", spec)
				}
			}
		})
	}
}

func TestRegistryPinsResolveCommits(t *testing.T) {
	const tag = "1111111111111111111111111111111111111111"
	const commit = "2222222222222222222222222222222222222222"
	actions := []*registry{{Slug: "owner/action", Tags: []string{"v1", "main"}}, {Slug: "owner/action", Path: "/nested", Tags: []string{"v1"}}}
	calls := 0
	err := pinRegistry(t.Context(), actions, func(context.Context, string) (map[string]string, error) {
		calls++
		return map[string]string{"refs/tags/v1": tag, "refs/tags/v1^{}": commit, "refs/heads/main": commit}, nil
	})
	if err != nil || calls != 1 {
		t.Fatalf("resolution: %d, %v", calls, err)
	}
	for _, action := range actions {
		for _, pin := range action.Pins {
			if pin != commit {
				t.Fatalf("tag object used as commit: %s", pin)
			}
		}
	}
	for _, missing := range []bool{false, true} {
		err := pinRegistry(t.Context(), actions, func(context.Context, string) (map[string]string, error) {
			if missing {
				return map[string]string{}, nil
			}
			return nil, errors.New("offline")
		})
		if err == nil {
			t.Fatal("unresolved commit accepted")
		}
	}
}

func TestRegistryRejectsInvalidPins(t *testing.T) {
	for _, pins := range []string{`{"v1":"main"}`, `{"v2":"0123456789012345678901234567890123456789"}`} {
		g := newTestGen(io.Discard, io.Discard, io.Discard)
		g.rawRegistry = []byte(`[{"slug":"a/b","tags":["v1"],"pins":` + pins + `}]`)
		if _, err := g.registry(); err == nil {
			t.Fatalf("accepted %s", pins)
		}
	}
}

func TestPinnedMetadataPreservesRetirement(t *testing.T) {
	const sha = "0123456789012345678901234567890123456789"
	g := newTestGen(io.Discard, io.Discard, io.Discard)
	g.rawRegistry = []byte(`[{"slug":"actions/checkout","tags":["v1"],"pins":{"v1":"` + sha + `"}}]`)
	g.client = &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("name: legacy\nruns: {using: node24, main: index.js}\n"))}, nil
	})}
	actions, err := g.fetchRemote()
	if err != nil {
		t.Fatal(err)
	}
	if !g.outdated("actions/checkout@"+sha, actions["actions/checkout@"+sha].Runs.Using) {
		t.Fatal("retired tag's commit became active")
	}
}
