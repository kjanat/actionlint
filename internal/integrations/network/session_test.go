package network

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestOfflineErrorClassification(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"offline", ErrOffline, true},
		{"wrapped", fmt.Errorf("config: %w", ErrOffline), true},
		{"joined", errors.Join(ErrOffline, Missing("plugin")), true},
		{"canceled", errors.Join(ErrOffline, context.Canceled), false},
		{"corrupt", errors.Join(ErrOffline, errors.New("checksum mismatch")), false},
		{"nil", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := OnlyOffline(tc.err); got != tc.want {
				t.Fatalf("OnlyOffline(%v)=%t, want %t", tc.err, got, tc.want)
			}
		})
	}
}

func TestOfflineSessionWarningsAndCancellation(t *testing.T) {
	t.Parallel()
	ctx, session := WithSession(t.Context(), Options{Offline: true})
	Warn(ctx, "missing plugin")
	Warn(ctx, "missing plugin")
	warnings := session.Warnings()
	if len(warnings) != 1 {
		t.Fatalf("warnings=%v", warnings)
	}
	warnings[0] = "changed"
	if session.Warnings()[0] != "missing plugin" {
		t.Fatal("caller changed stored warning")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := RequireOnline(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v", err)
	}
	if err := RequireOnline(t.Context()); err != nil {
		t.Fatal(err)
	}
	message := Missing("https://user:secret@example.org/config?token=secret#secret").Error()
	if strings.Contains(message, "secret") {
		t.Fatalf("credentials in warning: %s", message)
	}
}
