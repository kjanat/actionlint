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
		"merge_group": "destroyed", "check_suite": "requested", "issue_comment": "pinned",
	} {
		if slices.Contains(registry[event].Activities, forbidden) {
			t.Errorf("webhook-only activity admitted: %s.%s", event, forbidden)
		}
	}
	if !slices.Equal(registry["merge_group"].Activities, []string{"checks_requested"}) {
		t.Fatal("merge_group lost its supported activity")
	}
	if !slices.Equal(registry["image_version"].Fields, []string{"names", "types", "versions"}) {
		t.Fatal("documentation override lost image_version filters")
	}
}

func TestReviewedSchemaActivitySupplements(t *testing.T) {
	registry, _, err := loadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	for event, activities := range map[string][]string{
		"image_version":       {"created", "ready", "deleted"},
		"pull_request":        {"stacked"},
		"pull_request_target": {"stacked"},
	} {
		for _, activity := range activities {
			if !slices.Contains(registry[event].Activities, activity) {
				t.Errorf("reviewed schema activity lost: %s.%s", event, activity)
			}
		}
		if !slices.Contains(registry[event].Fields, "types") {
			t.Errorf("reviewed types filter lost: %s", event)
		}
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

func TestDocumentationParserSelectsCurrentTableVariant(t *testing.T) {
	src := "## `pull_request_target`\n" +
		"| Webhook event payload | Activity types | SHA |\n" +
		"| --- | --- | --- |\n" +
		"| {% ifversion ghes < 3.20 %} |\n" +
		"| payload | `opened`, `legacy_only` | sha |\n" +
		"| {% else %} |\n" +
		"| payload | `opened`, `milestoned`, `demilestoned` | sha |\n" +
		"| {% endif %} |\n"
	got, err := documentedTriggers([]byte(src), nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"demilestoned", "milestoned", "opened"}; !slices.Equal(got["pull_request_target"], want) {
		t.Fatalf("current activities = %v; want %v", got["pull_request_target"], want)
	}
}

func TestPinnedPullRequestTargetActivities(t *testing.T) {
	registry, _, err := loadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range []string{"pull_request", "pull_request_target"} {
		for _, activity := range []string{"milestoned", "demilestoned"} {
			if !slices.Contains(registry[event].Activities, activity) {
				t.Errorf("current documentation permits %s.%s", event, activity)
			}
		}
	}
}

func TestDocumentationParserRejectsUnknownTableCondition(t *testing.T) {
	src := "## `pull_request_target`\n| {% ifversion unknown-product %} |\n| payload | `opened` | sha |\n"
	if _, err := documentedTriggers([]byte(src), nil); err == nil || !strings.Contains(err.Error(), "unsupported documentation table condition") {
		t.Fatalf("unknown table variant accepted: %v", err)
	}
}

func TestReconciliationRequiresReviewedCurrentExceptions(t *testing.T) {
	for _, tc := range []struct{ name, want string }{
		{"missing", "unreviewed difference: payload-activity-only:merge_group:destroyed"},
		{"stale", "stale exception: payload-activity-only:merge_group:invented"},
		{"incomplete", "incomplete exception: payload-activity-only:merge_group:destroyed"},
		{"digest", "pinned source digest mismatch"},
		{"payload supplement", "schema supplement requires a schema activity"},
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
			case "payload supplement":
				exception := manifest.Exceptions[key]
				exception.PreserveSchema = true
				manifest.Exceptions[key] = exception
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
