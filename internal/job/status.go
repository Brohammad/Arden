package job

// Status is the current state of a job.
type Status string

const (
	StatusPending   Status = "pending"
	StatusRunning   Status = "running"
	StatusSucceeded Status = "succeeded"
	StatusDead      Status = "dead"
	StatusCancelled Status = "cancelled"
)

// Valid reports whether s is a known status.
func (s Status) Valid() bool {
	switch s {
	case StatusPending, StatusRunning, StatusSucceeded, StatusDead, StatusCancelled:
		return true
	default:
		return false
	}
}
