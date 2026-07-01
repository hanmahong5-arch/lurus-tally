# Lurus Tally — V2 路线图：多端 + 自治 Agent + 终端设备

> 创建：2026-06-20
> 状态：DRAFT — 待 owner 拍板起步 phase（已接受推荐：F1+F2 地基 → A2 自动进货）
> 方向决策（2026-06-20 owner）：**不抽离独立公司，留在 Lurus 体系**；身份层 platform 已把 Zitadel 换成另一套 IdP，对 Tally **零代码影响**（vendor-neutral OIDC，纯 env）。
> 产品愿景：把 Tally 从「Web 进销存」扩成 **多端（Web ✓ + 移动 App + 桌面 GUI） + 自治 Agent（自己谈单 / 进货 / 理货） + 异步处理引擎 + 终端设备接入**。
> 关联：`./roadmap-v1.5.md`（V1.5 三轨）/ `./architecture.md` / `./f1-f2-foundations-tdd-plan.md`（本路线起步两 phase 的可执行计划）
> 测绘来源：2026-06-20 5 路只读 Explore（异步骨架 / AI-agent / 采购域 / 库存域 / 多端·API·设备），全部对 HEAD 核验。

---

## Context — 为什么写这份计划

V1.5 路线（`roadmap-v1.5.md`）解决的是「把单一 Web 进销存做扎实并找到 PMF」。本路线接续 V1.5，回答 owner 在 2026-06-20 提出的产品扩张：**多端 + 自治 + 设备**。

核心结论先行：**这不是一次重构，是三条能力带的"扩"——没有任何一处需要推倒重来。** 现有代码地基比预期厚，自治闭环的「执行 + 安全」件已是生产级，真正缺的是：
1. 把同步 AI 升级成**后台自治循环 + 持久化长任务引擎**；
2. 采购域的**谈判 / 定价 / 报价**数据模型（目前空白）；
3. 多端与设备的**接入面**：API 规格（OpenAPI 零基础）、实时推送、设备身份。

---

## 现有地基复用盘点（2026-06-20 实勘，对 HEAD 核验）

| 子系统 | 复用度 | 关键现成件（file 引用） | 真缺口 |
|---|---|---|---|
| **异步骨架** | 高 | 事务性 outbox `internal/adapter/repo/event_outbox/store.go` + 30s 轮询 worker `internal/adapter/nats/outbox_worker.go` + `FOR UPDATE SKIP LOCKED` + RLS service-bypass（多副本安全，生产级）；第二个模板 = LLM usage 重试队列 `internal/adapter/usagereport/retry_worker.go` + mig 000053 | 无"长任务"状态机：running 中间态 / 进度 / 取消 / 超时 / DAG 依赖 / 定时调度 |
| **AI / Agent** | 高 | 8 读 + 3 写工具 `internal/app/ai/tools.go`；多轮 tool-calling（封顶 6 轮）`orchestrator.go`；**Plan → 批准 → 执行 → 30s 撤销** 安全闭环 `domain/ai/plan.go`+`executor.go`+`revert.go`；entitlement 闸 `app/entitlement/service.go`；Langfuse `observability/llm/`；Memorus recall `app/ai/memory.go`；MCP 只读 server `cmd/tally-mcp/` | Kova 客户端是 **TODO 占位（未接）** `internal/adapter/platform/agent.go`；无自治多步循环；无调度；无策略护栏；无供应商外部连接器 |
| **采购域** | 中（~40%） | supplier `domain/supplier/supplier.go` + partner 镜像（mig 000054 同步）；bill PO 草稿→批准 状态机 `domain/bill/bill.go`+`app/bill/create_purchase.go`；payment 多币种 `domain/payment/payment.go` | 无 supplier_pricing / 报价(quote) / RFQ / 账期·交期条款 / 谈判日志 / 自动批准路径 |
| **库存域** | 高（~70%） | stock_movement 追加流水 + FIFO/WAC 成本引擎 `app/stock/`（mig 000022）；lot；replenish 预测 ROP/学习提前期/安全库存 `app/replenish/usecase.go`；低库存告警 `app/replenish/low_stock.go`；scorecard mig 000050 | 无 bin 级快照；无盘点台；无跨仓调拨路由（列已留 `target_warehouse_id` 未接）；无设备扫码流水；无自动补货执行 |
| **客户端 / API** | 高 | REST **干净·端无关** `handler/router/router.go`；两条 auth：OIDC JWT（浏览器，vendor-neutral）+ **PAT** `domain/auth/pat.go`（可直供设备/移动/GUI）；dev 头绕过 `TALLY_DEV_MODE` | **无 OpenAPI 规格**（codegen 零基础）；无设备注册流；无实时推送（仅 AI chat 有 SSE）；PAT 未带设备身份；无离线同步 |

---

## V2 路线总图

```
F1(OpenAPI 规格) ─┬───────────────→ B1 移动 App / B2 桌面 GUI
                  └→ C1 设备身份 → C2 实时通道 → C3 设备接入
F2(异步 job 引擎) → A1 Agent 运行时+护栏 → A2 自动进货 → A3 谈单 → A4 理货
```

依赖只有两条主线，互相独立 → 可并行排期。两块"地基"（F1/F2）是全图解锁点。

---

## 🧱 地基（便宜·高杠杆·解锁一切）

### F1 — OpenAPI 规格 + 类型化 client codegen
**为什么**：现状 `router.go` 手工接线、无任何 spec（实勘 grep openapi/swagger 零命中）；前端 client `web/lib/api/*.ts` 手写。App / GUI / 设备三端要 codegen 全卡在这。
**做什么**：给 `/api/v1` 产出 OpenAPI 3 spec（注解或后置导出），进 CI；前端与移动端从 spec 生成类型化 client。
**验证**：CI 产出 spec 工件；前端 + 一个移动端 stub 均能从 spec codegen 并编译通过。
**风险**：低。不改业务逻辑，只加描述层。

### F2 — 持久化异步 job 引擎
**为什么**：自治长任务（谈单/进货跑数分钟~数小时）与设备/导入长流程都需要"可续跑、可取消、有进度"的后台执行；现有 outbox 是单发(pending→published)，撑不住。
**做什么**：新 `tally.async_jobs` 表（镜像 outbox + state/进度/超时/取消/parent/trace），复制 `outbox_worker.go` 轮询循环成 `async_job_worker.go`，接进 `lifecycle/app.go`。复用 `SKIP LOCKED` + RLS service-bypass。
**验证**：三步 demo job pending→running→completed；进程 kill 后续跑；可取消；RLS 隔离 e2e 绿。
**风险**：中（新表 + worker + 状态机）。**详见 `./f1-f2-foundations-tdd-plan.md`。**

---

## 🤖 Pillar A — 自治（谈单 / 进货 / 理货）= 产品差异化核心

### A1 — Agent 运行时 + 护栏
把同步 orchestrator 升级成 job 引擎上的**目标驱动后台循环**（复用 Plan/批准/执行/撤销安全闭环），加策略层：单笔上限 / 折扣帽 / 超额转人工审批。选型决策：进程内 agent 循环 vs 真接 Kova（现为占位）。
**验证**：给定目标"补齐低库存"→ agent 后台跑 → 产出待批 Plan → 审批执行 → 审计落 `account_audit_log`。

### A2 — 自动进货（**推荐自治第一站**，复用率最高）
扩 replenish：触 ROP → 自动建 PO 草稿 → 策略闸下自动/转人工批准 → in-transit/ETA 跟踪。复用 replenish 70% + Plan/批准闭环现成 + F2 引擎。
**验证**：库存触 ROP → 系统自动生成并（策略内）批准 PO → scorecard 采纳率可见。

### A3 — 自动谈单（新域最多）
新 `supplier_pricing` + 报价/RFQ + 谈判日志域；agent 多供应商比价、提议成交、人在环批准。
**验证**：多供应商比价 → agent 选优 → 谈判日志可追溯 → 转 A2 下单。

### A4 — 自动理货（面最广）
bin 级库存快照、盘点台（差异→补偿移动）、跨仓调拨路由、滞销/呆滞动作。
**验证**：盘点差异 → 补偿移动落账；跨仓建议 → 调拨单全链路。

---

## 📱 Pillar B — 多端（依赖 F1）

### B1 — 移动 App
框架候选：Expo / Flutter。PAT 或 OIDC 登录；扫码驱动的读 + 写；弱网容忍。
### B2 — 桌面 GUI
框架候选：Wails（贴 Lurus 现有桌面栈）/ Tauri。面向后台重操作（批量、报表、审批中心）。
**验证（各端）**：从 spec codegen → 登录 → 一笔采购→入库→销售 全链路跑通。

---

## 🔌 Pillar C — 终端设备（依赖设备身份 + 实时）

### C1 — 设备身份与注册
PAT 扩 `device_id`/`device_name`；注册流 + 审计。复用现有 PAT 加密/校验（`domain/auth/pat.go` 已 constant-time）。
### C2 — 实时通道
SSE / WebSocket 或 Redis/NATS pub-sub，推实时库存/价格给 POS/扫码枪（现仅 AI chat 有 SSE）。
### C3 — 设备接入
扫码→movement 快路径；`device_scan_log`；离线同步队列；GRN 收货对账。
**验证**：扫码枪实时查库存 + 出入库；断网重连补传不重复（幂等）。

---

## 推荐排序 + 起步建议

**地基先行**：F1 + F2 并行（风险最低、解锁全图）。
**首个自治**：A2 自动进货——复用率最高，最快验证"系统替你补货"的价值主张。
**往后**：A3 谈单（新域最多）→ Pillar B 多端 → Pillar C 设备。

> **owner 已接受此起步顺序（2026-06-20）。** F1+F2 的可执行 TDD 计划见 `./f1-f2-foundations-tdd-plan.md`。

---

## 诚实边界

- 本文是**结构与依赖**，**不含承诺工期**。Explore 给的周数是粗估，真排期在选定 phase 后细化为带失败测试的可验证目标。
- **migration 协调雷区**：当前 main migration head = 000053，feat 分支已到 000054，且 swarm 活跃 churn。F2 等新迁移**到实现时**经 `doc/coord/migration-ledger.md` 锁下一个空闲 ID，勿提前硬编。
- **Kova 复用是 aspirational**：`internal/adapter/platform/agent.go` 当前是 TODO 占位，A1 需先决定"进程内循环 vs 接 Kova"。
- 否定式结论（"无 X / 缺 Y"）均已对 2026-06-20 HEAD 核验；若后续 swarm 补齐某项，本文相应缺口应改标"已解决"。
