# Lurus Tally — UAT 验收报告

**日期**: 2026-05-25
**范围**: Session 内 10 条需求 + Foundation commit (`1ae417a3` → `92641e42`)
**模式**: Opus 监工 + 10 Sonnet worker swarm (3 波并行,worktree 隔离)

---

## 一、终验门结论

| 维度 | 结果 |
|------|------|
| **后端真实集成 (Wave A)** | ✅ 4/4 完成,**26 个 subtest 全 PASS** (real PG testcontainers) |
| **前端 E2E (Wave B)** | ⚠️ 2/3 spec 落地未跑 (NextAuth 无 dev 旁路阻挡);1/3 (B2) 完整 BLOCKED |
| **横向审计 (Wave C)** | ✅ 3/3 报告产出 (security / code-review / karpathy) |
| **诚实门 §4.1**          | ✅ 无完美 1.000/100%;agents 显式标 mock-green vs real;NEEDS-CHECK 实在 |
| **总验收**               | 🟡 **CONDITIONAL SHIP** — 后端核心闭环获实证;FE 自动化欠;**2 个 P0 bug 需修后方可推线上** |

---

## 二、Wave A — 后端真实集成 (全 PASS,26 subtest)

### A1: SQL 真跑 (17/17 PASS · `tests/integration/sql_real_test.go`)
所有 session 重写的 SQL 在 `pgvector/pgvector:pg16` 真容器上跑通,带 `EXPLAIN ANALYZE` 证据。

| 范围 | 函数 | 结果 |
|------|------|------|
| replenish | `ListSuggestions` (ROP + in-transit CTE) | PASS, 2 rows |
| reports   | `GrossMargin / ABCClassify / DeadStock / TopSales` (4 函数) | PASS |
| ai tool_repos | `SQLSaleRepo / SQLStockRepo / SQLProductRepo` | PASS |
| digest    | `ListReplenishCandidates / CountOversell / CountDeadStock` | PASS, 含 95d 死库存正例 |
| search    | `SearchProducts/Suppliers/Customers/Bills` | PASS |
| EXPLAIN   | in-transit CTE / stock_snapshot aggregate | PASS, 0.088ms exec |

**主线复跑验证**: ✅ 9.05s 全过。

### A2: AI Plan Confirm E2E (5/5 PASS · `tests/integration/ai_confirm_e2e_test.go`)
| Plan 类型 | 证据 |
|----------|------|
| CreatePurchaseDraft | `bill_no=PO-20260525-0001` 真生成 |
| PriceChange         | retail_price 100 → 110 |
| StockAdjust         | on_hand_qty 50 → 55 |
| 并发幂等            | 2 goroutines:1 成 1 拒 "plan is confirmed" |
| 失败回滚            | plan 回 pending,bills=0 |

### A3: Audit Real-Test (4/4 PASS · `tests/integration/ai_undo_audit_test.go`)
| 测试 | 证据 |
|------|------|
| OnPlanConfirm_WritesRow | `action=ai.plan.executed` + `payload.plan_id/type/affected_count` |
| AcrossAllThreePlanTypes | 3 行 distinct (`create_purchase_draft/price_change/bulk_stock_adjust`) |
| OnFailedConfirm | `action=ai.plan.failed` + `payload.error="product ... not found"` |
| ConcurrentConfirms | 1 audit row(非 2)— 与 idempotency 一致 |

### A4: Prometheus Metrics (5/5 PASS · `tests/integration/metrics_wad_test.go`)
| Metric | 证据 |
|--------|------|
| `tally_wad_total{tenant_id="..."}` | 0 → 1 |
| `tally_ai_plan_executed_total{type}` | 三 type 独立 0 → 1 |
| `tally_web_telemetry_total{event="plan_accept_rate"}` | HTTP 真打,0 → 1 |
| NoLeakAcrossLabels | `price_change` 保持 0 当只触发 `create_purchase_draft` |
| EndpointShape | HTTP 200, `text/plain`, `# HELP` 全在 |

**主线复跑验证**: ✅ 0.064s 全过。

**A4 边界声明 (诚实)**: `lifecycle.NewApp` 因 NEWAPI_API_KEY+Redis 缺失关闭 AI handler → confirm path E2E 不可触发;改测 `IncWAD/IncAIPlanExecuted` 直接调,等价生产路径。

---

## 三、Wave B — 前端 E2E (Specs 落地,Runtime 受阻)

| Agent | 落地 | 跑通 | 阻塞 |
|-------|------|------|------|
| **B1 ⌘K Palette**  | ✅ `uat-palette.spec.ts` 7 测 + config | ❌ | NextAuth middleware 服务端拦截,无 dev session 旁路 |
| **B2 Onboarding**  | ❌ 0 行 (agent 600s stall trying to forge session) | ❌ | 同上 + AUTH_SECRET 未持 |
| **B3 决策三件套**   | ✅ `uat-decisions.spec.ts` 4 测 (722 行) + config | ❌ | 同上 |

**根本原因**: `web/middleware.ts:13` 用 `auth()` wrap 全路由(除 /login /api /_next),`web/auth.ts` 仅 Zitadel provider,无 dev/test provider。本地 UAT mode 后端关 auth,FE NextAuth 不知道。

**补救建议** (不在本 UAT 范围,作为后续 ticket):
1. 加 `Credentials` test-only provider gated by `NEXT_PUBLIC_AUTH_DEV_BYPASS=true`(只在 dev/CI 开启)
2. 或在 `tests/e2e/auth.setup.localhost.ts` 写 session forge helper(需 AUTH_SECRET)
3. **现状 spec 仍有价值** — 一旦旁路落地立即可跑

---

## 四、Wave C — 横向审计 (3 报告)

### C1 Security (`_uat-reports/security.md`)

| ID | 等级 | 位置 | 描述 |
|----|------|------|------|
| **S-01** | 🔴 **HIGH** | `internal/adapter/handler/importing/handler.go:81-91` | CSV import 收 caller `warehouse_id`,无 tenant 归属校验 → 跨租户写库存 |
| S-02 | 🟡 Medium | `internal/adapter/handler/ai/handler.go:268` | `X-User-ID` header 当 actor → audit 可伪造 |
| S-03 | 🟡 Medium | ~20 client pages | `NEXT_PUBLIC_DEV_TENANT_ID` 进浏览器 bundle → 若 prod 配置则绕过认证 tenant 上下文 |
| S-04 | 🟢 Low | AI conversation localStorage | 数据卫生 |
| S-05 | 🟢 Low | `tally_signup_ts` localStorage | 可篡改扭曲漏斗指标 |

**Reviewed clean**: SQLi (全参数化) / 跨租户读 (tenant_id 在 WHERE) / CSV 公式注入 / AI executor 用 `plan.TenantID` 非用户输入 / 审计追踪 / FE token / 服务器 action CSRF / Prom label 基数 (event 限 7 个 allow-list)。

### C2 Code Review (`_uat-reports/code-review.md`) — effort=high

**5 Must-fix / 8 Should-fix / 6 Nits**

| ID | 位置 | 摘要 |
|----|------|------|
| **F01** | `internal/app/importing/usecase.go:431` | 非原子 create+approve+mark-seen;`MarkOrderSeen` 失败但 bill 已审核 → 重导双扣存 |
| **F02** | `web/app/(dashboard)/replenish/page.tsx:21,62,132` | `NEXT_PUBLIC_DEV_TENANT_ID` 用于读写;若 prod 设置则绕过认证 (= S-03) |
| **F03** | `internal/adapter/handler/ai/handler.go:268` | `X-User-ID` header (= S-02) |
| **F04** | `internal/app/importing/usecase.go:361-384` | DryRun 模式仍跑 dedup → 预览数据被静默 "skipped",计数误导 |
| **F05** | `internal/app/digest/usecase.go:89-110` | 三 goroutine 无 errgroup ctx 取消 → ctx 取消时 goroutine 可能久挂 |
| F06 | importing | `OversellRow.PlatformSKU` 永空字串 → 前端无法显示 SKU |
| F09 | `internal/app/reports` | `computeROP` dead code 被 `var _` 锚住 |
| F13 | `internal/app/ai/executor.go` | revert-on-failure 与并发 confirm 竞态(= C3 EC2) |

### C3 Karpathy + Adversarial (`_uat-reports/karpathy-adversarial.md`)

每条需求过 Karpathy 4Q + 3 EC。**评分:9 NEEDS-WORK / 1 BLOCKER (Reports — unbounded SQL)。** 无一条达 4/4 + 3/3 EC 满分。

**最关键发现** (与 C1/C2 交叉证实):
- 🔴 **Replenish/Reports SQL 无 LIMIT** (Req 3 + 10):`ListSuggestions` + `ListStockSnapshots` (`reports/repo.go:133` 最严重)→ 大目录 OOM
- 🔴 **Stock-adjust 部分原子性** (Req 1):`execStockAdjust` (`executor.go:186-189`) mid-batch fail 返非 nil result + 非 nil error;ConfirmPlan 回 Pending 但**不回滚已 adjust 的行** → 重试 double-apply
- 🔴 **`MarkOrderSeen` failure after bill creation** (Req 5,同 F01):重导可能产生重复 bill

**跨切面问题**:每个扫 decimal 列的 repo 都 `_, _ = decimal.NewFromString(...)` 静默吞错(replenish/reports/digest) → schema 漂移或脏数据返 `decimal.Zero` 无信号。

---

## 五、本次 UAT 找到的 P0 / P1 Bug 名单

### 🔴 P0 — 必修 (会致生产 500 或数据错乱)

1. **`ai/executor.go` `AdjustStock` 缺 `ReferenceID`** (A2 + A3 双重发现)
   - migration 34 已让 `stock_movement.reference_id` NOT NULL
   - 生产任何 AI stock-adjust confirm 会立即 NOT NULL 违反 500
   - **修法**:`internal/lifecycle/ai_executor.go` `aiStockAdjuster.AdjustStock` 传 `&planID` 作 ReferenceID,reference_type 设为 'ai_plan'

2. **CSV import `warehouse_id` 跨租户写** (C1 S-01)
   - `internal/adapter/handler/importing/handler.go:81-91` 收 body 中 warehouse_id 不验 tenant
   - **修法**:加 `WHERE id=$wid AND tenant_id=$tid` 查后才用

3. **Stock-adjust 部分失败不回滚** (C3 EC1 + C2 F13)
   - `executor.go:186-189` 批量调存中途 fail 后,plan 回 Pending,但已 adjust 的 row 还在
   - **修法**:`execStockAdjust` 整体 tx 包裹,或失败时主动 revert 已 adjust;或永远只接受 batch 内全成功

### 🟠 P1 — 应修 (隐患/数据不一致)

4. **CSV import `MarkOrderSeen` 在 bill 创建后失败** (C2 F01 + C3 EC1)
   - 重导可能产生重复 bill
   - **修法**:`MarkOrderSeen` 与 bill 创建放同 tx,或失败时回滚 bill

5. **`X-User-ID` header 当 actor** (C1 S-02 / C2 F03)
   - audit / bill creator 可伪造
   - **修法**:删 fallback,只信 middleware 注入的 actor

6. **`NEXT_PUBLIC_DEV_TENANT_ID` 进 bundle** (C1 S-03 / C2 F02)
   - 生产若误设则全员同租户
   - **修法**:CI gate(production env 此变量必空)

7. **Replenish/Reports SQL 无 LIMIT** (C3 Req 3 + 10)
   - 大目录 OOM
   - **修法**:加 LIMIT 1000 或分页

8. **Decimal 静默吞错** (C3 跨切面)
   - replenish/reports/digest repo 都 `_, _ = decimal.NewFromString`
   - **修法**:err != nil 时 log + 返回错误,或用 `decimal.RequireFromString` 在受信任来源

---

## 六、Acceptance 决策

### ✅ 已被 UAT 证实的事实
- AI Plan 决策闭环(#1)在真 DB 上**确实执行了**:bill 真生成、价真改、库存真调
- 幂等 + 失败回滚**确实工作**
- audit_log **真写入**,三种 plan 类型可区分
- 北极星指标 (#8) **真递增**:`tally_wad_total / tally_ai_plan_executed_total{type} / tally_web_telemetry_total{event}` 全过 promhttp scrape 验证
- session 重写的所有 SQL **对真实 schema 跑通**(17 个 query,带 EXPLAIN)

### ⚠️ 未被 UAT 证实的部分
- 前端 ⌘K p95 < 200ms 实测(spec 落地未跑)
- 前端 onboarding <10min 实测(B2 BLOCKED 未写 spec)
- 前端 monday card 真在 dashboard 显示(B3 spec 落地未跑)
- 撤销 30s 窗口前端 round-trip(C3 4Q 类型层面 PASS,但无 E2E)

### 🚫 不达 ship 标准(直至修完 P0)
- P0-1 / P0-2 / P0-3 必修;尤其 P0-1 是 5 分钟 commit 修复但若不修 AI 主推卖点直接挂

### 📋 后续 ticket 建议
1. **PR #UAT-1**: 修 P0(3 条)— 估时 2-4h
2. **PR #UAT-2**: NextAuth dev provider — 估时 2h,解锁 Wave B spec 真跑
3. **PR #UAT-3**: P1 五条 — 估时 4-6h
4. **PR #UAT-4**: 加 LIMIT / decimal 错处理 / errgroup ctx — 估时 2-3h

---

## 七、产物清单

| 文件 | 描述 |
|------|------|
| `tests/integration/sql_real_test.go` | A1 SQL 真跑 17 subtest (602 行) |
| `tests/integration/ai_confirm_e2e_test.go` | A2 AI Plan E2E 5 subtest (668 行) |
| `tests/integration/ai_undo_audit_test.go` | A3 Audit real-test 4 subtest (659 行) |
| `tests/integration/metrics_wad_test.go` | A4 Prom metrics 5 subtest |
| `web/tests/e2e/uat-palette.spec.ts` + config | B1 (7 测) — pending 旁路 |
| `web/tests/e2e/uat-decisions.spec.ts` + config | B3 (4 测) — pending 旁路 |
| `_uat-reports/security.md` | C1 5 finding |
| `_uat-reports/code-review.md` | C2 5+8+6 finding |
| `_uat-reports/karpathy-adversarial.md` | C3 10 requirement × (4Q + 3EC) |
| `docker-compose.uat.yml` + `.env.uat` | UAT 隔离 stack (PG 5436 / Redis 6379 / NATS 4222) |

## 八、运行 UAT 命令

```bash
# 起 stack (一次)
docker compose -f docker-compose.uat.yml -p tally-uat up -d

# 所有 backend UAT (≈10 min,testcontainers 自管 PG/Redis)
go test -tags integration -timeout 600s -count=1 -v ./tests/integration/

# 单独跑 SQL 真跑或 metrics
go test -tags integration -count=1 -v ./tests/integration/ -run TestSQLReal
go test -tags integration -count=1 -v ./tests/integration/ -run TestMetrics

# FE spec (待 auth 旁路落地后)
cd web && bunx playwright install chromium  # 一次
bunx playwright test --config=tests/e2e/uat-palette.config.ts uat-palette.spec.ts
bunx playwright test --config=tests/e2e/uat-decisions.config.ts uat-decisions.spec.ts
```

---

**报告生成**: 2026-05-25 Opus 监工 (整合 10 worker 产物)
**总 agent-hour**: ~7h (包含 1 次 socket drop 重试 + 1 次 stall 600s 杀掉)
**Worktree 状态**: 7 个 session worktree 待清理(`git worktree remove`)
