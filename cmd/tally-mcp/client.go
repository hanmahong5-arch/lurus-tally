package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// tallyClient is a thin HTTP client over the Tally REST API.
// Stays minimal on purpose: V0 only needs read paths, so we hand-roll the
// few endpoints instead of pulling in a generator.
type tallyClient struct {
	baseURL string
	tenant  string // dev-mode X-Tenant-ID
	pat     string // Phase 2 bearer
	http    *http.Client
}

const httpTimeout = 15 * time.Second

func newTallyClient(c config) *tallyClient {
	return &tallyClient{
		baseURL: c.BaseURL,
		tenant:  c.TenantID,
		pat:     c.PAT,
		http:    &http.Client{Timeout: httpTimeout},
	}
}

// StockSnapshot mirrors the shape returned by GET /api/v1/stock/snapshots.
// We re-declare it here rather than importing internal/domain to keep this
// binary independent — the MCP boundary is the right place to lock the wire
// contract, not the Go type system.
type StockSnapshot struct {
	ID           string `json:"id"`
	ProductID    string `json:"product_id"`
	WarehouseID  string `json:"warehouse_id"`
	OnHandQty    string `json:"on_hand_qty"`
	AvailableQty string `json:"available_qty"`
	UnitCost     string `json:"unit_cost"`
	CostStrategy string `json:"cost_strategy"`
	UpdatedAt    string `json:"updated_at"`
}

type listSnapshotsResponse struct {
	Items []StockSnapshot `json:"items"`
}

// BillHead is the shared list-row shape for both /sale-bills and /purchase-bills.
// Both endpoints return {items, total} with these fields populated.
type BillHead struct {
	ID          string `json:"id"`
	BillNo      string `json:"bill_no"`
	Status      int    `json:"status"`
	SubType     string `json:"sub_type"`
	BillDate    string `json:"bill_date"`
	TotalAmount string `json:"total_amount"`
	PaidAmount  string `json:"paid_amount,omitempty"`
	CreatedAt   string `json:"created_at"`
}

type listBillsResponse struct {
	Items []BillHead `json:"items"`
	Total int        `json:"total"`
}

// AIPlan mirrors the shape of /api/v1/ai/plans items. Kept loose with
// json.RawMessage on the payload so we don't have to track every new plan
// type the orchestrator introduces.
type AIPlan struct {
	ID        string          `json:"id"`
	TenantID  string          `json:"tenant_id"`
	Type      string          `json:"type"`
	Status    string          `json:"status"`
	Payload   json.RawMessage `json:"payload"`
	Preview   json.RawMessage `json:"preview"`
	CreatedAt string          `json:"created_at"`
	ExpiresAt string          `json:"expires_at"`
}

type listPlansResponse struct {
	Items []AIPlan `json:"items"`
	Count int      `json:"count"`
}

// LowStockRow mirrors GET /api/v1/stock/alerts/low-stock items. Quantity
// fields are decimal strings — the wire contract is "preserve precision".
type LowStockRow struct {
	TenantID     string `json:"tenant_id"`
	ProductID    string `json:"product_id"`
	ProductCode  string `json:"product_code"`
	ProductName  string `json:"product_name"`
	WarehouseID  string `json:"warehouse_id"`
	OnHandQty    string `json:"on_hand_qty"`
	AvailableQty string `json:"available_qty"`
	LowSafeQty   string `json:"low_safe_qty"`
}

type listLowStockResponse struct {
	Items []LowStockRow `json:"items"`
	Count int           `json:"count"`
}

// ListStockSnapshots fetches up to limit current stock snapshots.
func (c *tallyClient) ListStockSnapshots(ctx context.Context, limit int) ([]StockSnapshot, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	q := url.Values{}
	q.Set("limit", strconv.Itoa(limit))

	endpoint := c.baseURL + "/api/v1/stock/snapshots?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("tally-mcp: build request: %w", err)
	}
	c.applyAuth(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tally-mcp: GET %s: %w", endpoint, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("tally-mcp: GET %s: status %d, body=%s", endpoint, resp.StatusCode, string(body))
	}

	var out listSnapshotsResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("tally-mcp: decode response: %w", err)
	}
	return out.Items, nil
}

// ListSaleBills fetches a page of sale-bill headers. Newest first per backend
// default ordering.
func (c *tallyClient) ListSaleBills(ctx context.Context, page, size int) ([]BillHead, int, error) {
	return c.listBills(ctx, "/api/v1/sale-bills", page, size)
}

// ListPurchaseBills fetches a page of purchase-bill headers.
func (c *tallyClient) ListPurchaseBills(ctx context.Context, page, size int) ([]BillHead, int, error) {
	return c.listBills(ctx, "/api/v1/purchase-bills", page, size)
}

func (c *tallyClient) listBills(ctx context.Context, path string, page, size int) ([]BillHead, int, error) {
	if page <= 0 {
		page = 1
	}
	if size <= 0 || size > 200 {
		size = 50
	}
	q := url.Values{}
	q.Set("page", strconv.Itoa(page))
	q.Set("size", strconv.Itoa(size))

	endpoint := c.baseURL + path + "?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("tally-mcp: build request: %w", err)
	}
	c.applyAuth(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("tally-mcp: GET %s: %w", endpoint, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, 0, fmt.Errorf("tally-mcp: GET %s: status %d, body=%s", endpoint, resp.StatusCode, string(body))
	}

	var out listBillsResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, 0, fmt.Errorf("tally-mcp: decode response: %w", err)
	}
	return out.Items, out.Total, nil
}

// ListPendingPlans fetches pending AI plans for the tenant.
// V0 default: no status filter override beyond "pending".
func (c *tallyClient) ListPendingPlans(ctx context.Context) ([]AIPlan, error) {
	endpoint := c.baseURL + "/api/v1/ai/plans?status=pending"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("tally-mcp: build request: %w", err)
	}
	c.applyAuth(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tally-mcp: GET %s: %w", endpoint, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("tally-mcp: GET %s: status %d, body=%s", endpoint, resp.StatusCode, string(body))
	}

	var out listPlansResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("tally-mcp: decode response: %w", err)
	}
	return out.Items, nil
}

// ListLowStock fetches SKUs that have dropped below their per-warehouse
// low_safe_qty threshold.
func (c *tallyClient) ListLowStock(ctx context.Context, limit int) ([]LowStockRow, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	q := url.Values{}
	q.Set("limit", strconv.Itoa(limit))

	endpoint := c.baseURL + "/api/v1/stock/alerts/low-stock?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("tally-mcp: build request: %w", err)
	}
	c.applyAuth(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tally-mcp: GET %s: %w", endpoint, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("tally-mcp: GET %s: status %d, body=%s", endpoint, resp.StatusCode, string(body))
	}

	var out listLowStockResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("tally-mcp: decode response: %w", err)
	}
	return out.Items, nil
}

// applyAuth attaches whatever credential the user configured. PAT takes
// precedence (Phase 2 path); X-Tenant-ID is the dev-mode fallback.
func (c *tallyClient) applyAuth(req *http.Request) {
	if c.pat != "" {
		req.Header.Set("Authorization", "Bearer "+c.pat)
		return
	}
	if c.tenant != "" {
		req.Header.Set("X-Tenant-ID", c.tenant)
	}
}
