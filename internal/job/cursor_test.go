package job

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestCursorRoundTrip(t *testing.T) {
	createdAt := time.UnixMicro(time.Date(2026, 12, 2, 3, 4, 5, 0, time.UTC).UnixMicro()).UTC()
	id := uuid.MustParse("6f1b5c2e-9a4d-4e1f-8c3a-0b7d6e5f4a21")

	encoded := EncodeCursor(createdAt, id)
	gotTime, gotID, err := DecodeCursor(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if !gotTime.Equal(createdAt) || gotID != id {
		t.Fatalf("got %s %s", gotTime, gotID)
	}
}

func TestDecodeCursorRejects(t *testing.T) {
	for _, cursor := range []string{"", "@@@", "bm90LWEtdmFsaWQtY3Vyc29y"} {
		if _, _, err := DecodeCursor(cursor); err == nil {
			t.Fatalf("cursor %q was accepted", cursor)
		}
	}
}
