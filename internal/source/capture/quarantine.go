package capture

import (
	"errors"
	"strings"
)

// Quarantine makes contradictory consumer evidence a sticky document conflict.
// It performs no IO and retains the private bytes unchanged. Consumers supply a
// deterministic reason; declaration or other domain policy stays outside Store.
// A sealed table cannot be changed by late consumer evidence.
func (s *Store) Quarantine(snapshot Snapshot, reason string) error {
	if strings.TrimSpace(reason) == "" {
		return errors.New("source quarantine requires a reason")
	}
	if err := s.Verify(snapshot, snapshot.Bytes()); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	document := snapshot.value.revision.Document()
	e := s.docs[document]
	if e.conflicted() {
		return e.conflict
	}
	if s.sealed {
		return &ClosedError{}
	}
	e.conflict = ConflictError{document, reason}
	return e.conflict
}
