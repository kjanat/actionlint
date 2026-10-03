package actionlint

// executableState tracks when indexed modes still describe execution.
type executableState struct {
	sequential, pristine bool
	repositoryUnknown    bool
	changed              map[string]bool
}

func newExecutableState(hosted, callerRepositoryUnknown bool) executableState {
	return executableState{
		sequential:        true,
		repositoryUnknown: callerRepositoryUnknown || !hosted,
		changed:           make(map[string]bool),
	}
}

func (state *executableState) afterPossibleFailure() {
	state.pristine, state.repositoryUnknown = false, true
}

func (state *executableState) afterConcurrentExecution() {
	state.sequential, state.pristine = false, false
}

func (state *executableState) afterKnownCheckout() {
	state.pristine, state.repositoryUnknown = true, false
	clear(state.changed)
}
