package webhook_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
	appimporting "github.com/hanmahong5-arch/lurus-tally/internal/app/importing"
)

// 租户既没给单据仓库也没设默认仓时，取消回库不是服务器故障：
// 返回 422 warehouse_required 和中文提示，店主知道去「仓库」里补设置。
func TestShopifyWebhook_OrderCancelled_NoWarehouse_Returns422(t *testing.T) {
	uc := &mockIngest{cancelErr: fmt.Errorf("importing: approve reversal: %w", appbill.ErrWarehouseRequired)}
	r := buildEngine(testSecret, &mockResolver{m: defaultMapping()}, uc)
	body := orderBody("#2003", "SKU-W", 1, "10.00")
	w := doOrderRequest(r, body, sign(body), "test.myshopify.com", "orders/cancelled")

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("want 422, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["error"] != "warehouse_required" {
		t.Errorf("error = %v, want warehouse_required", resp["error"])
	}
	if msg, _ := resp["message"].(string); msg == "" {
		t.Error("message is empty")
	}
}

// 超卖订单只留草稿：取消事件无货可回，必须 200 收尾，否则 Shopify 无限重试直至删掉订阅。
func TestShopifyWebhook_OrderCancelled_NothingToReverse_Returns200(t *testing.T) {
	uc := &mockIngest{cancelResult: &appimporting.CancelResult{PlatformOrderNo: "#2004", NothingToReverse: true}}
	r := buildEngine(testSecret, &mockResolver{m: defaultMapping()}, uc)
	body := orderBody("#2004", "SKU-W", 1, "10.00")
	w := doOrderRequest(r, body, sign(body), "test.myshopify.com", "orders/cancelled")

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["status"] != "nothing_to_reverse" {
		t.Errorf("status = %v, want nothing_to_reverse", resp["status"])
	}
}

// 状态/校验类错误重试也不会好，不能落到 500。
func TestShopifyWebhook_OrderCancelled_UnreversibleState_Not5xx(t *testing.T) {
	for _, e := range []error{appbill.ErrInvalidBillStatus, appbill.ErrValidation} {
		uc := &mockIngest{cancelErr: fmt.Errorf("importing: create reversal: %w", e)}
		r := buildEngine(testSecret, &mockResolver{m: defaultMapping()}, uc)
		body := orderBody("#2005", "SKU-W", 1, "10.00")
		w := doOrderRequest(r, body, sign(body), "test.myshopify.com", "orders/cancelled")
		if w.Code != http.StatusOK {
			t.Errorf("%v: want 200, got %d body=%s", e, w.Code, w.Body.String())
		}
	}
}
