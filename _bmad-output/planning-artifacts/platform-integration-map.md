# Tally → Platform Integration Map

> 写于 2026-05-01。Tally 复用 `lurus-platform` (P0 基础设施) 的 capability 盘点。
> 决策依据：`lurus.yaml` 已声明 Tally consumes 7 个 capability，实际接了 4 个，3 个待接。
> 真源对照：`lurus.yaml` (capabilities + lurus-tally.consumes_capabilities)，
> `2l-svc-platform/api/openapi.yaml` (48 endpoints)，
> `2b-svc-psi/internal/pkg/platformclient/` (现有 client)。

---

## 1. 一句话原则

| 类型 | 归属 | 理由 |
|------|------|------|
| 客户的业务数据（商品/单据/库存/项目/苗木字典/客户/供应商）| **Tally 自持** | PSI 是客户用来管自己生意的，数据所有权属客户租户 |
| Tally 作为 SaaS 自身的运营基础（账户/订阅/钱包/通知/AI/Agent/记忆）| **走 Platform** | 跨产品共用，platform 是 P0，造重轮子是浪费 |

判别一句话：**"是 Tally 客户管自己客户的，还是 Lurus 管 Tally 客户的"** —— 后者全部走 platform。

---

## 2. Capability 接入清单

| # | Capability | Tally 状态 | Endpoint / 实现 | 备注 |
|---|-----------|-----------|----------------|------|
| 1 | **identity** | ✅ 已接 | `POST /internal/v1/accounts/upsert` via `platformclient.Client` (Task #39) | account upsert 在用，但 `validate-session` / `account_overview` 没用 |
| 2 | **billing** | ✅ 已接 | `POST /api/v1/subscriptions/checkout`、`/api/v1/billing/{overview,subscribe}` (Story 10.1) | `internal/pkg/platformclient/billing.go` |
| 3 | **llm-inference** | ✅ 已接 | `POST https://newapi.lurus.cn/v1/chat/completions` (AI Drawer + ⌘K, Story 11.1) | `internal/pkg/llmclient/client.go` |
| 4 | **auth** | ✅ 已接 | Zitadel OIDC PKCE → `web/auth.ts` (NextAuth v5) | id_token 转 backend Bearer |
| 5 | **notification** | 🟡 **client ready, 待业务调用** | `POST http://notification.lurus-platform.svc:18900/internal/v1/notify`<br>或发 NATS `PSI_EVENTS` 让 notification 消费 | Track A done: `internal/adapter/nats/publisher.go` + `internal/adapter/platform/notification.go`; PSI_EVENTS schema in contracts.md |
| 6 | **memory** | ✅ 已接 (Track C) | `http://memorus.lurus-system.svc:8880` (REST) via `internal/pkg/memorusclient/` | AI Drawer recall+write-back；MEMORUS_API_KEY 空→降级，AI 继续工作 |
| 7 | **agent-execution** | ❌ **待接** | `kova-rest:3002` | E17 (V1 计划) + E31.2 项目缺口预警 (V3-Horticulture) |

---

## 3. 待接 Capability 详细

### 3.1 notification（推荐优先级 P0）

**用例**:
- 项目状态变更 → 通知项目经理（站内 + 邮件）
- 付款到账 → 通知财务（站内）
- AI 日报推送（每天 8:00）→ 5 条要点（站内 + 推送）
- 库存预警（低于 ROP）→ 采购员（站内）
- 季节移植窗口提醒 → 业务员（站内 + 短信，如 SMS provider 已配）

**接入路径**（二选一）:
- **A. 同步 HTTP**: `POST /internal/v1/notify`
  - 适合：实时确认结果（如付款到账要立刻显示"已通知 N 人"）
  - 缺点：notification 慢/挂掉影响 Tally 主路径
- **B. 异步 NATS**: 发 `PSI_EVENTS` stream → notification 自己 subscribe + 路由
  - 适合：批量、可延迟、解耦
  - lurus.yaml 已配 `PSI_EVENTS` stream（line 154），现成的
  - 推荐 **B** 作为默认；A 仅用于"用户期待立即看到反馈"的场景

**估时**: 4h
- 1h: NATS publisher 封装到 `internal/adapter/nats/publisher.go`
- 2h: 在 project state change / payment confirm / AI 日报 cron 三处发事件
- 1h: 跟 platform 团队对齐 PSI_EVENTS schema → 加到 `doc/coord/contracts.md`

**架构**:
```
Tally write op → state change → publish PSI_EVENTS event
                                       │
                                       ▼
                                notification svc (consumer)
                                       │
                          ┌────────────┼────────────┐
                          ▼            ▼            ▼
                       in-app       email        SMS/FCM
```

---

### 3.2 memory（推荐优先级 P1）

**用例**:
- AI Drawer 推荐时间偏好（用户喜欢早 8 点 vs 晚 6 点）
- 常用客户/常用苗木快捷入口（按用户行为自动推荐）
- 跨设备同步 AI 对话历史（PC → 手机）
- 商品/客户 RAG 历史（搜"红枫" 后 AI 直接召回上次报价）

**接入路径**: `POST http://memorus.lurus-system.svc:8880/v1/memories`
- Auth: `X-API-Key` (env `MEMORUS_API_KEY`)
- OpenAPI: `2b-svc-memorus/api/openapi.yaml`
- 已有 client 模式参考: 找 `2c-svc-lucrum/internal/pkg/memorusclient/`（lucrum 是 consumer）

**估时**: 6h
- 2h: 写 `internal/pkg/memorusclient/`（add / search / delete 3 个方法）
- 2h: AI Drawer 接入 — 每次 chat 前 search 召回 + chat 后 add 摘要
- 1h: 用户偏好接入 — 替换当前的"硬编码默认值"
- 1h: 配置 + 单测 + 错误降级（memorus 挂掉 Tally 不挂）

**架构原则**: memorus 是**降级可用**依赖 — 调失败时 AI Drawer 仍能工作，只是不召回历史。

---

### 3.3 agent-execution（推荐优先级 P1）

**用例**（来自原 E17 + V3-Horticulture E31）:
- **补货 Agent**：扫库存 + 销量 + lead time → 生成补货单建议（人工审核）
- **季节窗口 Agent**：扫苗木字典 best_season + 当前未售库存 → 移植窗口提醒
- **项目缺口 Agent**：扫 project_item 计划用苗 vs 库存 → 缺口报告 + 推荐供应商
- **现金流预测 Agent**：已签未回 + 已订未付 → 月度图（E31.5）

**接入路径**: `POST http://kova-rest.kova.svc:3002/v1/workflows`
- 每个 Agent = 一个 kova workflow definition（YAML/JSON）
- Kova 持久执行，WAL 崩溃恢复
- Tally 提交 workflow + 输入数据，poll 或 webhook 拿结果

**估时**: 10h
- 2h: 写 `internal/pkg/kovaclient/`（submit / poll / cancel）
- 3h: 把"补货 Agent" 落地为 kova workflow YAML（schedule + steps + outputs）
- 2h: Tally 端：补货建议页 + 审批流
- 2h: webhook receiver — kova 回调 Tally 同步状态
- 1h: 配置 + 错误降级

**架构原则**: Tally 永远只**提交输入 + 接收输出**，不在 Tally 进程内跑 long-running agent。

---

## 4. Anti-Pattern 清单（不该做的）

| 反模式 | 应该做的 |
|--------|---------|
| Tally 自己写 cron 跑"季节窗口提醒" | 走 kova workflow（持久 + 可观测）|
| Tally 自己写 SMTP 发邮件 | 发 NATS / 调 platform notification |
| Tally 自己存"用户偏好"到 user_settings 表 | 存 memorus（跨设备同步）|
| Tally 自己写 OpenAI client | 走 newapi gateway（已做 ✅）|
| Tally 自建 Zitadel 客户端 / 自己签 JWT | 用 NextAuth + platform validate-session |
| Tally 直接读 platform DB（identity/billing schema）| 永远走 internal HTTP/gRPC API |
| Tally 复制 platform 的 wallet 表自己做余额 | 调 `/internal/v1/accounts/{id}/wallet/balance` |

---

## 5. 架构沉淀建议

### 5.1 把所有 platform 调用收敛到 `internal/adapter/platform/` ✅ DONE (Track A)

原来散落在 `internal/pkg/platformclient/`，Track A 已迁移为：

```
internal/adapter/platform/
├── identity.go        # account upsert / overview / validate-session
├── billing.go         # 订阅 / 钱包 / checkout（已有，迁移过来）
├── notification.go    # 同步 HTTP / 异步 NATS publisher 二合一
├── memory.go          # memorus client wrapper
├── agent.go           # kova workflow submit / poll
├── llm.go             # newapi (从 internal/pkg/llmclient 迁移)
└── client.go          # shared http client + retry + circuit breaker
```

**好处**:
- 一处 mock 全测试通
- platform v1→v2 升级只改 adapter，业务代码不动
- 单测能跑（不依赖 platform up）

**估时**: 4h（重构 + 测试不变）

### 5.2 OpenAPI 自动生成 client

`2l-svc-platform/api/openapi.yaml` 是真源（48 endpoint）。Tally 应跑 `oapi-codegen` 生成 typed client，避免手写漂移。

**估时**: 2h（写 build script + commit 生成产物 + CI 校验）

### 5.3 NATS contract 写死

`PSI_EVENTS` stream 的 event schema 必须写到 `doc/coord/contracts.md` 并 review 通过才能发。否则 notification / memorus consumer 会被 schema 漂移搞挂。

---

## 6. 推荐落地顺序

| Sprint | 内容 | 估时 | 收益 |
|--------|------|------|------|
| **下一个** | (1) 重构 `internal/adapter/platform/` 包 + (2) 接 notification (NATS-first) | 4 + 4 = 8h | 解决"AI 日报推送"卡点；为后续 epic 铺路 |
| +1 | 接 memorus（AI Drawer 历史召回 + 用户偏好） | 6h | AI 体验跨设备一致 |
| +2 | 接 kova（补货 Agent → 季节窗口 → 项目缺口）三选一先做 | 10h | 兑现"差异化护城河" |
| +3 | OpenAPI codegen + CI 校验 | 2h | 长期防漂移 |

**总计**: ~26h ≈ 0.5 sprint，但分摊到 4 个 sprint 可与 horticulture epic 并行。

---

## 7. 已知风险 / 阻塞

| # | 风险 | 缓解 |
|---|------|------|
| R1 | Tally pod 在 R6 (cloud-ubuntu-5)，platform 在 R1 PROD — **跨集群跨网络** | platform 已暴露 public `identity.lurus.cn`；STAGE 阶段允许走 public，PROD 时再切 internal mesh |
| R2 | NATS `PSI_EVENTS` schema 没人 own，Tally 改了 notification 不知道 | 接入前必须先 PR `doc/coord/contracts.md` + ping notification owner |
| R3 | memorus 还在改（MEMORY.md 标 "同事在改，勿动"）| 接入前确认 stable；先做 client wrapper 但不上线，等 memorus prod-ready |
| R4 | kova CI 全红（MEMORY.md 标 blocker）| kova 接入推到 +2 sprint，等 CI 修好再启动 |
| R5 | platform `INTERNAL_API_KEY` 在 R6 secret 里有没有？已 audit | 部署前 `kubectl -n lurus-tally get secret tally-secrets -o yaml \| grep -i platform` 确认 |

---

## 8. 决策点（需要拍板）

- **D1**: NATS-first 还是 HTTP-first 通知？→ **NATS-first**（默认；HTTP 仅"立即反馈"场景）
- **D2**: memorus 客户端用 OpenAPI 生成还是手写？→ **手写 wrapper**（接口简单，3 个方法，codegen 反而重）
- **D3**: kova workflow 定义放哪？→ **`deploy/kova/workflows/*.yaml`**（跟 K8s manifest 同 git tree，便于 review）
- **D4**: 现有 `internal/pkg/platformclient/` 是否立刻重构？→ **已重构 (Track A)**：原实现迁到 `internal/adapter/platform/`；旧包保留 `aliases.go` 兼容层，待 merge 后统一删除

---

## 9. 关联文档

- 真源：`lurus.yaml` (capabilities + lurus-tally.consumes_capabilities)
- Platform OpenAPI: `2l-svc-platform/api/openapi.yaml`
- Memorus OpenAPI: `2b-svc-memorus/api/openapi.yaml`
- Kova: `2b-svc-kova/CLAUDE.md`
- 跨服务契约: `doc/coord/contracts.md`
- Horticulture 扩展: `_bmad-output/planning-artifacts/horticulture-extension.md`
- 现有 client: `2b-svc-psi/internal/pkg/platformclient/`
- 现有 LLM client: `2b-svc-psi/internal/pkg/llmclient/`
