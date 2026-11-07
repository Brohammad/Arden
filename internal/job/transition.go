package job

// transitions is the legal status-change matrix.
// A same-status write, such as a lease renewal, is not a transition.
var transitions = map[Status]map[Status]struct{}{
	StatusPending: {
		StatusRunning:   {},
		StatusCancelled: {},
	},
	StatusRunning: {
		StatusSucceeded: {},
		StatusPending:   {},
		StatusDead:      {},
	},
	StatusDead: {
		StatusPending: {},
	},
	StatusSucceeded: {},
	StatusCancelled: {},
}

// CanTransition reports whether a job may move from one status to another.
func CanTransition(from, to Status) bool {
	next, ok := transitions[from]
	if !ok {
		return false
	}
	_, ok = next[to]
	return ok
}
