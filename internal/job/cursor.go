package job

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// EncodeCursor builds an opaque keyset cursor for (created_at DESC, id DESC).
func EncodeCursor(createdAt time.Time, id uuid.UUID) string {
	raw := strconv.FormatInt(createdAt.UTC().UnixMicro(), 10) + "|" + id.String()
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// DecodeCursor parses a cursor produced by EncodeCursor.
func DecodeCursor(cursor string) (time.Time, uuid.UUID, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, uuid.UUID{}, fmt.Errorf("decode cursor: %w", err)
	}
	micros, idText, ok := strings.Cut(string(decoded), "|")
	if !ok || micros == "" || idText == "" {
		return time.Time{}, uuid.UUID{}, fmt.Errorf("cursor is missing a field")
	}
	usec, err := strconv.ParseInt(micros, 10, 64)
	if err != nil {
		return time.Time{}, uuid.UUID{}, fmt.Errorf("cursor time: %w", err)
	}
	id, err := uuid.Parse(idText)
	if err != nil {
		return time.Time{}, uuid.UUID{}, fmt.Errorf("cursor id: %w", err)
	}
	return time.UnixMicro(usec).UTC(), id, nil
}
