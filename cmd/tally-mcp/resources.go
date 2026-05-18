package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// MCP resource URIs are namespaced under tally:// so AI agents can route
// queries by prefix. Keep these stable — they appear in user-visible config.
const (
	uriInventorySnapshots = "tally://inventory/snapshots"

	mimeJSON = "application/json"

	defaultSnapshotLimit = 200
)

// registerResources wires every read-only MCP resource Phase 1 exposes.
// Phase 3 adds: bills/sales/recent, bills/purchases/recent, alerts/low-stock,
// ai/plans/pending (see ADR-0011).
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
}

func makeInventorySnapshotsHandler(c *tallyClient) server.ResourceHandlerFunc {
	return func(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
		snaps, err := c.ListStockSnapshots(ctx, defaultSnapshotLimit)
		if err != nil {
			return nil, fmt.Errorf("inventory/snapshots: %w", err)
		}
		body, err := json.Marshal(map[string]any{
			"items": snaps,
			"count": len(snaps),
		})
		if err != nil {
			return nil, fmt.Errorf("inventory/snapshots: marshal: %w", err)
		}
		return []mcp.ResourceContents{
			mcp.TextResourceContents{
				URI:      req.Params.URI,
				MIMEType: mimeJSON,
				Text:     string(body),
			},
		}, nil
	}
}
