package config

import "testing"

func TestDisabledTimeoutDoesNotExposeBounds(t *testing.T) {
	t.Parallel()
	policy := &JobTimeoutPolicy{minMinutes: 5}
	if lower, present := policy.MinMinutes(); lower != 0 || present {
		t.Fatalf("disabled policy exposed its minimum: %v, %t", lower, present)
	}
}
