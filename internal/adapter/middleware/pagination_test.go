package middleware_test

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
)

func ginCtx(qs string) *gin.Context {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/?"+strings.TrimPrefix(qs, "?"), nil)
	return c
}

func TestParseLimitQuery_ClampsAbovenMax(t *testing.T) {
	c := ginCtx("limit=999999")
	got := middleware.ParseLimitQuery(c, "limit", 20, 500)
	if got != 500 {
		t.Errorf("limit=999999 with max=500 → %d, want 500 (clamped, not error)", got)
	}
}

func TestParseLimitQuery_DefaultsWhenAbsent(t *testing.T) {
	c := ginCtx("")
	got := middleware.ParseLimitQuery(c, "limit", 20, 500)
	if got != 20 {
		t.Errorf("absent limit → %d, want default 20", got)
	}
}

func TestParseLimitQuery_RejectsZeroAndNegative(t *testing.T) {
	for _, q := range []string{"limit=0", "limit=-5", "limit=abc"} {
		c := ginCtx(q)
		got := middleware.ParseLimitQuery(c, "limit", 20, 500)
		if got != 20 {
			t.Errorf("%s → %d, want default 20", q, got)
		}
	}
}

func TestParseLimitQuery_AcceptsValidRange(t *testing.T) {
	c := ginCtx("limit=100")
	got := middleware.ParseLimitQuery(c, "limit", 20, 500)
	if got != 100 {
		t.Errorf("limit=100 → %d, want 100", got)
	}
}

func TestParseLimitQuery_DefaultExceedingMaxIsClamped(t *testing.T) {
	c := ginCtx("")
	got := middleware.ParseLimitQuery(c, "limit", 1000, 500)
	if got != 500 {
		t.Errorf("def=1000 max=500 → %d, want 500 (def clamped to max)", got)
	}
}

func TestParseOffsetQuery_DefaultsToZero(t *testing.T) {
	c := ginCtx("")
	if got := middleware.ParseOffsetQuery(c, "offset"); got != 0 {
		t.Errorf("absent offset → %d, want 0", got)
	}
}

func TestParseOffsetQuery_RejectsNegative(t *testing.T) {
	c := ginCtx("offset=-1")
	if got := middleware.ParseOffsetQuery(c, "offset"); got != 0 {
		t.Errorf("negative offset → %d, want 0", got)
	}
}

func TestParseOffsetQuery_ClampsAboveMax(t *testing.T) {
	c := ginCtx("offset=2147483647")
	got := middleware.ParseOffsetQuery(c, "offset")
	if got != middleware.DefaultMaxPageOffset {
		t.Errorf("offset=2147483647 → %d, want %d (clamped)", got, middleware.DefaultMaxPageOffset)
	}
}

func TestParseOffsetQuery_AcceptsValidWithinMax(t *testing.T) {
	c := ginCtx("offset=1000")
	if got := middleware.ParseOffsetQuery(c, "offset"); got != 1000 {
		t.Errorf("offset=1000 → %d, want 1000", got)
	}
}
