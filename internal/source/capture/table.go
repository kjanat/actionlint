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
	if !name.documentRequired || e == nil {
		return Snapshot{}, Absent, nil
	}
	current := state(e)
	switch current {
	case Captured:
		return s.snapshot(e), current, nil
	case Failed:
		return Snapshot{}, current, e.err
	case Conflicted:
		return Snapshot{}, current, e.conflict
	case Absent, Pending:
		// Absent and pending documents have no snapshot or error.
		fallthrough
	default:
		return Snapshot{}, current, nil
	}
}

// Sources returns copied rows only after Close has successfully joined all work.
// Aliases include every completed source resolution, even from cancelled or
// conflicting requests. Identity-only aliases are omitted. Returned rows and
// alias slices cannot mutate the sealed table.
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

// Revision returns an exact captured revision only after sealing. It performs
// no metadata or content IO and never admits failed or conflicted documents.
func (s *Store) Revision(revision source.Revision) (Snapshot, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.sealed || !revision.Valid() {
		return Snapshot{}, false
	}
	e := s.docs[revision.Document()]
	if e == nil || state(e) != Captured || e.value.revision != revision {
		return Snapshot{}, false
	}
	return s.snapshot(e), true
}

// ReadAliases lists requested Read aliases whose documents sealed as Captured,
// including cancelled waiters. Overlays and identity-only lookups do not count.
func (s *Store) ReadAliases() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.sealed {
		return nil, ErrNotSealed
	}
	var paths []string
	for _, entry := range s.docs {
		if state(entry) != Captured {
			continue
		}
		for path := range entry.readAliases {
			paths = append(paths, path)
		}
	}
	slices.Sort(paths)
	return paths, nil
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
		case Absent, Pending:
			// Absent and pending rows carry no capture payload.
		}
		rows = append(rows, row)
	}
	slices.SortFunc(rows, func(a, b Source) int {
		return cmp.Compare(a.Document, b.Document)
	})
	return rows
}
