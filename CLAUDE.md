# Lurus Tally (2b-svc-psi)

AI-native 智能进销存 SaaS (Web only)，面向中小企业（制造/批发/零售/电商）。Platform 产品组 (P0)。domain `tally.lurus.cn`（stage R6 `tally-stage.lurus.cn`），ns `lurus-tally`。Go 1.25 + Gin + GORM / Next.js 14 + shadcn/ui + Bun。License Apache-2.0。Migration head 48；RLS Wave 3 strict-flip 全租户表生效。

## Commands

```bash
# Backend
go run ./cmd/server                         # :18200
go test -v ./...
CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -trimpath -o tally ./cmd/server

# Frontend
cd web && bun install && bun run dev
cd web && bun run build && bun run lint
```

> 真源/细节: 端口/域名/schema/capabilities `lurus.yaml` · 契约(NATS PSI_EVENTS / Stock REST / Billing / cross-service deps) `doc/coord/contracts.md` · migration `doc/coord/migration-ledger.md` · 策略/roadmap/3 条产品红线 `_bmad-output/planning-artifacts/` · 开发手册 `/tally-dev` skill · 历史叙事 `doc/claude-md-archive-2026-06-10.md`。
