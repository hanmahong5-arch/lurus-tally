package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// MCP resource URIs are namespaced under tally:// so AI agents can route
// queries by prefix. Keep these stable — they appear in user-visible config.
const (
	uriInventorySnapshots = "tally://inventory/snapshots"
	uriSalesRecent        = "tally://bills/sales/recent"
	uriPurchasesRecent    = "tally://bills/purchases/recent"
	uriAlertsStockouts    = "tally://alerts/stockouts"

	mimeJSON = "application/json"

	defaultSnapshotLimit = 200
	defaultBillsPage     = 50
)

// registerResources wires every read-only MCP resource Phase 3 exposes.
//
// Deferred to Phase 3b (needs backend additions):
//   - tally://alerts/low-stock — requires joining stock_initial.low_safe_qty
//     via a dedicated endpoint
//   - tally://ai/plans/pending — needs GET /api/v1/ai/plans (only POST exists)
//
// Implemented:
//   - tally://inventory/snapshots
//   - tally://bills/sales/recent
//   - tally://bills/purchases/recent
//   - tally://alerts/stockouts (derived client-side: on_hand_qty <= 0)
func registerResources(s *server.MCPServer, c *tallyClient) {
	s.AddResource(
		mcp.NewResource(
			uriInventorySnapshots,
			"Inventory snapshots",
			mcp.WithResourceDescription("Current on-hand and available quantity per (product, warehouse). Decimal values are JSON strings to preserve precision."),
			mcp.WithMIMEType(mimeJSON),
		),
		makeInventorySnapshotsHandler(c),
	)
	s.AddResource(
		mcp.NewResource(
			uriSalesRecent,
			"Recent sale bills",
			mcp.WithResourceDescription("Most recent sale bills (newest first). Includes bill_no, status, total_amount, paid_amount, bill_date."),
			mcp.WithMIMEType(mimeJSON),
		),
		makeBillsHandler(c, kindSale),
	)
	s.AddResource(
		mcp.NewResource(
			uriPurchasesRecent,
			"Recent purchase bills",
			mcp.WithResourceDescription("Most recent purchase bills (newest first). Includes bill_no, status, total_amount, bill_date."),
			mcp.WithMIMEType(mimeJSON),
		),
		makeBillsHandler(c, kindPurchase),
	)
	s.AddResource(
		mcp.NewResource(
			uriAlertsStockouts,
			"Stockout alerts",
			mcp.WithResourceDescription("Products whose on_hand_qty has reached zero or negative. Derived client-side from inventory snapshots; no separate backend endpoint required."),
			mcp.WithMIMEType(mimeJSON),
		),
		makeStockoutsHandler(c),
	)
}

func makeInventorySnapshotsHandler(c *tallyClient) server.ResourceHandlerFunc {
	return func(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
		snaps, err := c.ListStockSnapshots(ctx, defaultSnapshotLimit)
		if err != nil {
			return nil, fmt.Errorf("inventory/snapshots: %w", err)
		}
		return wrapJSON(req.Params.URI, map[string]any{
			"items": snaps,
			"count": len(snaps),
		})
	}
}

type billKind int

const (
	kindSale billKind = iota
	kindPurchase
)

func makeBillsHandler(c *tallyClient, kind billKind) server.ResourceHandlerFunc {
	label := "sales"
	fetch := c.ListSaleBills
	if kind == kindPurchase {
		label = "purchases"
		fetch = c.ListPurchaseBills
	}
	return func(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
		items, total, err := fetch(ctx, 1, defaultBillsPage)
		if err != nil {
			return nil, fmt.Errorf("bills/%s/recent: %w", label, err)
		}
		return wrapJSON(req.Params.URI, map[string]any{
			"items":     items,
			"count":     len(items),
			"total":     total,
			"page_size": defaultBillsPage,
		})
	}
}

func makeStockoutsHandler(c *tallyClient) server.ResourceHandlerFunc {
	return func(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
		snaps, err := c.ListStockSnapshots(ctx, defaultSnapshotLimit)
		if err != nil {
			return nil, fmt.Errorf("alerts/stockouts: %w", err)
		}
		out := make([]StockSnapshot, 0, len(snaps))
		for _, s := range snaps {
			// Decimal strings — parse defensively. A value that can't be parsed
			// is treated as "not a stockout" rather than crashing the resource.
			f, err := strconv.ParseFloat(s.OnHandQty, 64)
			if err == nil && f <= 0 {
				out = append(out, s)
			}
		}
		return wrapJSON(req.Params.URI, map[string]any{
			"items":     out,
			"count":     len(out),
			"threshold": "on_hand_qty <= 0",
		})
	}
}

// wrapJSON marshals payload as application/json and wraps it in the single
// TextResourceContents slice every resource handler in this binary returns.
func wrapJSON(uri string, payload any) ([]mcp.ResourceContents, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}
	return []mcp.ResourceContents{
		mcp.TextResourceContents{
			URI:      uri,
			MIMEType: mimeJSON,
			Text:     string(body),
		},
	}, nil
}
