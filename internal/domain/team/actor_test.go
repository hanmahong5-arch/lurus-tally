package team

import (
	"testing"

	"github.com/google/uuid"
)

func TestActorIDFromSubject(t *testing.T) {
	u := uuid.New()
	if got := ActorIDFromSubject(u.String()); got != u {
		t.Errorf("uuid sub: got %s, want %s", got, u)
	}
	if got := ActorIDFromSubject(""); got != uuid.Nil {
		t.Errorf("empty sub: got %s", got)
	}
	// Frozen value: changing the algorithm would orphan stored creator ids.
	want := uuid.NewSHA1(uuid.MustParse("6f1c2a54-3b7e-4d0a-9c85-2e4b7a91d3f6"), []byte("idp:12345"))
	if got := ActorIDFromSubject("12345"); got != want || got == uuid.Nil {
		t.Errorf("numeric sub: got %s, want %s", got, want)
	}
}
