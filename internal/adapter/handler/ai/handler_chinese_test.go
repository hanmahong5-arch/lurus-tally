package ai_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	handlerai "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/ai"
	appai "github.com/hanmahong5-arch/lurus-tally/internal/app/ai"
)

func hasASCIILetter(s string) bool {
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' {
			return true
		}
	}
	return false
}

// A purchase draft naming a product the books do not have fails with a prompt
// that names the product and says what to do about it.
func TestAIHandler_ConfirmPlan_ProductNotFound_NamesTheProduct(t *testing.T) {
	err := fmt.Errorf("confirm plan: %w: %w", appai.ErrPlanExecutionFailed, &appai.ProductNotFoundError{Name: "红富士"})
	e := newTestEngine(handlerai.New(&stubOrchestrator{confirmErr: err}), uuid.New())
	rec := postConfirm(e, uuid.New(), "")
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	want := "找不到商品「红富士」，请先建这个商品，或者换成商品库里的名字"
	if !strings.Contains(rec.Body.String(), want) {
		t.Fatalf("body=%s want detail %q", rec.Body.String(), want)
	}
}

// Every other prompt the AI endpoints send to a shopkeeper's screen is Chinese
// and never carries internal error text.
func TestAIHandler_OtherDetailsAreChinese(t *testing.T) {
	pid := uuid.New().String()
	reverter := &stubReverter{}
	cases := []struct {
		name   string
		h      *handlerai.Handler
		tenant uuid.UUID
		path   string
		body   string
	}{
		{"chat_no_tenant", handlerai.New(&stubOrchestrator{}), uuid.Nil, "/api/v1/ai/chat", `{"message":"x"}`},
		{"chat_bad_body", handlerai.New(&stubOrchestrator{}), uuid.New(), "/api/v1/ai/chat", `{}`},
		{"confirm_bad_id", handlerai.New(&stubOrchestrator{}), uuid.New(), "/api/v1/ai/plans/bad/confirm", ``},
		{"cancel_bad_id", handlerai.New(&stubOrchestrator{}), uuid.New(), "/api/v1/ai/plans/bad/cancel", ``},
		{"cancel_not_found", handlerai.New(&stubOrchestrator{cancelErr: appai.ErrPlanNotFound}), uuid.New(), "/api/v1/ai/plans/" + pid + "/cancel", ``},
		{"revert_off", handlerai.New(&stubOrchestrator{}), uuid.New(), "/api/v1/ai/plans/" + pid + "/revert", ``},
		{"revert_bad_id", handlerai.New(&stubOrchestrator{}).WithReverter(reverter), uuid.New(), "/api/v1/ai/plans/bad/revert", ``},
		{"revert_not_found", handlerai.New(&stubOrchestrator{}).WithReverter(&stubReverter{err: appai.ErrPlanNotFound}), uuid.New(), "/api/v1/ai/plans/" + pid + "/revert", ``},
		{"revert_already", handlerai.New(&stubOrchestrator{}).WithReverter(&stubReverter{err: appai.ErrAlreadyReverted}), uuid.New(), "/api/v1/ai/plans/" + pid + "/revert", ``},
		{"revert_window", handlerai.New(&stubOrchestrator{}).WithReverter(&stubReverter{err: appai.ErrRevertWindowClosed}), uuid.New(), "/api/v1/ai/plans/" + pid + "/revert", ``},
		{"revert_not_revertible", handlerai.New(&stubOrchestrator{}).WithReverter(&stubReverter{err: appai.ErrPlanNotRevertible}), uuid.New(), "/api/v1/ai/plans/" + pid + "/revert", ``},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newTestEngine(tc.h, tc.tenant)
			req, _ := http.NewRequest(http.MethodPost, tc.path, bytes.NewReader([]byte(tc.body)))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)
			if rec.Code < 400 {
				t.Fatalf("expected an error status, got %d", rec.Code)
			}
			var body map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("body=%s", rec.Body.String())
			}
			if body["detail"] == "" || hasASCIILetter(body["detail"]) {
				t.Fatalf("detail must be non-empty Chinese, got %q", body["detail"])
			}
		})
	}
}

// A chat stream that dies must not push the raw error text to the screen.
func TestAIHandler_Chat_StreamError_IsChinese(t *testing.T) {
	e := newTestEngine(handlerai.New(&stubOrchestrator{streamErr: errors.New("dial tcp 10.0.0.1:443: i/o timeout")}), uuid.New())
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/ai/chat", bytes.NewReader([]byte(`{"message":"x"}`)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if strings.Contains(rec.Body.String(), "dial tcp") {
		t.Fatalf("internal error leaked: %s", rec.Body.String())
	}
	for _, ev := range handlerai.ParseSSEEvents(rec.Body.Bytes()) {
		if ev.Event == "error" && hasASCIILetter(strings.ReplaceAll(strings.ReplaceAll(ev.Data, `"error"`, ""), `"detail"`, "")) {
			t.Fatalf("error event not Chinese: %s", ev.Data)
		}
	}
}

// A plan that is no longer pending is a state conflict (409); a failing plan
// store is a server fault (5xx), not "refresh and retry".
func TestAIHandler_ConfirmPlan_ConflictVersusStoreFault(t *testing.T) {
	conflict := fmt.Errorf("confirm plan: plan is confirmed, cannot confirm: %w", appai.ErrPlanNotPending)
	rec := postConfirm(newTestEngine(handlerai.New(&stubOrchestrator{confirmErr: conflict}), uuid.New()), uuid.New(), "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("conflict: code=%d body=%s", rec.Code, rec.Body.String())
	}
	fault := fmt.Errorf("confirm plan: get: %w", errors.New("redis: connection refused"))
	rec = postConfirm(newTestEngine(handlerai.New(&stubOrchestrator{confirmErr: fault}), uuid.New()), uuid.New(), "")
	if rec.Code < 500 {
		t.Fatalf("store fault: code=%d body=%s", rec.Code, rec.Body.String())
	}
}
