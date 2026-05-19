# Lurus Tally — V1.5 进化路线图 (Release R1)

> 2026 H2 · Sprint 0 (52h pre-budget) + 12 sprint × 2 周 = 6 个月
> Source: 3 份联网调研（国内 SMB / 海外 AI ERP / AI workspace UX）+ 3 角色 Plan swarm + 8 维度自评（2026-05-18 重写）
> 创建：2026-05-18 · 重写：2026-05-18（4-stage flat → 3-track parallel）
> 状态：APPROVED — 待 BMAD story 拆解执行
> 关联：`./epics.md` V2.5 横向增强带（E21-E27）/ `./roadmap-ux-supplement.md` / `./assumptions.md`
> 旧版（4-stage flat）已归档：`./_archive/roadmap-v1.5-flat-stages-2026-05-18.md`

---

## Context — 为什么写这份计划

**产品现状（2026-05-18）**:
- Tally 后端核心闭环已就绪：snapshots / movements / bills / stockouts / low-stock / AI replenishment plans + 库存 UI 完
- tally-mcp Phase 3c 刚落地：GitHub Release pipeline + lurus.yaml capability entry（commits cca50454 / 98b9eea / a627424）
- **未上线任何商业客户**；STAGE 已在 R6 跑 24d (`tally-stage.lurus.cn` /ready 200), ArgoCD 已绕开走人工 apply (ADR-0006); 真实可用性 (登录链路 / OIDC client_secret 是否 PKCE-only) 待端到端验证
- V1 砍枝已 lock：Epic 12-16 离线/边缘延后到 V2.5

**问题**: 进销存赛道竞品成熟（聚水潭年迭代 400 次、用友/金蝶下沉中、海外 Cogsy/Settle 卖 $49-199），若按"做完 Epic 1-20"思路推进,会变成又一个"功能全但用户没感觉"的产品。本计划用调研 + swarm + 8 维度自评把"做什么 / 不做什么 / 卖给谁 / 怎么验证"钉死。

**为什么重写（2026-05-18 8 维度自评）**:

| Dim | 分 | 痛点 |
|---|---|---|
| 5 CI/CD & DevOps | **5.0** | `release.yaml` 不依赖 CI workflow → image publish 没 gate；只有 2 个 telemetry event；LLM 成本无 Prom metric |
| 8 客户/市场验证 | **3.5** | 0 真实客户；H1/H2/H3 没 owner/deadline；onboarding first-PO = 45+ min（target 30 min）|
| 6 部署成熟度 | 6.5 | 无 PG backup；ArgoCD bypass (ADR-0006)；nodes/probes 已合规 |
| 4 工程纪律 | 7.5 | 诚实约束七则有执行但没 owner 把假设跑成数据 |

旧 roadmap 4-stage flat 结构只走 Track P（产品 feature），Q + C 全藏在脚注，导致 5.0 + 3.5 两个底分没人治。**全面改进 = 把 Q + C 提升到第一公民并行轨**，3 轨并行穿透 12 sprint。

具体代码 gap（Explore 实勘）：
- `.github/workflows/release.yaml` 第 25 / 87 行：`image-backend` + `image-web` 无 `needs:` → lint 红镜像照发
- `internal/pkg/llmclient/client.go:232`：parse 了 `Usage{prompt_tokens, completion_tokens}` 但不 log 也不 metric
- `web/lib/telemetry.ts:8`：`TelemetryEvent` union 只含 2 种（`draft_restore` / `undo_used`）
- `/metrics` Prometheus endpoint 整个不存在
- 无 `pg_dump` cron / WAL archiving
- `internal/adapter/handler/router/router.go`：缺 `POST /api/v1/suppliers` + `POST /api/v1/warehouses` → 客户拼不出第一张 PO
- 无 CSV import；无 demo seed（除 horticulture nursery）；无 tour 库；无 feedback widget
- 18 dashboard 路由全实现 ✓；HTTP probe 已 path-based 合规 ✓；migration up/down 全配对 ✓

---

## 调研摘要

### A. 国内 SMB 竞品（聚水潭 / 旺店通 / 金蝶星辰 / 用友 T+ / 简道云 / 管家婆）
- **最佳 UX**: 聚水潭多平台合单（200+ 平台秒级同步、企微预警）/ 金蝶星辰 OCR 票据→自动凭证→一键报税 / 旺店通波次拣货 + 3D 路径（错拣率 0.3%）
- **8 大价值特性**: 多平台聚合 / 智能预警 / OCR 对账 / 一键报税 / 低代码表单 / WMS 拣货 / 跨国会计 / NLP 查询
- **5 大痛点**: ① 幽灵库存超卖（赔 5% 尾款）② 拣货错拣率 2-3% ③ 财务对账 5-7 天 ④ 预警噪声大被关掉 ⑤ 集成需 ¥1-3万 + 2-4 周
- **赛道格局**: 传统巨头下沉 / 聚水潭增速最快 / 简道云轻流 AI 爆发 / 跨境垂直挤压严重

### B. 海外 AI-native ERP（Cogsy / Inventory Planner / Settle / Katana KAI / Cin7 / Zoho / Pivot）
- **价格锚**: Cogsy $49/月节省 20h/周；Settle $199/月节省 10h/周
- **AI 浓度排序**: Inventory Planner ★★★★★ > Cogsy ★★★★ ≈ Settle ★★★★ ≈ Katana ★★★★ > Cin7 ★★★
- **避坑("伪洋玩意儿")**: 复杂 MRP/BOM、多账簿多准则、多层级审批链、黑盒预测算法、Omnichannel 全链路
- **国内独占价值**: 钉钉/企微集成、金税对接、淘宝/拼多多/抖音 SKU 同步、暗黑数据可视化

### C. AI workspace + UX 灵感（Linear / Superhuman / Cursor / Claude Projects / Notion AI / Raycast / Stripe / Vercel / Attio）
- **AI 嵌入工作流 5 范式**: Palette-First / Sidebar Drawer / MCP Tool Approval / Project-Scoped Context / Inline Automation
- **⌘K 进化光谱**: v1 fuzzy → v2 command+entity → v3 AI NL fallback → v4 multi-step agent；**Tally 目标 = v3**
- **30 秒 aha 三角色首屏**: 采购员（缺货红卡）/ 仓管（入库大字 + 扫码 60% 屏）/ 老板（毛利 + 周转率 + 缺货丢单红警）
- **Agent-native 3 护栏**: Preview Before Execute / Audit Trail / 权限隔离

---

## 哲学定锚 — 3 条流血也不让步的产品原则

**① AI 不是卖点,自动化才是。**
卖"老板每周省 8 小时",不卖"我们家用了大模型"。AI 在用户看不见的地方扣库存、在最焦虑的时刻递补货建议。
> 违反样子：首页放 "AI Powered ✨" badge,AI Drawer 塞个"问我任何问题"空输入框,用户问 3 次没 actionable 答案后再不打开。

**② ⌘K 是肌肉记忆,不是搜索框。**
⌘K 在 200ms 内出 command + entity + AI 三栏。日活渗透率必须高过侧边导航点击率,否则失败。
> 违反样子：60 天后 ⌘K DAU < 30%,老板说"那个搜索框我没用过"。

**③ 每一次 AI 写库存,都要可预览、可撤销、可审计。**
抄海外护栏,不抄黑盒预测。所有 AI/Agent 写操作：执行前 delta 预览 → 执行后写 audit trail（who/what/why/when JSON）→ 30 秒内可一键 Cmd+Z。
> 违反样子：Kova 自动生成 PO 落库,老板第二天发现重复采购 80 万、找不到记录、撤不回 → Tally 死在客户名声里。

---

## ICP 锁定 — 第一个客户

**跨境电商 3-8 人精品工作室**（深圳 / 广州 / 义乌 / 杭州）

| 维度 | 具体 |
|---|---|
| 年营收 | ¥300-1500 万（月 ¥30-120 万） |
| SKU 数 | 80-400 个活跃 SKU（精品路线,**不**铺货） |
| 平台 | Amazon US/EU 主战场 + Shopify + 偶尔 TikTok Shop |
| 当前系统 | Excel + 钉飞表格 + Amazon 后台手工拉报表；试过店小秘/马帮"太重"；金蝶精斗云和库存对不上 |
| 拍板人 | 老板本人就是采购,决策周期 = 1 通电话 |
| 痛点冤枉时间 | 每月 25-40h 对账+算补货；最近 Amazon 超卖被扣 ¥1.2 万；FBA 补货靠"感觉",去年砸 ¥18 万死库存 |
| 为何愿意试 | 聚水潭对它太"内贸批发"、店小秘界面像 2015 年、Cogsy 英文 + 美金 + 不接 1688；要"中文 + 懂 FBA 周期 + 30 分钟上手 + < ¥500/月" |

**不要的客户**: 5000+ SKU 铺货大卖 / 纯 1688 内贸（聚水潭已强）/ 年营收 5000 万+（要 ERP 不是 Tally）。

**定价锚**: ¥299/月年付（¥3588/年）/ ¥399/月月付；前 10 客户**免费 90 天**（FBA 周期就是 60-90 天,14 天看不到 aha）。

---

## 结构变化总览

| 旧（4-stage flat） | 新（3-track parallel + Sprint 0） |
|---|---|
| Stage 1-4 × 4 feature | Track P（产品 16 feature）+ Track Q（质量）+ Track C（客户）穿过 12 sprint |
| 总 1014h ÷ 12 sprint | 1014h P + 180h Q + 150h C = 1344h（envelope 上限），Sprint 0 = 52h pre-budget |
| 客户 S6 才到 3 家 | 客户 **S4 = 3 家 / S6 = 5 家 / S7 = 10 家** 前移 |
| KS 在 doc 提了没 wiring | KS 三个 alert 写进 `prometheus-rules.yaml` 自动发飞书 |
| H1/H2/H3 假设无 owner | `assumptions.md` schema + `bin/assumption-snapshot.sh` 日跑 |

---

## Sprint 0 — Foundation Week (52h, pre-budget, week -1)

**为什么**：S0-Q1/Q2/Q3/Q4 是 Track P 后续 feature 的 gate，没它们 Sprint 1 仍然走旧的"lint 红镜像照发 + 无 telemetry"路径。**S0 不过 → 不开 S1**。

| ID | 任务 | 工时 | Owner | Exit 标准 |
|---|---|---|---|---|
| **S0-Q1** | `release.yaml` `image-backend` + `image-web` 加 `needs: [backend-lint, backend-test, frontend]`；故意 push 一个 `errcheck` 违规验证 image push 被 block | 6h | devops | red lint 提交 → image build job skipped → 验证一次后 revert |
| **S0-Q2** | 新建 `internal/pkg/llmgateway/metrics.go`：注册 `tally_llm_cost_cny_total{tenant,model}` + `tally_llm_tokens_total{tenant,model,direction}`；在 `internal/pkg/llmclient/client.go:232` 解析完 `Usage` 后 emit；router 暴露 `/internal/v1/metrics` | 12h | backend | `curl /internal/v1/metrics` 见 2 metric；integration test 验证 counter increment |
| **S0-Q3** | `web/lib/telemetry.ts:8` union 扩展 5 个事件：`palette_invocation`、`ai_drawer_open`、`plan_accept_rate`、`onboarding_first_po_exported`、`cmd_z_used`；`web/app/api/otel-events/route.ts` 把 event 写到 NATS `PSI_TELEMETRY.web.*` typed publisher | 10h | full-stack | Playwright 触发 5 event 全部到 NATS subject |
| **S0-Q4** | `deploy/k8s/base/cronjob-pgbackup.yaml`：daily 02:00 UTC `pg_dump → MinIO`；14d 保留；`STAGE_RUNBOOK.md` 加 restore drill 章节 | 8h | devops | 在 STAGE 跑 `kubectl create job --from=cronjob/tally-pgbackup test-restore` 生 dump >1MB；restore drill 实际跑通一次 |
| **S0-C1** | `_bmad-output/planning-artifacts/assumptions.md`：H1/H2/H3 schema（owner / deadline / threshold / evidence_source / current_value / status / last_evidence_url）；3 个假设 owner 签字 | 4h | founder | 文件提交；H1/H2/H3 owner 三栏全填非空 |
| **S0-C2** | `bin/assumption-snapshot.sh`：每天 scrape Prom + manual fields → 写入 assumptions.md current_value；cron `0 8 * * *` 在 governance 机执行 | 8h | devops | 跑一次后 assumptions.md 有 timestamp 当前值 |
| **S0-C3** | `bin/health-report.sh`：周一发飞书周报，含 features_shipped / bugs_open / assumption_evidence_gap_days / lint_warnings_total / feature-to-WAD ratio | 4h | devops | 第一份周报发出（即使数据全 0/N/A）|

**Sprint 0 exit gate**（4 个全过才开 S1）：
1. 红 lint commit → image build job skipped
2. `/internal/v1/metrics` 返 2 个 LLM metric counter
3. Playwright 触发 5 个新 event 全到 NATS
4. PG backup CronJob 跑一次 + restore drill 通

**风险**：`backend-integration` job 本身可能不绿，加 `needs:` 会自锁；S0-Q1 落地前必须先 `gh run list --workflow=ci.yaml --limit=10` 确认 integration job 在 main 上是绿的，否则先治 integration 再加 gate。

---

## 12 Sprint × 3 Track 实施表

> 每 sprint 列：Track P / Q / C + Exit Gate + Risk。工时上限 = 2 dev × 56h × 70% = ~157h/sprint。

### Sprint 1 (weeks 1-2) — STAGE 收口 + Palette 起步 + Supplier/Warehouse CRUD
| Track | 任务 | 工时 |
|---|---|---|
| **P** | F1.1 STAGE end-to-end 验证（4h, devops）+ F1.2 Palette v3 前 60%（30h, frontend；文件 `web/components/command-palette/Palette.tsx` + `groups.ts`）| 34h |
| **Q** | `golangci-lint run ./... > _research/lint-baseline.txt`；治 top 3 包的 `errcheck` 违规 | 10h |
| **C** | 新建 `internal/adapter/handler/{supplier,warehouse}/handler.go` + 在 `router.go` 注册；`POST/GET/PATCH/DELETE /api/v1/{suppliers,warehouses}`；RLS policy integration test | 12h |
| **Exit** | STAGE 真账号能 `POST /api/v1/suppliers` + `POST /api/v1/warehouses` + Palette 唤起 <200ms（由新 `palette_invocation.latency_ms` event 实测）|  |
| **Risk** | 新 CRUD 端点无 rate limit 可能暴露 tenant 枚举，必须加 RLS test |  |

### Sprint 2 (weeks 3-4) — Palette 收尾 + 暗黑模式 + Audit log 扩列 + CSV import
| Track | 任务 | 工时 |
|---|---|---|
| **P** | F1.2 Palette 收尾（20h frontend）+ F1.3 暗黑模式库存色板（50h frontend）| 70h |
| **Q** | Migration 000032：`tally.audit_log` 加列 `model / tokens_in / tokens_out / latency_ms / plan_id / source enum('chat','palette','mcp','agent')`；index `(tenant_id, source, created_at DESC)` | 12h |
| **C** | `POST /api/v1/products/import` CSV endpoint；解析 SKU 表 + Amazon Business Report CSV；3 个 golden-file regression test | 14h |
| **Exit** | 新用户 supplier → warehouse → CSV-import <15 min 完（`onboarding_csv_import_completed_at - onboarding_started_at` 实测）|  |
| **Risk** | CSV 列名映射脆，1 个 Amazon report 格式变就崩 → 必须 fixture 3 个客户脱敏 CSV |  |

### Sprint 3 (weeks 5-6) — AI Drawer + Tour + Demo seed
| Track | 任务 | 工时 |
|---|---|---|
| **P** | F1.4 AI Drawer v0.5（80h，split 40 backend + 40 frontend；文件 `internal/adapter/handler/ai/` + `web/components/ai-assistant/Drawer.tsx`）| 80h |
| **Q** | `staticcheck` + `ineffassign` 违规清零（8h backend）；Grafana dashboard provisioned via `configmap.yaml` 暴露 LLM metric（4h devops）| 12h |
| **C** | Driver.js tour 集成到 `web/app/(dashboard)/layout.tsx`；首 tour = `setup → suppliers → warehouses → import → AI Drawer hover`（12h frontend）+ `migrations/data/cross_border_seed.sql`（mirror nursery_seed pattern，Amazon/Shopify SKU + 1688 supplier + FBA warehouse，8h backend）| 20h |
| **Exit** | **STAGE-1 gate**：5 个内部账号 30 min 内走完 tour P95；AI Drawer 答 3 个 page-context 问相关性 ≥4/5（n=15 内测）；Palette + Drawer event 在 Grafana 可见 |  |
| **Risk** | Tour overlay z-index 与 Drawer slide-in 冲突 — 需 coordinator hook |  |

### Sprint 4 (weeks 7-8) — Kova 补货 + 首 3 客户 + LLM rate limit
| Track | 任务 | 工时 |
|---|---|---|
| **P** | F2.1 Kova 补货 v1（80h，56 backend + 24 frontend）；migration 000033 `replenishment_suggestion`；新 `internal/domain/replenishment/` + `internal/app/replenishment/{engine,narrator,po_builder}.go` | 80h |
| **Q** | `internal/pkg/llmgateway/ratelimit.go`：默认 60req/min/tenant；emit `tally_llm_rate_limit_dropped_total` | 12h |
| **C** | **首 3 lighthouse 客户落地**（founder 时间，跨境 3-8 人精品工作室画像）；demo seed 已 S3 落地确保 onboarding 30 min 内能跑完 | 16h founder |
| **Exit** | 3 客户登陆 STAGE，每家 import ≥50 SKU，AI 至少为每客生 1 条 replenishment suggestion，至少有 1 次 "导出 PO" 点击（`onboarding_first_po_exported` event）|  |
| **Risk** | 首客真实 Amazon CSV 撞破 S2 parser → 预留 2 天 emergency parser fix buffer |  |

### Sprint 5 (weeks 9-10) — Timeline + Preview-Before-Execute + Feedback widget
| Track | 任务 | 工时 |
|---|---|---|
| **P** | F2.2 stock movement timeline（80h，40 backend + 40 frontend）+ F2.3 Preview-Before-Execute 前 80%（40h backend）| 120h |
| **Q** | Audit-log 180d retention CronJob `deploy/k8s/base/cronjob-audit-retention.yaml`（6h devops）+ `tally_audit_log_rows_total{source}` gauge（4h backend）| 10h |
| **C** | Crisp 反馈 widget，feature-flag + lazy-load，只 STAGE+PROD（8h frontend）；founder 与首 3 客户做 1v1 周话（founder 时间）| 12h |
| **Exit** | 所有 AI 写操作通过 `/api/v1/ai/plans/:id/confirm` 走且写 audit-log row 含 `plan_id, model, tokens`；integration test 断言 source='agent' 行 `plan_id` 0 NULL |  |
| **Risk** | Crisp 加载 3rd-party JS 拉低 Lighthouse 分（dim 5 反噬）→ 必须 lazy-load 且 feature flag 可关 |  |

### Sprint 6 (weeks 11-12) — 多平台订单聚合 + Preview 收尾 + 客户 4-5
| Track | 任务 | 工时 |
|---|---|---|
| **P** | F2.4 多平台订单聚合（120h，80 backend + 40 frontend；migration 000034-36 `platform_channel/order/order_line`）+ F2.3 Preview 收尾（10h）| 130h |
| **Q** | `tally_platform_sync_lag_seconds{platform}` histogram + `tally_platform_oversell_total{platform}` counter → 接 KS alerting | 10h |
| **C** | 客户 #4-5 onboarding；`bin/assumption-snapshot.sh` 加 H3 日算（`palette_invocation_dau / total_dau`, `ai_drawer_open_dau / total_dau`）写入 assumptions.md | 12h |
| **Exit** | **STAGE-2 gate**：5 客户 on STAGE，WAD ≥10/周（`plan_accept_rate` event 实测），`tally_platform_oversell_total` 连续 14d = 0 |  |
| **Risk** | 拼多多/抖音 token bucket 在生产配错会冻结 sync 数小时 → 加 chaos test 模拟 429 级联 |  |

### Sprint 7 (weeks 13-14) — Time Machine + Role Atrium 起 + 10-客户里程碑
| Track | 任务 | 工时 |
|---|---|---|
| **P** | F3.1 Time Machine（60h，24 backend + 36 frontend）+ F3.2 Role Atrium 前 40%（30h frontend）| 90h |
| **Q** | `cmd_z_used` event payload schema review（含 `entity_type, undo_latency_ms`，4h frontend）；Grafana Q-dashboard 加 panel（4h devops）| 8h |
| **C** | **客户 #6-10 onboarding**（founder 时间）；demo seed 扩展低库存/几乎超卖/多币种场景（4h backend）| 20h |
| **Exit** | 10 客户活跃；day-14 cohort 留存 ≥60%（`dau_by_signup_cohort` 日 snapshot）；Cmd+Z 月调用 ≥5 次/活跃 |  |
| **Risk** | Time Machine IndexedDB 配额浏览器差异大（Chrome 80% / Safari 40%）→ quota-exceeded 回退 in-memory store |  |

### Sprint 8 (weeks 15-16) — Hub NLQ + Atrium 收尾 + NLQ 红队
| Track | 任务 | 工时 |
|---|---|---|
| **P** | F3.3 Hub NLQ MVP（120h，56 backend + 64 frontend）+ F3.2 Atrium 收尾（40h frontend）— 总 160h **借 S10 slack 48h** | 160h |
| **Q** | `internal/pkg/llmgateway/sqlguard.go`：NLQ 生成 SQL 必过 regex + AST allow-list；emit `tally_nlq_blocked_total{reason}` | 12h |
| **C** | H1/H2/H3 evidence freeze checkpoint：founder review assumptions.md snapshot；任一 red 连 2 周 → pivot 会议 | 4h founder |
| **Exit** | 50 条真实客户 NLQ 准确率 ≥80%（manual eval）；benign query `tally_nlq_blocked_total{reason='injection'}` 假阳 0；H1/H2/H3 dashboard 显示过去 56d 日值 |  |
| **Risk** | NLQ SQL guard 是 V1.5 最高 security 风险点 → 任何客户开 NLQ 前安排 1 天内部红队 |  |

### Sprint 9 (weeks 17-18) — 微信/钉钉 push + 分布式 trace + STAGE-3
| Track | 任务 | 工时 |
|---|---|---|
| **P** | F3.4 微信/钉钉 push（50h，30 backend + 20 frontend）| 50h |
| **Q** | OTel SDK wire 在 `router.go` middleware（16h backend）；Tempo 配在 `configmap.yaml`；scaffold trace 跨 NLQ + replenishment 链 | 16h |
| **C** | 10 客户 retention 30-min 访谈每家；transcript tagged 对 H1/H2/H3 | 16h founder |
| **Exit** | **STAGE-3 gate**：10 客户 WAD ≥40/周累计，push confirm 转化 ≥30%，留存 ≥60% at d14 |  |
| **Risk** | WeChat Work API 需 per-tenant secret 加密存 → 复用 `platform_channel.creds` AES-GCM pattern（migration 000034 已建）|  |

### Sprint 10 (weeks 19-20) — Trust Center + OCR 起 + KS 告警上线
| Track | 任务 | 工时（S8 借走 48h，剩 ~54h）|
|---|---|---|
| **P** | F4.1 Trust Center 审计页（50h，30 backend + 20 frontend）+ F4.2 OCR 前 57%（40h backend）| 90h |
| **Q** | `deploy/k8s/base/prometheus-rules.yaml`：3 KS alert 上线 — `tally_onboarding_completion_rate < 0.4`、`tally_po_accept_rate_d45 < 0.2`、`tally_trial_conversion_d90 < 0.3`；接飞书 webhook | 8h |
| **C** | 启动付费转化：首 3 个客户被问 ¥299/月签约意向 | 4h founder |
| **Exit** | 3 KS alert 在 synthetic 阈值越界时全发飞书；Trust Center 显示任意租户过去 90d audit-log diff |  |
| **Risk** | OCR 上 Tencent Cloud API 而非嵌 ML → 防 Python 依赖污染 Go-only 部署 |  |

### Sprint 11 (weeks 21-22) — OCR 收尾 + 性能基线 + Lighthouse CI
| Track | 任务 | 工时 |
|---|---|---|
| **P** | F4.2 OCR 收尾（30h）+ F4.3 perf 基线 + 虚拟滚动（40h）| 70h |
| **Q** | Lighthouse CI step 接 `.github/workflows/ci.yaml`：PR `/dashboard` perf <80 阻塞合并；median-of-3 防 flake | 8h |
| **C** | 第 2 波付费转化（founder 时间）；`_bmad-output/planning-artifacts/customer-success-runbook.md` 起草 | 12h |
| **Exit** | Lighthouse CI 在 main 绿；10k SKU 表 60fps 渲；≥3/10 客户口头承诺付费 |  |
| **Risk** | Lighthouse CI 在 GitHub-hosted runner 抖（CPU 噪声）→ median-of-3 + soft threshold |  |

### Sprint 12 (weeks 23-24) — Billing meter + R1 收口
| Track | 任务 | 工时 |
|---|---|---|
| **P** | F4.4 Billing × Usage Meter（40h，20 backend + 20 frontend；同时复用 S0-Q2 metric）| 40h |
| **Q** | Final lint/test coverage 审计；`coverage.out` badge 发布；target ≥60% | 8h |
| **C** | 90d cohort H1/H2/H3 最终评分；cohort report；R2 planning kickoff | 12h founder |
| **Exit** | **R1 收口 gate**：WAD ≥80 累计；≥3 付费；H1/H2/H3 每个标 truthy / falsified / inconclusive + evidence URL；CI/CD 子分 ≥7.5；客户子分 ≥6.0 |  |
| **Risk** | Sprint 12 进 feature freeze，客户会要 custom 功能 → 严守不做客户定制红线 |  |

---

## Track 之间的 Q-before-P 依赖矩阵

| P feature | 依赖 Q | 为什么 |
|---|---|---|
| F1.2 Palette v3（S1）| S0-Q3 `palette_invocation` event | ⌘K DAU ≥60% 没埋点测不了 |
| F1.4 AI Drawer（S3）| S0-Q2 LLM cost metric | Drawer 是首个持续 LLM 消费者，没 metric 证不了 ¥3.6/租户/月 |
| F2.1 Kova 补货（S4）| S0-Q2 metric + S4-Q rate limiter | Kova 批量 LLM，没限流会被 newapi 反向限 |
| F2.3 Preview-Before-Execute（S5）| S2-Q audit_log 扩列 | Preview 必须 persist plan-id → audit-log |
| F3.1 Time Machine（S7）| S0-Q3 `cmd_z_used` event + S7-Q payload | "Cmd+Z 月调用 ≥5" 实测要靠埋点 |

---

## 与现有 Epics 映射

| F-Feature | 关联 Epic | 关系 |
|---|---|---|
| F1.1 | — | Ops 验证, 不属 BMAD epic |
| F1.2 ⌘K v3 | E18 (Palette) | 升级 v2→v3 |
| F1.3 暗黑色板 | E11 (Onboarding) | UI polish |
| F1.4 AI Drawer | E17 (Kova Agent) 前置 | Drawer 是 AI 落地容器 |
| F2.1 Kova 补货 v1 | E17 | 主线落地 |
| F2.2 库存时间线 | E26 (信任审计) + E17 | AI 溯源是审计可视化 |
| F2.3 Preview 护栏 | E21 (可恢复性) + E17 | 跨 epic 横向 enabler |
| F2.4 多平台聚合 | E27.4 (电商同步) | V2.5 提前到 V1.5 |
| F3.1 Time Machine | E21 | S21.1+21.2+21.3 完整落地 |
| F3.2 Role Atrium | E11.* (Onboarding) | persona 首屏分流 |
| F3.3 Hub NLQ | E19 | V3 提前到 V1.5 |
| F3.4 微信钉钉 | E25.3 | V2.5 提前到 V1.5 |
| F4.1 Trust Center | E26.1 | 信任审计核心 |
| F4.2 OCR | E23.5 | V2.5 提前 |
| F4.3 性能基线 | E22 | V2.5 提前 |
| F4.4 Usage Meter | E10.1 后续 | Billing 集成扩展 |

---

## 5 个核心 Feature 技术架构（Migration 32-60 区间）

> 当前 head=30, PAT 31。**核心复用**: advisory lock / audit_log / stock_movement append-only / LLM client / AI orchestrator / Palette / Drawer / FIFO-WAC 计算器 / NATS typed publisher。

### F2.4 多平台订单聚合 — Backend 80h + FE 24h
- **Tables (32-34)**: `platform_channel` (creds AES-GCM 加密)、`platform_order` (idempotency UNIQUE)、`platform_order_line`；ALTER `stock_movement.reference_type` CHECK 加 `'platform_order'`
- **Packages**: `internal/domain/channel/` + `internal/app/channel/{sync_usecase,reserve_usecase}.go` + `internal/adapter/platform_ext/{taobao,pdd,douyin,a1688,shopify}/`
- **NATS**: 新 subject `PSI_EVENTS.platform.order_received/cancelled`
- **复用**: `internal/adapter/repo/stock/advisory.go` (FNV-64a + `pg_advisory_xact_lock`)
- **风险**: 拼多多/抖音 API QPS≤10 必须 token bucket + 退避；超卖根因 80% 是 webhook 重复推送 → idempotency UNIQUE 是底线

### F2.1 Kova 补货 Agent — Backend 56h + FE 24h
- **Table (35)**: `replenishment_suggestion` (suggested_qty, supplier_id, reasoning, status, expires_at, po_bill_id)
- **Engine**: 纯 Go EWMA + 季节因子（< 200 行）, **不上 Prophet**（Python 依赖太重）
- **Narrator**: 数字结果发 LLM 生成自然语言（节省 token：不发原始历史）
- **复用**: `internal/app/ai/orchestrator.go` 的 Plan 二次确认；`stock` 的 `AvgDailySales`；`PlanCard.tsx` 加 `kind='replenishment'`
- **成本测算**: narrator 600 tokens × 100 SKU × 30 天 ≈ 1.8M tokens/月 ≈ DeepSeek-v4 ¥3.6/租户/月 ✅

### F1.2 ⌘K Palette v3 + F3.3 NLQ — Backend 24h + FE 32h
- **新 endpoint**: `POST /api/v1/ai/nlq` 强制 `response_format: json_schema` → `{action: 'navigate|query|execute', target, args}`
- **复用**: 现有 Palette `AI_TRIGGER_MIN_CHARS=5` 保留；改 Enter on AI_ASK_ACTION 行为；新 hook `web/hooks/usePageContext.ts`
- **降级**: 失败/超时 >3s 回退到打开 Drawer
- **成本测算**: palette 400 tokens × 50 queries/user × 100 users × 30 ≈ 60M tokens/月 ≈ ¥120 全租户 ✅
- **风险**: NL→SQL 在 RLS 下的注入；execute 必须走 Plan, 绝不直接落库

### F2.2 库存变动时间线 — Backend 32h + FE 28h
- **零新表** — `stock_movement` 已 append-only。物化视图 (36) `mv_stock_movement_enriched` JOIN product/warehouse/bill_no/user_email
- **AI 集成**: tools.go 新 safe tool `explain_movement(id)`；Redis 24h cache（key 必须带 tenant_id 防跨租户泄漏）
- **Routes**: `GET /api/v1/stock/products/:id/timeline?cursor=` + `POST /api/v1/stock/movements/:id/explain`
- **FE**: `web/app/(dashboard)/stock/[product_id]/timeline/page.tsx`；shadcn Timeline 自定义 + ContextMenu 右键"AI 解释"

### MCP Project-Scoped Resources — Backend 16h + FE 16h
- **`cmd/tally-mcp/manifest.go`** 新建, 3 套 manifest：
  - **buyer**: snapshots / low-stock / purchases / replenishment-suggestions
  - **warehouse**: snapshots / stockouts / movements-recent / transfer-pending
  - **owner**: bills-sales-recent / kpi-dashboard / alerts-all / ai-plans-pending
- **持久解法**: V2 把 persona 编进 PAT scopes；V1 仅 client-side header `X-Tally-Persona`
- **FE**: 补缺失的 PAT 管理页 `web/app/(dashboard)/settings/api-tokens/page.tsx`

---

## 4 个跨 Feature 技术 Enabler（必须先做）

| Enabler | 内容 |
|---|---|
| **1. AI Audit Trail** | 扩 `tally.audit_log` 加列 `model / tokens_in / tokens_out / latency_ms / plan_id / source('chat\|palette\|mcp\|agent')`；index `(tenant_id, source, created_at DESC)`；retention 180 天, agent 1 年；middleware 强制注入 `*AuditLogger`, 业务代码绝不自己写 |
| **2. LLM Gateway** | 新包 `internal/pkg/llmgateway/`：限流（默认 60req/min/tenant）+ prompt versioning（yaml + go:embed）+ 成本统计（Prometheus `tally_llm_cost_cny_total`）+ Hub 切流（env `LURUS_LLM_BASE_URL` 默认 newapi）|
| **3. dblock utility** | 抽 advisory lock 到 `internal/pkg/dblock/`：namespace + keys → FNV-64a；stock / order_create / channel_creds 各一个 namespace, 避免碰撞 |
| **4. MCP Manifest Registry** | `cmd/tally-mcp/manifest.go` 工厂模式；`main.go` 读 `TALLY_PERSONA` env；Claude Desktop 配 3 个 server entry 即可同时挂三套 |

---

## 架构红线（不可破例）

1. **禁 LLM 直连** — 必须经 `internal/pkg/llmgateway` → newapi.lurus.cn；禁止 vendor openai/anthropic/google-genai SDK
2. **禁同步 RPC 到外部电商平台** — 所有 taobao/pdd/douyin/shopify 调用必须 NATS consumer 异步执行
3. **禁绕过 advisory lock** — 任何 `stock_movement` 写入必须先 `dblock.TryAcquire("stock", tenant, product, warehouse)`
4. **禁裸 LLM 写操作** — destructive tool 必须返回 Plan 走 `/api/v1/ai/plans/:id/confirm`；MCP server 永不暴露写 resource
5. **禁 movement UPDATE/DELETE** — append-only；退款/撤销写反向 movement（direction='in', ref=原 movement_id）

---

## 砍掉的 5 个方向（V1.5 不做）

| 砍的方向 | 为什么 |
|---|---|
| 多账簿多准则跨国会计 | 调研 B 点名"伪洋玩意儿"；前 10 客户没人需要 IFRS+中国准则并存 |
| MRP / BOM / 委外加工 | 跟 cross_border + retail 双 persona 对不上, 做下去被制造客户拖死 |
| 完整 Omnichannel（含线下 POS 实时同步天猫旗舰店）| 渠道割裂 + POS 长尾；F2.4 订单聚合够前 10 客户 |
| 复杂审批链 DSL | 前 10 客户团队 < 5 人二级审批够；DSL 是金蝶/用友"大而全"陷阱, 违反原则一 |
| 跨设备草稿同步 + 多人实时协同 | CRDT/OT 工程量巨大；F3.1 单机 IndexedDB 草稿已覆盖 95% 场景 |

---

## GTM — 前 10 客户路径

### 渠道（按优先级）
| 渠道 | 转化预期 | 单 lead 成本 |
|---|---|---|
| 知识星球"跨境老鸟"/"亚马逊卖家精英会" | 100 曝光 → 8 试用 → 2 付费 | ¥0（4h/帖） |
| 小红书"跨境老板日常" tag | 5 笔记 → 30 私信 → 5 试用 | ¥0-500（薯条）|
| 创始人朋友圈 + 微信 1v1 | 50 朋友 → 15 试用 → 5 付费 | ¥0 |
| V2EX / 即刻"跨境电商"节点 | 2 深度帖 → 20 试用 → 3 付费 | ¥0 |
| **不做**：抖音信息流 / 百度 SEM / 阿里云市场 / 1688 卖家社群 | CAC ¥800-2000 太高 / 画像不对 | — |

### 冷启 talktrack（30 秒）
> "专门给 3-8 人跨境团队做的 AI 进销存。不是另一个聚水潭/店小秘。**只解决一件事：你下一批 FBA 该补多少、什么时候、从哪个 1688 供应商下 PO, AI 算好你按一下确认**。Amazon 销量+在途+供应商交期全打通, 不用再开 5 个 Excel。前 10 客户免费 3 个月, 老板亲自帮你导数据。"

### Onboarding 三件套（30 分钟从注册到第一笔 PO）
- **导入字段**：SKU 表 6 字段 + Amazon Business Report CSV 原文件 + 供应商 3 字段（**总共 10 字段封顶**）
- **第一次成功 = 用户在注册后 30 分钟内, 看到 AI 为某 SKU 生成的补货建议卡片, 并点击"导出 PO 给供应商"按钮**（不要求真下单, 要求点了按钮 = 产生信任动作）
- **第二次回访**：第 3 天创始人微信语音 10 分钟, 问"那张 PO 你下单了吗 / 这 3 天打开几次 / 关掉了你最想念什么"

### 避免做的 3 件事
1. **不做"对比聚水潭/店小秘"营销页** — 受过伤的客户会觉得"又是想抢蛋糕的"
2. **不接受任何客户定制功能请求**（哪怕付 ¥5 万）— 做完不付 + 产品被带歪 + 第 11 客户用不了
3. **不做大客户 case study / 媒体 PR / 投融资 deck** — 客户 < 50 之前对外保持隐身

---

## H1/H2/H3 假设 Schema（`./assumptions.md`）

```yaml
id: H1
hypothesis: "跨境老板愿付 ¥3000/年 for 多平台合单 + Kova 周一补货"
owner: <founder email>
deadline: 2026-W36 (Sprint 12 end ≈ 2026-08)
falsification_threshold: <3/8 trials convert at >= ¥3000
evidence_source:
  - prometheus: tally_trial_conversion_d90
  - manual: signed_contracts.csv
current_value: <updated daily by bin/assumption-snapshot.sh>
status: pending | truthy | falsified | inconclusive
last_evidence_url: <call recording / payment record link>
```

H1（付费意愿）/ H2（OCR + 群机器人替代 Excel）/ H3（⌘K + Drawer DAU 渗透）三个 owner = founder。**S0-C1 落地前任何 S1 工作都不开。**

---

## Anti-metric 升级（Grafana panel "V1.5 health"）

旧：`features_added ≤ 3/月`。新（每周一发飞书）：

| 指标 | 目标 |
|---|---|
| `features_shipped`（feature flag 翻开数）| ≤ 3/月 |
| `bugs_open - bugs_closed`（GH issue `regression` label delta）| ≤ 5 同时 open |
| `assumption_evidence_gap_days`（H1/H2/H3 row 最后更新距今）| ≤ 7d |
| `lint_warnings_total`（CI artifact）| 单调递减 |
| `features_shipped_cumulative / WAD_weekly_avg` | 趋势必降，不升 |

`bin/health-report.sh`（S0-C3）每周一发飞书；任一指标连 2 周违 → 触发 retrospective。

---

## Kill Switch wiring（codebase 位置）

| KS | 信号源 | wiring |
|---|---|---|
| **KS1**：onboarding <40% | event `onboarding_first_po_exported / signups_d7` | telemetry → NATS `PSI_TELEMETRY.web.onboarding.*` → `internal/app/billing/telemetry_aggregator.go`（新, S10）→ Prom gauge `tally_onboarding_completion_rate` → S10 rule |
| **KS2**：d45 PO 采纳 <20% | event `plan_accept_rate` JOIN `replenishment_suggestion.status='accepted'` | view `mv_kpi_po_accept_d45`（migration 000045, S10）→ Prom gauge `tally_po_accept_rate_d45` |
| **KS3**：d90 转化 <30% | 手 + webhook `subscription.status='active'` | 现有 `internal/adapter/handler/subscription/`；夜跑 Prom gauge `tally_trial_conversion_d90` |

三 alert → 飞书 webhook `KILL_SWITCH_FEISHU_URL`（configmap env）。连 2 周 red → GitHub Action 自动开 issue `[KILL-SWITCH] KS<n> tripped` 附 assumptions.md。

---

## 总工时 envelope

| 类 | h | % |
|---|---|---|
| Track P（16 feature）| 1014 | 75% |
| Track Q（CI/telemetry/backup/security）| 180 | 13% |
| Track C（客户 + onboarding gap + 假设）| 150 | 11% |
| **核 sprint 总** | **1344** | **100%** |
| Sprint 0（pre-budget）| 52 | — |
| **大总** | **1396** | — |

容量上限 2 dev × 12 sprint × 56h × 70% = **1344h**，**Sprint 8 借 Sprint 10 的 48h（NLQ 大）**，其它平均。Sprint 0 视作 pre-launch week。

---

## 关键文件 / 待修改路径

### 复用 — 0 新建
- `internal/pkg/llmclient/client.go` — LLM HTTP（newapi + SSE + tool-calling + 5 个 DeepSeek-v4 trap defense）
- `internal/app/ai/orchestrator.go` + `tools.go` — AI 编排 / Plan 二次确认 / propose_* 模式
- `internal/adapter/handler/ai/handler.go` — SSE chat + plan confirm/cancel
- `internal/adapter/repo/stock/advisory.go` + `repo.go:69` — FNV-64a advisory lock
- `internal/adapter/nats/events.go` + `publisher_typed.go` — typed publisher
- `cmd/tally-mcp/` (main/client/resources.go) — MCP stdio + 6 V0 read resource
- `web/components/command-palette/Palette.tsx` + `groups.ts` — ⌘K（不重写, 加 RPC）
- `web/components/ai-assistant/Drawer.tsx` + `MessageList.tsx` + `PlanCard.tsx` — AI Drawer SSE + plan
- 表：`tally.stock_movement` (m22) / `tally.audit_log` (m9) / `personal_access_token` (m31)

### 新建（Sprint 0 起逐 sprint 落代码，本路线图不直接 commit 代码）
- `_bmad-output/planning-artifacts/assumptions.md`（S0-C1，骨架 commit 在本 PR）
- `bin/assumption-snapshot.sh`（S0-C2）
- `bin/health-report.sh`（S0-C3）
- `deploy/k8s/base/cronjob-pgbackup.yaml`（S0-Q4）
- `deploy/k8s/base/cronjob-audit-retention.yaml`（S5-Q）
- `deploy/k8s/base/prometheus-rules.yaml`（S10-Q）
- `internal/pkg/llmgateway/metrics.go`（S0-Q2）
- `internal/pkg/llmgateway/ratelimit.go`（S4-Q）
- `internal/pkg/llmgateway/sqlguard.go`（S8-Q）
- `internal/app/billing/telemetry_aggregator.go`（S10）
- `internal/adapter/handler/{supplier,warehouse}/handler.go`（S1-C）
- `internal/adapter/handler/product/import.go`（S2-C）
- `internal/pkg/dblock/`（跨 enabler）
- `internal/domain/channel/` + `internal/app/channel/` + `internal/adapter/platform_ext/{taobao,pdd,douyin,a1688,shopify}/`（S6-P）
- `internal/domain/replenishment/` + `internal/app/replenishment/{engine,narrator,po_builder}.go`（S4-P）
- `cmd/tally-mcp/manifest.go` — 3 套 persona manifest
- Migrations: 000032 audit_log ALTER (S2-Q) / 000033 replenishment_suggestion (S4-P) / 000034-36 platform_channel/order/order_line (S6-P) / 000037 mv_stock_movement_enriched / 000038 PAT.persona / 000045 mv_kpi_po_accept_d45 (S10)
- `migrations/data/cross_border_seed.sql`（S3-C）
- `web/app/(dashboard)/channels/` + `replenishment/` + `stock/[id]/timeline/` + `settings/api-tokens/`
- `web/hooks/usePageContext.ts` — Palette/Drawer context 注入

### 改既有
- `.github/workflows/release.yaml`：image-* job 加 `needs: [backend-lint, backend-test, frontend]`（S0-Q1）
- `.github/workflows/ci.yaml`：加 Lighthouse CI step（S11-Q）
- `internal/pkg/llmclient/client.go:232`：metric emit hook（S0-Q2）
- `internal/adapter/handler/router/router.go`：注册 `/internal/v1/metrics`（S0-Q2）+ supplier/warehouse/import 路由（S1, S2）+ OTel middleware（S9-Q）
- `web/lib/telemetry.ts:8`：union 扩 5 个 event（S0-Q3）
- `web/app/api/otel-events/route.ts`：写 NATS（S0-Q3）
- `web/app/(dashboard)/layout.tsx`：Driver.js tour + Crisp widget（S3-C, S5-C）
- `internal/adapter/platform/agent.go` — 当前 TODO 桩, 填掉接 Kova `http://kova-rest:3002/v1/workflows/replenishment/runs`
- `cmd/tally-mcp/resources.go` — 抽 manifest 工厂

---

## 验证

### 每个 sprint 的"算完成"门
每 feature 必须配 3 件：① 失败测试 → pass（Karpathy 第 4 原则）② 量化目标实测数（不是"我觉得"）③ 1 个客户访谈反馈 / 内测 score。

### Sprint exit gate（12 sprint 全检查清单）
- **Sprint 0 末**：4 件 S0-Q + 3 件 S0-C 全过；红 lint commit → image build skipped 实测；`/internal/v1/metrics` 见 2 LLM metric；5 新 telemetry event 到 NATS；PG backup CronJob restore drill 通；assumptions.md owner 三栏非空
- **Sprint 3 末（STAGE-1）**：5 个内部账号 30 min 内走完 tour P95；AI Drawer 答 3 个 page-context 问相关性 ≥4/5（n=15 内测）；Palette + Drawer event 在 Grafana 可见
- **Sprint 4 末**：首 3 客户登陆 STAGE，每家 ≥50 SKU import，每家至少 1 次 `onboarding_first_po_exported`
- **Sprint 6 末（STAGE-2）**：5 客户 on STAGE，WAD ≥10/周，`tally_platform_oversell_total` 连续 14d = 0
- **Sprint 7 末**：10 客户活跃；d14 cohort 留存 ≥60%；Cmd+Z 月调用 ≥5 次/活跃
- **Sprint 8 末**：NLQ 准确率 ≥80%（50 条真实客户问），injection 假阳 0；H1/H2/H3 dashboard 显示过去 56d 日值
- **Sprint 9 末（STAGE-3）**：10 客户 WAD ≥40/周累计，push confirm 转化 ≥30%，留存 ≥60% at d14
- **Sprint 10 末**：3 KS alert 在 synthetic 阈值越界时全发飞书；Trust Center 显示任意租户过去 90d audit-log diff
- **Sprint 11 末**：Lighthouse CI 在 main 绿；10k SKU 表 60fps 渲；≥3/10 客户口头承诺付费
- **Sprint 12 末（R1 收口）**：WAD ≥80 累计；≥3 付费；H1/H2/H3 每个标 truthy/falsified/inconclusive + evidence URL；CI/CD 子分 ≥7.5；客户子分 ≥6.0

### 工具
- 埋点：5 个新 event（S0-Q3）+ 旧 `draft_restore` / `undo_used` = 7 个；每个量化目标埋一个
- LLM 成本：Prometheus `tally_llm_cost_cny_total{tenant,model}` + `tally_llm_tokens_total`；S0-Q2 落地
- 客户访谈：录音 → 周一上午全团队过 10 个回访
- 假设：`assumptions.md` + `bin/assumption-snapshot.sh` 日跑

### 不通过的处理
- 任一 sprint exit gate 不过 → 不进下一 sprint，加 1 sprint 修；最多加 2 sprint，再不过启动 pivot 会议（按 3 个 Kill Switch 决策）

---

## Out of Scope（本计划不含）

- 实际写代码、改 commit、推 PR — 本计划只是路线图, 待 BMAD story 拆解后逐 sprint 落地
- 新建 BMAD epic / story 文件 — 落地阶段再做（S0 后逐 sprint）
- 联系真实客户 / 投广告 — GTM 描述的渠道动作待 Sprint 4 启动
- 部署 PG backup / Prometheus rules — 在 Sprint 0 / Sprint 10 实际任务里落地
- 给 `assumptions.md` 填具体 owner 邮箱 — S0-C1 founder 签字
- 修旧 `roadmap-v1.5.md` 里的细微 typo — 整体替换不在乎

---

## 下一步

待用户拍板后, 由后续 session 按 sprint 拆 BMAD story:
1. **Sprint 0**: 4 件 Q + 3 件 C → 各开一个 issue/story, owner 签字 assumptions.md
2. **Sprint 1**: F1.1 STAGE 端到端验证 + F1.2 Palette 起步 + Supplier/Warehouse CRUD
3. **Sprint 2-3**: F1.2 收尾 + F1.3 暗黑色板 + F1.4 AI Drawer + Tour + Demo seed → STAGE-1 gate
4. **Sprint 4 起**: 首 3 lighthouse 客户上 STAGE，按 Q-before-P 矩阵推进
