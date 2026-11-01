package config

import (
	"context"
	"errors"
	"testing"
)

func TestLoadRepositoryContextStopsBeforeReading(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, _, err := LoadRepositoryContext(ctx, t.TempDir(), func(string) ([]byte, error) {
		t.Fatal("canceled configuration discovery read a source")
		return nil, nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("discovery error = %v", err)
	}
}

func TestInspectContextStopsBeforeSelection(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := InspectContext(ctx, Selection{Path: "missing.yaml"}, true)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("inspection error = %v", err)
	}
}
