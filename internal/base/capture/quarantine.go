package capture

import (
	"errors"
	"strings"
)

// Quarantine performs no IO, and late consumer evidence cannot change a sealed table.
func (s *Store) Quarantine(snapshot CapturedDocument, reason string) error {
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
