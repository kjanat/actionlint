package action

import (
	"strings"
	"testing"
)

func TestNode20HostedMigration(t *testing.T) {
	t.Parallel()
	for _, using := range []string{"node20", "NODE20"} {
		code, message := RuntimeProblem(using)
		if code != CodeRuntimeDeprecated {
			t.Errorf("runtime %s: code=%q, want %q", using, code, CodeRuntimeDeprecated)
		}
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

func TestRuntimeProblemCodeAndMessage(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		using   string
		code    ProblemCode
		message string
	}{
		{"node20", CodeRuntimeDeprecated, "is deprecated; GitHub.com"},
		{"NODE20", CodeRuntimeDeprecated, "is deprecated; GitHub.com"},
		{"node16", CodeRuntimeDeprecated, `use "node24" instead`},
		{"NODE16", CodeRuntimeDeprecated, `use "node24" instead`},
		{"node12", CodeRuntimeDeprecated, `use "node24" instead`},
		{"NODE12", CodeRuntimeDeprecated, `use "node24" instead`},
		{"node24", "", ""},
		{"NODE24", "", ""},
		{"node999", "", ""},
		{"", "", ""},
	} {
		t.Run(tc.using, func(t *testing.T) {
			t.Parallel()
			code, message := RuntimeProblem(tc.using)
			if code != tc.code {
				t.Fatalf("code=%q message=%q; want code %q", code, message, tc.code)
			}
			if tc.message == "" {
				if message != "" {
					t.Fatalf("no-finding code has message %q", message)
				}
			} else if !strings.Contains(message, tc.message) {
				t.Fatalf("code=%q message=%q; want message containing %q", code, message, tc.message)
			}
		})
	}
}
