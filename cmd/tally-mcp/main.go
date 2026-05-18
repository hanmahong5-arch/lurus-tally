// Command tally-mcp exposes Tally inventory data as an MCP (Model Context
// Protocol) stdio server. AI agents (Claude Desktop, Cursor, OpenHuman, Cline,
// Continue, …) can attach and query the user's inventory in natural language.
//
// V0 scope: read-only resources backed by the existing Tally REST API.
// Auth: dev-mode X-Tenant-ID header; PAT (Personal Access Token) bearer is
// the Phase 2 deliverable per ADR-0011.
//
// Run locally:
//
//	export TALLY_BASE_URL=http://localhost:18200
//	export TALLY_TENANT_ID=<uuid>
//	go run ./cmd/tally-mcp
//
// Claude Desktop / OpenHuman config examples live in ADR-0011
// (lurus/doc/decisions/0011-tally-mcp-server.md).
package main

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/mark3labs/mcp-go/server"
)

const serverName = "tally-mcp"

// serverVersion is overridden at build time via:
//
//	go build -ldflags="-X main.serverVersion=<tag>" ./cmd/tally-mcp
var serverVersion = "dev"

type config struct {
	BaseURL  string // TALLY_BASE_URL — e.g. https://tally.lurus.cn
	TenantID string // TALLY_TENANT_ID — dev-mode tenant UUID (Phase 1)
	PAT      string // TALLY_PAT — Phase 2 long-lived bearer; takes precedence over TenantID when set
}

func loadConfig() (config, error) {
	c := config{
		BaseURL:  strings.TrimRight(os.Getenv("TALLY_BASE_URL"), "/"),
		TenantID: os.Getenv("TALLY_TENANT_ID"),
		PAT:      os.Getenv("TALLY_PAT"),
	}
	if c.BaseURL == "" {
		return c, fmt.Errorf("TALLY_BASE_URL is required (e.g. https://tally.lurus.cn or http://localhost:18200)")
	}
	if c.PAT == "" && c.TenantID == "" {
		return c, fmt.Errorf("either TALLY_PAT (Phase 2) or TALLY_TENANT_ID (dev mode) must be set")
	}
	return c, nil
}

func main() {
	// stdio is reserved for the JSON-RPC framing; logs must go to stderr to
	// keep the MCP protocol stream clean.
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	cfg, err := loadConfig()
	if err != nil {
		slog.Error("tally-mcp: config error", slog.Any("error", err))
		os.Exit(1)
	}

	client := newTallyClient(cfg)

	s := server.NewMCPServer(
		serverName,
		serverVersion,
		server.WithResourceCapabilities(true, false),
		server.WithLogging(),
	)

	registerResources(s, client)

	slog.Info("tally-mcp: starting stdio server",
		slog.String("base_url", cfg.BaseURL),
		slog.String("auth_mode", authMode(cfg)),
	)

	if err := server.ServeStdio(s); err != nil {
		slog.Error("tally-mcp: stdio server exited", slog.Any("error", err))
		os.Exit(1)
	}
}

func authMode(c config) string {
	if c.PAT != "" {
		return "pat"
	}
	return "dev-tenant-header"
}
