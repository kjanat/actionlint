package expressions

func newTestChecker() *Checker { return New(Dependencies{}, Options{UnsoundTernary: true}) }
