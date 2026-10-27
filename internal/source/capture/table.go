package capture

import (
	"cmp"
	"slices"

	"actionlint.kjanat.dev/v2/source"
)

// State describes already-discovered evidence; lookup never acquires evidence.
type State string

const (
	Absent     State = "absent"
	Pending    State = "pending"
	Captured   State = "captured"
	Failed     State = "failed"
	Conflicted State = "conflicted"
)

// Source is a copied row of the sealed source table. Conflicted documents expose
// no revision or model: first-arrival bytes remain private and cannot authorize
// admission. Code and Message are deterministic projections of Error for JSON.
type Source struct {
	Document             string             `json:"document"`
	State                State              `json:"state"`
	Revision             source.Revision    `json:"revision,omitzero"`
	Coordinates          source.Coordinates `json:"coordinates,omitempty"`
	CoordinatesAvailable bool               `json:"coordinates_available"`
	Aliases              []string           `json:"aliases"`
	Code                 string             `json:"code,omitempty"`
	Message              string             `json:"message,omitempty"`
	Error                error              `json:"-"`
}

func state(e *entry) State {
	switch {
	case e.conflict != nil:
		return Conflicted
	case e.done == nil:
		return Absent
	case e.err != nil:
		return Failed
	case e.value != nil:
		return Captured
	default:
		return Pending
	}
}

// Lookup performs no metadata or content IO and never waits. An alias not yet
// passed to Canonical/Read is Absent; admission must resolve authorized aliases
// through Canonical during the capture phase. A pending alias is Pending.
func (s *Store) Lookup(path string) (Snapshot, State, error) {
	if err := validatePath(path); err != nil {
		return Snapshot{}, Absent, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	name := s.aliases[path]
	if name == nil {
		return Snapshot{}, Absent, nil
	}
	select {
	case <-name.done:
	default:
		return Snapshot{}, Pending, nil
	}
	e := s.docs[name.document]
	current := state(e)
	switch current {
	case Captured:
		return s.snapshot(e), current, nil
	case Failed:
		return Snapshot{}, current, e.err
	case Conflicted:
		return Snapshot{}, current, e.conflict
	default:
		return Snapshot{}, current, nil
	}
}

// Sources returns copied rows only after Close has successfully joined all work.
// Aliases include every completed resolution, even from cancelled/conflicting
// requests. Returned rows and alias slices cannot mutate the sealed table.
func (s *Store) Sources() ([]Source, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.sealed {
		return nil, ErrNotSealed
	}
	rows := slices.Clone(s.sources)
	for index := range rows {
		rows[index].Aliases = slices.Clone(rows[index].Aliases)
	}
	return rows, nil
}

// sourceTable requires s.mu after all counted work has been joined.
func (s *Store) sourceTable() []Source {
	rows := make([]Source, 0, len(s.docs))
	for document, e := range s.docs {
		row := Source{Document: document, State: state(e), Aliases: s.snapshot(e).aliases}
		switch row.State {
		case Captured:
			row.Revision = e.value.revision
			row.Coordinates = e.value.model
			row.CoordinatesAvailable = e.value.utf8
		case Failed:
			row.Coordinates = e.model
			row.Error, row.Code, row.Message = e.err, e.err.Code(), e.err.Error()
		case Conflicted:
			row.Error, row.Code, row.Message = e.conflict, e.conflict.Code(), e.conflict.Error()
		}
		rows = append(rows, row)
	}
	slices.SortFunc(rows, func(a, b Source) int {
		return cmp.Compare(a.Document, b.Document)
	})
	return rows
}
