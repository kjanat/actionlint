package config_test

import (
	"context"
	"errors"
	"testing"

	checking "actionlint.kjanat.dev/v2/internal/checking"
	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"
)

func TestLoadRepositoryContextStopsBeforeReading(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, _, err := checking.ConfigurationRules().LoadRepositoryContext(ctx, t.TempDir(), func(string) ([]byte, error) {
		t.Fatal("canceled configuration discovery read a source")
		return nil, nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("discovery error = %v", err)
	}
}

func TestInspectContextStopsBeforeSelection(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := checking.ConfigurationRules().InspectContext(ctx, repoconfig.Selection{Path: "missing.yaml"}, true)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("inspection error = %v", err)
	}
}
