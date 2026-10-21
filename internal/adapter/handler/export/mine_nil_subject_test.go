package export_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
)

// An all-zero subject (unauthenticated dev header) must not silently filter by
// the nil creator and return an empty file.
func TestBills_MineWithNilSubject_400(t *testing.T) {
	bills := &spyExporter{body: "h\n"}
	w := billsGet(t, bills, "?mine=1", uuid.Nil.String())
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, want 400", w.Code)
	}
	if bills.wasCalled() {
		t.Error("exporter must not run")
	}
}

// A numeric (non-UUID) subject is a real person; mine=1 must resolve it to the
// stable derived id instead of rejecting the request.
func TestBills_MineWithNumericSubject_200(t *testing.T) {
	bills := &spyExporter{body: "h\n"}
	w := billsGet(t, bills, "?mine=1", "10086")
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	if !bills.wasCalled() {
		t.Error("exporter must run")
	}
}

// mine=1 filters by exactly the id the bills are stamped with (ActorUUID), for a
// UUID subject and for a numeric one alike.
func TestBills_MineCreatorIDEqualsActorUUID(t *testing.T) {
	uuidSub := uuid.New().String()
	for _, sub := range []string{uuidSub, "10086"} {
		bills := &spyExporter{body: "h\n"}
		w := billsGet(t, bills, "?mine=1", sub)
		if w.Code != http.StatusOK {
			t.Fatalf("sub %s: code = %d, want 200", sub, w.Code)
		}
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Set(middleware.CtxKeyIDPSubject, sub)
		want := middleware.ActorUUID(c)
		if bills.gotFilter.CreatorID == nil || *bills.gotFilter.CreatorID != want {
			t.Errorf("sub %s: CreatorID = %v, want %s", sub, bills.gotFilter.CreatorID, want)
		}
	}
}
