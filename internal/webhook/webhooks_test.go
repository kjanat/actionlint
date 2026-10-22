package webhook

import (
	"slices"
	"testing"
)

func TestGeneratedAllWebhooks(t *testing.T) {
	if len(AllWebhookTypes) == 0 {
		t.Fatal("AllWebhookTypes is empty")
	}

	for name, types := range AllWebhookTypes {
		if name == "" {
			t.Errorf("Name is empty (types=%v)", types)
			continue
		}

		seen := map[string]struct{}{}
		for _, ty := range types {
			if _, ok := seen[ty]; ok {
				t.Errorf("type %q duplicates in webhook %q: %v", ty, name, types)
			} else {
				seen[ty] = struct{}{}
			}
		}
	}
}

func TestDefaultActivities(t *testing.T) {
	for _, event := range []string{"pull_request", "pull_request_target"} {
		activities, ok := DefaultActivities(event)
		if !ok || !slices.Equal(activities, []string{"opened", "synchronize", "reopened"}) {
			t.Fatalf("%s defaults: %v, %v", event, activities, ok)
		}
		if !slices.Contains(AllWebhookTypes[event], "closed") || slices.Contains(activities, "closed") {
			t.Fatal("permitted activities confused with default activities")
		}
		activities[0] = "mutated"
		if next, _ := DefaultActivities(event); next[0] == "mutated" {
			t.Fatal("caller mutated registry")
		}
	}
	if activities, ok := DefaultActivities("merge_group"); !ok || !slices.Equal(activities, []string{"checks_requested"}) {
		t.Fatalf("merge_group defaults: %v", activities)
	}
	if activities, ok := DefaultActivities("push"); !ok || activities == nil || len(activities) != 0 {
		t.Fatalf("actionless event: %v, %v", activities, ok)
	}
	if activities, ok := DefaultActivities("repository_dispatch"); !ok || activities != nil {
		t.Fatalf("custom activities: %v, %v", activities, ok)
	}
	if _, ok := DefaultActivities("unknown"); ok {
		t.Fatal("unknown event accepted")
	}
}
