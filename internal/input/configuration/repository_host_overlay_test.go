package config

import (
	"slices"
	"testing"

	repository "actionlint.kjanat.dev/v2/internal/model/repositories"
)

func TestRepositoryRunnerOverlayPreservesOtherHostFacts(t *testing.T) {
	t.Parallel()
	rules, err := NewRuleSet(nil)
	if err != nil {
		t.Fatal(err)
	}
	names, err := repository.NewNames("HOST")
	if err != nil {
		t.Fatal(err)
	}
	host := repository.Snapshot{RunnerLabels: names, Secrets: names}
	for _, tc := range []struct {
		name, source string
		runnersKnown bool
		runners      []string
		secretsKnown bool
	}{
		{"omitted", `{}`, true, []string{"HOST"}, true},
		{"empty", `{"repository":{"runners":[]}}`, true, []string{}, true},
		{"null runners", `{"repository":{"runners":null}}`, false, nil, true},
		{"null repository", `{"repository":null}`, false, nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			resolved, err := rules.ResolveDocument([]byte(tc.source))
			if err != nil {
				t.Fatal(err)
			}
			facts := RepositoryFacts(resolved.Config, host)
			if facts.RunnerLabels.Available() != tc.runnersKnown || !slices.Equal(facts.RunnerLabels.Values(), tc.runners) {
				t.Errorf("runner labels: available=%t, values=%v; want available=%t, values=%v", facts.RunnerLabels.Available(), facts.RunnerLabels.Values(), tc.runnersKnown, tc.runners)
			}
			if facts.Secrets.Available() != tc.secretsKnown {
				t.Errorf("secrets available=%t, want %t", facts.Secrets.Available(), tc.secretsKnown)
			}
			if tc.secretsKnown && !slices.Equal(facts.Secrets.Values(), []string{"HOST"}) {
				t.Errorf("secrets: got %v, want [HOST]", facts.Secrets.Values())
			}
		})
	}
}
