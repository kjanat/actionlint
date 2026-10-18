package main

import (
	"bytes"
	"encoding/json/v2"
	"os"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

func TestPinnedTriggerReconciliation(t *testing.T) {
	registry, _, err := loadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	for event, forbidden := range map[string]string{
		"merge_group": "destroyed", "check_suite": "requested", "issue_comment": "pinned", "image_version": "created",
	} {
		if slices.Contains(registry[event].Activities, forbidden) {
			t.Errorf("webhook-only activity admitted: %s.%s", event, forbidden)
		}
	}
	if !slices.Equal(registry["merge_group"].Activities, []string{"checks_requested"}) {
		t.Fatal("merge_group lost its supported activity")
	}
	if !slices.Equal(registry["image_version"].Fields, []string{"names", "versions"}) {
		t.Fatal("documentation override lost image_version filters")
	}
}

func TestGeneratedTriggerRegistryCurrent(t *testing.T) {
	want, err := registrySource()
	if err != nil {
		t.Fatal(err)
	}
	have, err := os.ReadFile("../webhooks.go")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bytes.ReplaceAll(have, []byte("\r\n"), []byte("\n")), want) {
		t.Fatal("trigger registry stale; run go run ./internal/webhook/generate --registry internal/webhook/webhooks.go from repository root")
	}
}

func TestStrictSchemaRejectsPermissivePath(t *testing.T) {
	src, err := sources.ReadFile("sources/workflow-v1.0.json")
	if err != nil {
		t.Fatal(err)
	}
	src = bytes.Replace(src, []byte(`"type": "on-strict"`), []byte(`"type": "on"`), 1)
	if _, err := schemaTriggers(src); err == nil || !strings.Contains(err.Error(), "strict") {
		t.Fatalf("permissive root accepted: %v", err)
	}
}

func TestDocumentationParserRejectsUnknownActivityCell(t *testing.T) {
	src := "## `merge_group`\n\n| Webhook event payload | Activity types | SHA |\n| --- | --- | --- |\n| payload | Unexpected format | sha |\n"
	if _, err := documentedTriggers([]byte(src), nil); err == nil {
		t.Fatal("unrecognized activity table silently accepted")
	}
}

func TestReconciliationRequiresReviewedCurrentExceptions(t *testing.T) {
	for _, tc := range []struct{ name, want string }{
		{"missing", "unreviewed difference: payload-activity-only:merge_group:destroyed"},
		{"stale", "stale exception: payload-activity-only:merge_group:invented"},
		{"incomplete", "incomplete exception: payload-activity-only:merge_group:destroyed"},
		{"digest", "pinned source digest mismatch"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := fstest.MapFS{}
			entries, err := sources.ReadDir("sources")
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				name := "sources/" + entry.Name()
				b, err := sources.ReadFile(name)
				if err != nil {
					t.Fatal(err)
				}
				files[name] = &fstest.MapFile{Data: b}
			}
			var manifest sourceManifest
			if err := json.Unmarshal(files["sources/manifest.json"].Data, &manifest); err != nil {
				t.Fatal(err)
			}
			const key = "payload-activity-only:merge_group:destroyed"
			switch tc.name {
			case "missing":
				delete(manifest.Exceptions, key)
			case "stale":
				manifest.Exceptions["payload-activity-only:merge_group:invented"] = manifest.Exceptions[key]
			case "incomplete":
				exception := manifest.Exceptions[key]
				exception.ProductionVerified = nil
				manifest.Exceptions[key] = exception
			case "digest":
				files["sources/workflow-v1.0.json"].Data = []byte(`{}`)
			}
			b, err := json.Marshal(manifest)
			if err != nil {
				t.Fatal(err)
			}
			files["sources/manifest.json"].Data = b
			if _, _, err := loadRegistryFiles(files); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("wanted %q; got %v", tc.want, err)
			}
		})
	}
}
