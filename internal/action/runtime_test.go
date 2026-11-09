package action

import (
	"strings"
	"testing"
)

func TestLegacyRuntimeReplacement(t *testing.T) {
	for _, using := range []string{"node12", "NODE12", "node16", "NODE16"} {
		message := RuntimeProblem(using)
		if !strings.Contains(message, "deprecated") || !strings.Contains(message, `use "node24" instead`) {
			t.Errorf("runtime %s: missing execution replacement in %q", using, message)
		}
		if strings.Contains(message, "no longer bundled") {
			t.Errorf("runtime %s: reported an unavailable executable despite the replacement: %s", using, message)
		}
	}
	if message := RuntimeProblem("node24"); message != "" {
		t.Errorf("current runtime: %s", message)
	}
}

func TestNode20HostedMigration(t *testing.T) {
	for _, using := range []string{"node20", "NODE20"} {
		message := RuntimeProblem(using)
		for _, want := range []string{"GitHub.com", "Node 24", "no longer supports the Node 20 opt-out"} {
			if !strings.Contains(message, want) {
				t.Errorf("runtime %s: wanted %q in %q", using, want, message)
			}
		}
		if strings.Contains(message, "scheduled") || strings.Contains(message, "no longer bundled") {
			t.Errorf("runtime %s: conflated hosted migration with bundle inventory: %s", using, message)
		}
	}
}
