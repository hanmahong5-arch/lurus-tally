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
