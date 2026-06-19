package privacy

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

type mockEraser struct {
	n   int
	err error
	got int64
}

func (m *mockEraser) Erase(_ context.Context, accountID int64) (int, error) {
	m.got = accountID
	return m.n, m.err
}

func newReq(body string) (*httptest.ResponseRecorder, *gin.Context) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/internal/v1/privacy/erase", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	return w, c
}

func TestHandler_Erase_BadBody(t *testing.T) {
	w, c := newReq(`{not json`)
	NewHandler(&mockEraser{}).Erase(c)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestHandler_Erase_MissingAccount(t *testing.T) {
	w, c := newReq(`{"event_id":"acct-erase-0","reason":"user_requested"}`)
	er := &mockEraser{}
	NewHandler(er).Erase(c)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if er.got != 0 {
		t.Errorf("eraser must not be called for a missing account_id")
	}
}

func TestHandler_Erase_Accepted(t *testing.T) {
	w, c := newReq(`{"event_id":"acct-erase-5","account_id":5,"reason":"user_requested"}`)
	er := &mockEraser{n: 1}
	NewHandler(er).Erase(c)
	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", w.Code)
	}
	if er.got != 5 {
		t.Errorf("eraser got account_id %d, want 5", er.got)
	}
}

func TestHandler_Erase_NoData200(t *testing.T) {
	w, c := newReq(`{"event_id":"acct-erase-5","account_id":5}`)
	NewHandler(&mockEraser{n: 0}).Erase(c)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), "no_data") {
		t.Errorf("body = %s, want no_data", w.Body.String())
	}
}

func TestHandler_Erase_ServiceError500(t *testing.T) {
	w, c := newReq(`{"event_id":"acct-erase-5","account_id":5}`)
	NewHandler(&mockEraser{err: errors.New("boom")}).Erase(c)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
}
