package helper

type Store struct{ rows [][]int }

func (s *Store) Row(i int) []int { return s.rows[i] } // want "ARC003: Row result 1"
func (s *Store) Rows() [][]int   { return s.rows }    // want "ARC003: Rows result 1"
func (s *Store) Count(i int) int { return len(s.rows[i]) }

// Pick returns its last parameter, not the receiver's storage.
func (s *Store) Pick(i int, row []int) []int { return row }

func (s *Store) EachRow(yield func([]int) bool) {
	for _, r := range s.rows {
		if !yield(r) {
			return
		}
	}
}
