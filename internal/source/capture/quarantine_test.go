package capture

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/v2/source"
)

func TestQuarantineRejectsMisuseAndPreservesSealedTable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "source.yml")
	reader := func(context.Context, string) ([]byte, error) { return nil, errors.New("unexpected read") }
	store := newStore(t, []Overlay{{Path: path, Content: []byte("original"), Coordinates: source.YAML}}, reader)
	other := newStore(t, []Overlay{{Path: path, Content: []byte("original"), Coordinates: source.YAML}}, reader)
	captured := readSnapshot(t, store, path, source.YAML)
	foreign := readSnapshot(t, other, path, source.YAML)
	for _, invalid := range []Snapshot{{}, foreign} {
		if err := store.Quarantine(invalid, "contradictory evidence"); !errors.Is(err, ErrInvalidSnapshot) {
			t.Fatalf("invalid handle quarantine error = %v", err)
		}
	}
	if err := store.Quarantine(captured, " "); err == nil {
		t.Fatal("accepted an empty quarantine reason")
	}
	if err := store.Verify(captured, []byte("original")); err != nil {
		t.Fatalf("misuse quarantined a valid source: %v", err)
	}
	if err := store.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := store.Quarantine(captured, "late evidence"); !errors.Is(err, ErrClosed) {
		t.Fatalf("sealed quarantine error = %v", err)
	}
	rows, err := store.Sources()
	if err != nil || len(rows) != 1 || rows[0].State != Captured {
		t.Fatalf("late quarantine mutated seal: rows=%+v error=%v", rows, err)
	}
}
