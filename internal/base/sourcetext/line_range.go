package sourcetext

// LineRange is an inclusive range of one-based line numbers.
type LineRange struct{ First, Last int }

// Contains reports whether line lies within r.
func (r LineRange) Contains(line int) bool { return r.First <= line && line <= r.Last }

// Overlaps reports whether r and other share at least one line.
func (r LineRange) Overlaps(other LineRange) bool {
	return r.First <= other.Last && other.First <= r.Last
}
