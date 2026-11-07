package job

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestNewSubmission(t *testing.T) {
	when := time.Date(2026, 12, 1, 15, 4, 5, 123456789, time.FixedZone("IST", 5*3600+30*60))
	priority := 7
	attempts := 3

	got, err := NewSubmission("email", []byte(`{"to":"a@example.com"}`), &priority, &when, &attempts, "welcome-1")
	if err != nil {
		t.Fatalf("valid submission: %v", err)
	}
	if got.Queue != "email" || got.Priority != 7 || got.MaxAttempts != 3 {
		t.Fatalf("normalized fields = %+v", got)
	}
	if !got.ScheduledAtWasSet || got.ScheduledAt == nil {
		t.Fatal("expected scheduled_at to be set")
	}
	if !got.ScheduledAt.Equal(when.UTC().Truncate(time.Microsecond)) {
		t.Fatalf("scheduled_at = %s", got.ScheduledAt)
	}
	if got.IdempotencyKey == nil || *got.IdempotencyKey != "welcome-1" {
		t.Fatalf("idempotency key = %v", got.IdempotencyKey)
	}

	defaults, err := NewSubmission("", []byte(` {} `), nil, nil, nil, "")
	if err != nil {
		t.Fatalf("defaults: %v", err)
	}
	if defaults.Queue != DefaultQueue || defaults.Priority != 0 || defaults.MaxAttempts != DefaultMaxAttempts {
		t.Fatalf("defaults = %+v", defaults)
	}
	if defaults.ScheduledAtWasSet || defaults.IdempotencyKey != nil {
		t.Fatal("omitted optional fields should stay unset")
	}
}

func TestNewSubmissionRejects(t *testing.T) {
	priority := 32768
	attempts := 0
	tooBig := []byte(`{"k":"` + strings.Repeat("x", MaxPayloadBytes) + `"}`)

	tests := []struct {
		name     string
		queue    string
		payload  string
		priority *int
		attempts *int
		key      string
		tooLarge bool
	}{
		{name: "queue", queue: "Email", payload: `{}`},
		{name: "payload missing", payload: ``},
		{name: "payload array", payload: `[]`},
		{name: "payload null", payload: `null`},
		{name: "payload string", payload: `"hi"`},
		{name: "payload too big", payload: string(tooBig), tooLarge: true},
		{name: "priority", payload: `{}`, priority: &priority},
		{name: "attempts", payload: `{}`, attempts: &attempts},
		{name: "idempotency", payload: `{}`, key: "has space"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewSubmission(tt.queue, []byte(tt.payload), tt.priority, nil, tt.attempts, tt.key)
			var validation *ValidationError
			if !errors.As(err, &validation) {
				t.Fatalf("error = %v", err)
			}
			if validation.TooLarge != tt.tooLarge {
				t.Fatalf("TooLarge = %v, want %v (%s)", validation.TooLarge, tt.tooLarge, validation.Message)
			}
		})
	}
}
