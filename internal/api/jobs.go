package api

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Brohammad/Arden/internal/job"
	"github.com/Brohammad/Arden/internal/store"
	"github.com/google/uuid"
)

const maxBodyBytes = job.MaxPayloadBytes + 4096

type createRequest struct {
	Queue       *string         `json:"queue"`
	Payload     json.RawMessage `json:"payload"`
	Priority    *int            `json:"priority"`
	ScheduledAt *string         `json:"scheduled_at"`
	MaxAttempts *int            `json:"max_attempts"`
}

type listResponse struct {
	Jobs       []jobResponse `json:"jobs"`
	NextCursor *string       `json:"next_cursor"`
}

func (s *server) createJob(w http.ResponseWriter, r *http.Request) {
	var req createRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	var scheduled *time.Time
	if req.ScheduledAt != nil {
		parsed, err := time.Parse(time.RFC3339Nano, *req.ScheduledAt)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_argument", "scheduled_at must be RFC3339")
			return
		}
		scheduled = &parsed
	}

	idempotencyKey, ok := idempotencyKeyFrom(w, r)
	if !ok {
		return
	}

	queue := ""
	if req.Queue != nil {
		queue = *req.Queue
	}
	sub, err := job.NewSubmission(queue, req.Payload, req.Priority, scheduled, req.MaxAttempts, idempotencyKey)
	if err != nil {
		writeValidation(w, err)
		return
	}

	createdJob, created, err := s.store.CreateJob(r.Context(), sub)
	if err != nil {
		writeStoreError(s, w, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, newJobResponse(createdJob))
}

func (s *server) getJob(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	found, err := s.store.GetJob(r.Context(), id)
	if err != nil {
		writeStoreError(s, w, err)
		return
	}
	writeJSON(w, http.StatusOK, newJobResponse(found))
}

func (s *server) listJobs(w http.ResponseWriter, r *http.Request) {
	filter, ok := listFilterFrom(w, r)
	if !ok {
		return
	}
	page, err := s.store.ListJobs(r.Context(), filter)
	if err != nil {
		writeStoreError(s, w, err)
		return
	}

	resp := listResponse{Jobs: make([]jobResponse, 0, len(page.Jobs))}
	for _, found := range page.Jobs {
		resp.Jobs = append(resp.Jobs, newJobResponse(found))
	}
	if page.HasMore && len(page.Jobs) > 0 {
		last := page.Jobs[len(page.Jobs)-1]
		cursor := job.EncodeCursor(last.CreatedAt, last.ID)
		resp.NextCursor = &cursor
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *server) cancelJob(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	found, err := s.store.CancelJob(r.Context(), id)
	if err != nil {
		writeStoreError(s, w, err)
		return
	}
	writeJSON(w, http.StatusOK, newJobResponse(found))
}

func listFilterFrom(w http.ResponseWriter, r *http.Request) (store.ListFilter, bool) {
	query := r.URL.Query()
	filter := store.ListFilter{Limit: 50}

	if raw := query.Get("limit"); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 200 {
			writeError(w, http.StatusBadRequest, "invalid_argument", "limit must be between 1 and 200")
			return store.ListFilter{}, false
		}
		filter.Limit = limit
	}

	if raw := query.Get("status"); raw != "" {
		status := job.Status(raw)
		if !status.Valid() {
			writeError(w, http.StatusBadRequest, "invalid_argument", "status is invalid")
			return store.ListFilter{}, false
		}
		filter.Status = &status
	}

	if raw := query.Get("queue"); raw != "" {
		if !job.ValidQueue(raw) {
			writeError(w, http.StatusBadRequest, "invalid_argument", "queue must match ^[a-z0-9][a-z0-9_-]{0,63}$")
			return store.ListFilter{}, false
		}
		filter.Queue = &raw
	}

	if raw := query.Get("cursor"); raw != "" {
		createdAt, id, err := job.DecodeCursor(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_argument", "cursor is invalid")
			return store.ListFilter{}, false
		}
		filter.CursorCreatedAt = &createdAt
		filter.CursorID = &id
	}
	return filter, true
}

func pathID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_argument", "id must be a UUID")
		return uuid.UUID{}, false
	}
	return id, true
}

func idempotencyKeyFrom(w http.ResponseWriter, r *http.Request) (string, bool) {
	values := r.Header.Values("Idempotency-Key")
	if len(values) == 0 {
		return "", true
	}
	if len(values) != 1 || values[0] == "" {
		writeError(w, http.StatusBadRequest, "invalid_argument", "idempotency key must be 1-255 visible ASCII characters")
		return "", false
	}
	return values[0], true
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dest any) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "invalid_argument", "content type must be application/json")
		return false
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dest); err != nil {
		var maxBytes *http.MaxBytesError
		if errors.As(err, &maxBytes) {
			writeError(w, http.StatusRequestEntityTooLarge, "invalid_argument", "payload exceeds 256 KiB")
			return false
		}
		writeError(w, http.StatusBadRequest, "invalid_argument", jsonErrorMessage(err))
		return false
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid_argument", "request body must contain a single JSON value")
		return false
	}
	return true
}

func jsonErrorMessage(err error) string {
	var syntax *json.SyntaxError
	var unmarshal *json.UnmarshalTypeError
	switch {
	case errors.As(err, &syntax):
		return "request body must be valid JSON"
	case errors.As(err, &unmarshal):
		return "request body has the wrong JSON type"
	case strings.Contains(err.Error(), "unknown field"):
		return "request body contains an unknown field"
	default:
		return "request body must be a JSON object"
	}
}

func writeValidation(w http.ResponseWriter, err error) {
	var validation *job.ValidationError
	if errors.As(err, &validation) {
		status := http.StatusBadRequest
		if validation.TooLarge {
			status = http.StatusRequestEntityTooLarge
		}
		writeError(w, status, "invalid_argument", validation.Message)
		return
	}
	writeError(w, http.StatusBadRequest, "invalid_argument", "invalid request")
}

func writeStoreError(s *server, w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "job not found")
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "conflict", "job cannot be cancelled")
	case errors.Is(err, store.ErrIdempotencyMismatch):
		writeError(w, http.StatusConflict, "idempotency_mismatch", "idempotency key was already used with a different request")
	default:
		s.log.Error("request failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "internal error")
	}
}
