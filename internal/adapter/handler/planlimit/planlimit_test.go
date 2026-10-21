package planlimit_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/planlimit"
	"github.com/hanmahong5-arch/lurus-tally/internal/app/entitlement"
)

func TestWrite_PlanLimitBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for key, label := range map[string]string{
		entitlement.KeyMaxUsers: "成员", entitlement.KeyMaxSKUs: "商品", entitlement.KeyMaxWarehouses: "仓库",
	} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		wrapped := fmt.Errorf("create: %w", &entitlement.ErrPlanLimitReached{Key: key, Used: 5, Limit: 5})
		if !planlimit.Write(c, wrapped) {
			t.Fatal("must handle plan limit error")
		}
		if w.Code != http.StatusForbidden {
			t.Fatalf("status = %d", w.Code)
		}
		var body map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		want := fmt.Sprintf("当前套餐最多 5 个%s，已用 5，请升级套餐", label)
		if body["code"] != "plan_limit_reached" || body["message"] != want || body["key"] != key || body["used"] != float64(5) || body["limit"] != float64(5) {
			t.Fatalf("body = %v", body)
		}
	}
}

func TestWrite_OtherErrorUntouched(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	if planlimit.Write(c, errors.New("x")) || w.Body.Len() != 0 {
		t.Fatal("non plan-limit errors must not be written")
	}
}
