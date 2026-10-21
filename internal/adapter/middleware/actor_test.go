package middleware_test

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
)

func actorCtx(sub *string) *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	if sub != nil {
		c.Set(middleware.CtxKeyIDPSubject, *sub)
	}
	return c
}

func TestActorUUID(t *testing.T) {
	str := func(s string) *string { return &s }

	t.Run("uuid sub kept as is", func(t *testing.T) {
		want := uuid.New()
		if got := middleware.ActorUUID(actorCtx(str(want.String()))); got != want {
			t.Fatalf("got %s want %s", got, want)
		}
	})
	t.Run("numeric sub is stable", func(t *testing.T) {
		a := middleware.ActorUUID(actorCtx(str("312345678901234567")))
		b := middleware.ActorUUID(actorCtx(str("312345678901234567")))
		if a == uuid.Nil || a != b {
			t.Fatalf("unstable or nil: %s %s", a, b)
		}
	})
	t.Run("different subs differ", func(t *testing.T) {
		a := middleware.ActorUUID(actorCtx(str("312345678901234567")))
		b := middleware.ActorUUID(actorCtx(str("312345678901234568")))
		if a == b {
			t.Fatal("collision")
		}
	})
	t.Run("empty or absent is nil", func(t *testing.T) {
		if middleware.ActorUUID(actorCtx(str(""))) != uuid.Nil {
			t.Fatal("empty should be nil")
		}
		if middleware.ActorUUID(actorCtx(nil)) != uuid.Nil {
			t.Fatal("absent should be nil")
		}
	})
}
