package job

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Job is the persisted definition and current state of one unit of work.
type Job struct {
	ID             uuid.UUID
	Queue          string
	Payload        json.RawMessage
	Status         Status
	Priority       int16
	ScheduledAt    time.Time
	Attempt        int
	MaxAttempts    int
	LeaseOwner     *uuid.UUID
	LeaseExpiresAt *time.Time
	FencingToken   int64
	IdempotencyKey *string
	LastError      *string
	Result         json.RawMessage
	ScheduleID     *uuid.UUID
	OccurrenceAt   *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}
