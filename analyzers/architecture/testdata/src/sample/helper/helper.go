package helper                // want package:"function flow summaries"
func Borrow[T any](v []T) []T { return v }
func Detach(v []byte) []byte  { out := make([]byte, len(v)); copy(out, v); return out }
