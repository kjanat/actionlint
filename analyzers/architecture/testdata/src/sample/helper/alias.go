package helper // want package:"function flow summaries"

type Alias[T any] = []T
type Phantom[T any] = int

func Forward(f func(*int64), p *int64) { f(p) }

type Handle struct{ value *int64 }

func NewHandle(p *int64) Handle        { return Handle{value: p} }
func (h Handle) Change(f func(*int64)) { f(h.value) }
