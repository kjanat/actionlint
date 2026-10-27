package capture

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/v2/source"
)

func TestVerifyHandleRejectsForeignAndQuarantinedEvidence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "source.yml")
	reader := func(context.Context, string) ([]byte, error) { return nil, errors.New("unexpected read") }
	overlays := []Overlay{{Path: path, Content: []byte("captured"), Coordinates: source.YAML}}
	store, other := newStore(t, overlays, reader), newStore(t, overlays, reader)
	snapshot := readSnapshot(t, store, path, source.YAML)
	foreign := readSnapshot(t, other, path, source.YAML)
	for _, invalid := range []Snapshot{{}, foreign} {
		if err := store.VerifyHandle(invalid); !errors.Is(err, ErrInvalidSnapshot) {
			t.Fatalf("invalid snapshot check = %v", err)
		}
	}
	if err := store.VerifyHandle(snapshot); err != nil {
		t.Fatal(err)
	}
	if allocations := testing.AllocsPerRun(100, func() {
		if err := store.VerifyHandle(snapshot); err != nil {
			t.Fatal(err)
		}
	}); allocations != 0 {
		t.Fatalf("handle admission allocated %v times", allocations)
	}
	conflict := store.Quarantine(snapshot, "contradictory declaration")
	if conflict == nil || !errors.Is(store.VerifyHandle(snapshot), conflict) {
		t.Fatalf("quarantined handle remained admitted: %v", conflict)
	}
}
