package job

import (
	"bytes"
	"encoding/json"
	"regexp"
	"time"
)

const (
	DefaultQueue       = "default"
	DefaultMaxAttempts = 5
	MaxPayloadBytes    = 256 * 1024
	minAttempts        = 1
	maxAttemptsLimit   = 100
	minPriority        = -32768
	maxPriority        = 32767
)

var (
	queuePattern       = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)
	idempotencyPattern = regexp.MustCompile(`^[\x21-\x7e]{1,255}$`)
)

// ValidationError is a caller mistake that should be returned as HTTP 400 or 413.
type ValidationError struct {
	Message  string
	TooLarge bool
}

func (e *ValidationError) Error() string { return e.Message }

// Submission is a validated job create request.
type Submission struct {
	Queue             string
	Payload           json.RawMessage
	Priority          int16
	ScheduledAt       *time.Time
	ScheduledAtWasSet bool
	MaxAttempts       int
	IdempotencyKey    *string
}

// NewSubmission validates and normalizes a create request.
// idempotencyKey is empty when the client did not send one.
// scheduledAt is nil when the client omitted it; the database then uses now().
func NewSubmission(queue string, payload json.RawMessage, priority *int, scheduledAt *time.Time, maxAttempts *int, idempotencyKey string) (Submission, error) {
	if queue == "" {
		queue = DefaultQueue
	}
	if !queuePattern.MatchString(queue) {
		return Submission{}, &ValidationError{Message: "queue must match ^[a-z0-9][a-z0-9_-]{0,63}$"}
	}

	if len(bytes.TrimSpace(payload)) == 0 {
		return Submission{}, &ValidationError{Message: "payload is required"}
	}
	if len(payload) > MaxPayloadBytes {
		return Submission{}, &ValidationError{Message: "payload exceeds 256 KiB", TooLarge: true}
	}
	if !isJSONObject(payload) {
		return Submission{}, &ValidationError{Message: "payload must be a JSON object"}
	}

	var pri int16
	if priority != nil {
		if *priority < minPriority || *priority > maxPriority {
			return Submission{}, &ValidationError{Message: "priority must be between -32768 and 32767"}
		}
		pri = int16(*priority)
	}

	attempts := DefaultMaxAttempts
	if maxAttempts != nil {
		if *maxAttempts < minAttempts || *maxAttempts > maxAttemptsLimit {
			return Submission{}, &ValidationError{Message: "max_attempts must be between 1 and 100"}
		}
		attempts = *maxAttempts
	}

	var scheduled *time.Time
	wasSet := scheduledAt != nil
	if wasSet {
		truncated := scheduledAt.UTC().Truncate(time.Microsecond)
		scheduled = &truncated
	}

	var key *string
	if idempotencyKey != "" {
		if !idempotencyPattern.MatchString(idempotencyKey) {
			return Submission{}, &ValidationError{Message: "idempotency key must be 1-255 visible ASCII characters"}
		}
		key = &idempotencyKey
	}

	return Submission{
		Queue:             queue,
		Payload:           append(json.RawMessage(nil), payload...),
		Priority:          pri,
		ScheduledAt:       scheduled,
		ScheduledAtWasSet: wasSet,
		MaxAttempts:       attempts,
		IdempotencyKey:    key,
	}, nil
}

// ValidQueue reports whether queue matches the name rule.
func ValidQueue(queue string) bool {
	return queuePattern.MatchString(queue)
}

func isJSONObject(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return false
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	var obj map[string]json.RawMessage
	if err := dec.Decode(&obj); err != nil {
		return false
	}
	return !dec.More()
}
