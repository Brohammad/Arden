package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/Brohammad/Arden/internal/job"
)

type errorBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	var body errorBody
	body.Error.Code = code
	body.Error.Message = message
	writeJSON(w, status, body)
}

type jobResponse struct {
	ID             string          `json:"id"`
	Queue          string          `json:"queue"`
	Payload        json.RawMessage `json:"payload"`
	Status         job.Status      `json:"status"`
	Priority       int16           `json:"priority"`
	ScheduledAt    time.Time       `json:"scheduled_at"`
	Attempt        int             `json:"attempt"`
	MaxAttempts    int             `json:"max_attempts"`
	LeaseOwner     *string         `json:"lease_owner"`
	LeaseExpiresAt *time.Time      `json:"lease_expires_at"`
	FencingToken   int64           `json:"fencing_token"`
	IdempotencyKey *string         `json:"idempotency_key"`
	LastError      *string         `json:"last_error"`
	Result         json.RawMessage `json:"result"`
	ScheduleID     *string         `json:"schedule_id"`
	OccurrenceAt   *time.Time      `json:"occurrence_at"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

func newJobResponse(j job.Job) jobResponse {
	resp := jobResponse{
		ID:             j.ID.String(),
		Queue:          j.Queue,
		Payload:        j.Payload,
		Status:         j.Status,
		Priority:       j.Priority,
		ScheduledAt:    j.ScheduledAt.UTC(),
		Attempt:        j.Attempt,
		MaxAttempts:    j.MaxAttempts,
		LeaseExpiresAt: utcPtr(j.LeaseExpiresAt),
		FencingToken:   j.FencingToken,
		IdempotencyKey: j.IdempotencyKey,
		LastError:      j.LastError,
		Result:         j.Result,
		OccurrenceAt:   utcPtr(j.OccurrenceAt),
		CreatedAt:      j.CreatedAt.UTC(),
		UpdatedAt:      j.UpdatedAt.UTC(),
	}
	if resp.Result == nil {
		resp.Result = json.RawMessage("null")
	}
	if j.LeaseOwner != nil {
		value := j.LeaseOwner.String()
		resp.LeaseOwner = &value
	}
	if j.ScheduleID != nil {
		value := j.ScheduleID.String()
		resp.ScheduleID = &value
	}
	return resp
}

func utcPtr(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	utc := value.UTC()
	return &utc
}
