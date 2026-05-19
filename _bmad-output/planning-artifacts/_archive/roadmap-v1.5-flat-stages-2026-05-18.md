# Lurus Tally — V1.5 进化路线图 (Release R1)

> 2026 H2 · 12 sprint × 2 周 = 6 个月
> Source: 3 份联网调研（国内 SMB / 海外 AI ERP / AI workspace UX）+ 3 角色 Plan swarm（PM·UX / 架构 / GTM）
> 创建：2026-05-18
> 状态：APPROVED — 待 BMAD story 拆解执行
> 关联：`./epics.md` V2.5 横向增强带（E21-E27）/ `./roadmap-ux-supplement.md`

---

## Context — 为什么写这份计划

**产品现状（2026-05-18）**:
- Tally 后端核心闭环已就绪：snapshots / movements / bills / stockouts / low-stock / AI replenishment plans + 库存 UI 完
- tally-mcp Phase 3c 刚落地：GitHub Release pipeline + lurus.yaml capability entry（commits cca50454 / 98b9eea / a627424）
- **未上线任何商业客户**；STAGE 已在 R6 跑 24d (`tally-stage.lurus.cn` /ready 200), ArgoCD 已绕开走人工 apply (ADR-0006); 真实可用性 (登录链路 / OIDC client_secret 是否 PKCE-only) 待端到端验证
- V1 砍枝已 lock：Epic 12-16 离线/边缘延后到 V2.5

**问题**: 进销存赛道竞品成熟（聚水潭年迭代 400 次、用友/金蝶下沉中、海外 Cogsy/Settle 卖 $49-199），若按"做完 Epic 1-20"思路推进,会变成又一个"功能全但用户没感觉"的产品。本计划用调研 + swarm 把"做什么 / 不做什么 / 卖给谁 / 怎么验证"钉死。

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

## 6 个月进化路线（12 sprint × 2 周）

### 阶段一：止血与立桩（Sprint 1-3, 184h）
> STAGE 已运行 (2026-05-18 实测), 主线是把"⌘K + AI Drawer + 暗黑模式"门面立起来; F1.1 缩到端到端验证。

| Feature | One-liner | 工时 | 量化目标 |
|---|---|---|---|
| **F1.1 STAGE 端到端验证 + 漂移补齐** | 真实 5 账号浏览器走完 Zitadel 登录 → 看库存 → 建草稿单; 按发现的缺口补 secret/configmap | 4h | sprint 1 末 5 个内部账号可登录下单看库存, P95 < 400ms |
| **F1.2 ⌘K v3 Palette** | 200ms 内出"新建 PO / 跳 SKU / 帮我查"三栏（AI fallback ≥5 字符已有） | 50h | 30 天 ⌘K DAU 渗透 ≥ 60%, 命中 ≤ 2 keystrokes |
| **F1.3 暗黑模式库存色板** | 表格深色不刺眼、低库存红不糊 | 50h | 所有列表页 Lighthouse contrast ≥ AA |
| **F1.4 AI Drawer v0.5** | 右抽屉根据页面 context 一句话回答（"这个 SKU 周转怎么样"） | 80h | 3 个真实页面回答相关性 ≥ 4/5（n=15 内测） |

### 阶段二：AI 主动开口（Sprint 4-6, 330h） — *前 3 个 lighthouse 客户上 STAGE*
> 从"AI 你问我答"升级到"AI 主动找你"。

| Feature | One-liner | 工时 | 量化目标 |
|---|---|---|---|
| **F2.1 Kova 补货 Agent v1** | 周一 8am AI Drawer 弹卡片"3 个 SKU 即将断货, 一键 PO" | 80h | 30 天 AI 生成 PO 采纳率 ≥ 40%（±20% 数量内提交） |
| **F2.2 库存变动时间线 + AI 溯源** | 点 SKU 看"3 单出库+1 退货+AI 调拨建议", 每条带 audit JSON | 80h | 85% 用户首次进入 60s 内不需答疑 |
| **F2.3 Preview Before Execute 护栏** | 所有 AI 写操作 delta 预览 ✓/✗ | 50h | 覆盖 100% AI 写操作, 渲染 < 200ms |
| **F2.4 多平台订单聚合 v0** | 淘宝 + Shopify + 阿里国际站三家自动合单 + 防超卖 | 120h | 3 家试点超卖事件 = 0, 同步延迟 P95 < 60s |

### 阶段三：可恢复 + 角色化（Sprint 7-9, 300h） — *客户扩到 10 家*
> 用户真用起来第一个会爆"我刚才那个单不小心删了"。

| Feature | One-liner | 工时 | 量化目标 |
|---|---|---|---|
| **F3.1 Time Machine** | IndexedDB 草稿 + 30 秒全局 Cmd+Z + 单据时间线 | 60h | 客户访谈"丢工作"投诉 = 0, Cmd+Z 月调用 ≥ 5 次/活跃 |
| **F3.2 30 秒 Aha Role Atrium** | 按 role+persona 直出 3 种首屏（采购员/仓管/老板） | 70h | 新用户首次操作 < 90s, 30 天留存 ≥ 70% |
| **F3.3 Hub NLQ MVP** | ⌘K 输"上月哪 5 个 SKU 毛利最高" → SQL → 表 → 导出 | 120h | 50 条真实问题语料准确率 ≥ 80% |
| **F3.4 微信/钉钉机器人推送** | 缺货预警、补货建议、周报推群, 群里 ✓ 同意即下单 | 50h | 试点客户人均 ≥ 10 条/周, 确认转化 ≥ 30% |

### 阶段四：信任打钉 + 跑量（Sprint 10-12, 200h） — *R1 收口*
> 前 9 sprint 加东西, 最后 3 sprint 打磨信任锚。

| Feature | One-liner | 工时 | 量化目标 |
|---|---|---|---|
| **F4.1 Trust Center 审计页** | 老板进设置 → 全字段 diff + 谁/何时/为什么 + 导出 | 50h | 老板"查岗"场景 100% 覆盖, 搜索 P95 < 1s |
| **F4.2 OCR 票据录入** | 拍送货单照片 → AI 抽 SKU+数量+金额 → 预览入库 | 70h | 零售客户首月 OCR 录单占比 ≥ 30%, 字段准确率 ≥ 85% |
| **F4.3 性能基线 + 虚拟滚动** | 5k 行列表 60fps, 路由切换 < 200ms | 40h | 10k SKU 首屏 TTI < 500ms, 交互 P95 < 100ms |
| **F4.4 Billing × Usage Meter** | 账户页显示"本月 AI / OCR / 同步条数 / 配额剩余" | 40h | 续费决策访谈提到"看得见用量"= 加分项 |

**总工时**: 1014h / 12 sprint, 按 2 人并行 70% 效率（56h/sprint/人）容量 1344h, 缓冲 25% 应对假设证伪转向（F1.1 释放 36h 后修订）。

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

## 北极星指标 + 3 个 falsifiable 假设（First-90-Days）

### North Star
**Weekly Active Decisions (WAD)** = 每周用户基于 Tally 建议实际执行的 PO 数量
- 90 天目标：累计 ≥ 80 WAD（10 客户 × 平均 8 PO/月）
- 每周一 9:00 自动出报表

### Input metrics
1. 试用注册 → onboarding 成功转化率 ≥ 50%（周度）
2. 第 14 天活跃留存 ≥ 60%（每周 cohort）
3. 创始人 1v1 客户访谈 ≥ 5 次/周（周一上午 review）

### Anti-metric
**功能数量增长 ≤ 3 个/月**。某周新增 5 功能但 WAD 没涨 = 在自嗨。功能数÷WAD 越低越健康。

### 3 个 falsifiable 假设
| 假设 | 验证方法 | 证伪 → pivot |
|---|---|---|
| **H1**: 跨境老板愿意为"多平台合单 + AI 周一补货建议"付 ¥3000/年 | 8 家年 GMV 200-2000 万试用 30 天 + 支付意愿表 | < 3/8 选 ¥3000+ → 砍包月策略, 改订单量阶梯或回退老板个人订阅 |
| **H2**: 零售老板娘愿意把 OCR + 微信群机器人作为唯一进销存（取代手抄+Excel）| 5 家社区五金店/杂货铺线下陪跑 2 周 | < 3/5 完全停用 Excel → retail persona 砍"全替代", 可能整体退出 retail |
| **H3**: ⌘K + AI Drawer 双件套让付费客户 90 天 DAU 渗透率 ≥ 60% | 前 10 客户埋点不培训不发邮件 | < 40%（⌘K）或 < 30%（Drawer）→ "Linear 级体验"在 SMB 不成立, 重评估差异化路径 |

### 3 个 Kill Switch 信号（任一红 2 周连续 = 召开 pivot 会议）
1. 前 10 客户 onboarding 完成率 < 40% → ICP 错 / 数据导入太复杂 / AI 建议不可信
2. 第 45 天 PO 实际下单率 < 20%（AI 建议金额 ±20% 内）→ 算法不准 / 采购流程不在 Tally / 决策 SaaS 不成立
3. 90 天试用付费转化率 < 30% → 价格太高 / 价值不持续 / SaaS 模式不成立

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

### 新建
- `internal/pkg/llmgateway/` — Gateway（限流 + prompt versioning + 成本统计 + Hub 切流）
- `internal/pkg/dblock/` — 通用 advisory lock utility
- `internal/domain/channel/` + `internal/app/channel/` + `internal/adapter/platform_ext/{taobao,pdd,douyin,a1688,shopify}/`
- `internal/domain/replenishment/` + `internal/app/replenishment/{engine,narrator,po_builder}.go`
- `cmd/tally-mcp/manifest.go` — 3 套 persona manifest
- Migrations 32-37（platform_channel/order/order_line + audit_log ALTER + replenishment_suggestion + mv_stock_movement_enriched + PAT.persona）
- `web/app/(dashboard)/channels/` + `replenishment/` + `stock/[id]/timeline/` + `settings/api-tokens/`
- `web/hooks/usePageContext.ts` — Palette/Drawer context 注入

### 改既有
- `internal/adapter/platform/agent.go` — 当前 TODO 桩, 填掉接 Kova `http://kova-rest:3002/v1/workflows/replenishment/runs`
- `cmd/tally-mcp/resources.go` — 抽 manifest 工厂

---

## 验证

### 每个 sprint 的"算完成"门
每 feature 必须配 3 件：① 失败测试 → pass（Karpathy 第 4 原则）② 量化目标实测数（不是"我觉得"）③ 1 个客户访谈反馈 / 内测 score。

### 阶段 gate
- **Sprint 3 末**：STAGE 5 内部账号可下单看库存, ⌘K demo 通过 10 分钟客户演示
- **Sprint 6 末**：3 家 lighthouse 客户在 STAGE 跑实单（不发 PROD）, WAD ≥ 10/周
- **Sprint 9 末**：10 家试用, 前 14 天活跃留存 ≥ 60%, Cmd+Z 月调用 ≥ 5 次/活跃用户
- **Sprint 12 末（R1 收口）**：至少 3 家付费（¥299/月或 ¥3588/年）, 北极星 WAD ≥ 80 累计, 3 个假设至少 1 个验真

### 工具
- 埋点：`onboarding_first_po_exported` / `palette_invocation` / `ai_drawer_open` / `plan_accept_rate`（每个量化目标埋一个事件）
- LLM 成本：Prometheus `tally_llm_cost_cny_total{tenant,model}` + 周度报表
- 客户访谈：录音 → 周一上午全团队过 10 个回访

### 不通过的处理
- 任一阶段 gate 不过 → 不进下一阶段, 加 sprint 修；最多加 2 sprint, 再不过启动 pivot 会议（按 3 个 Kill Switch 决策）

---

## Out of Scope（本计划不含）

- 实际写代码、改 commit、推 PR — 本计划只是路线图, 待 BMAD story 拆解后逐 sprint 落地
- 新建 BMAD epic / story 文件 — 落地阶段再做
- 联系真实客户 / 投广告 — GTM 描述的渠道动作待用户决定何时启动
- 部署 STAGE — 已完成 24d 前; F1.1 仅做端到端验证 + 漂移补齐

---

## 下一步

待用户拍板后, 由后续 session 按阶段拆 BMAD story:
1. F1.1 STAGE 端到端验证 → 浏览器走 5 账号 Zitadel 登录 → 看库存 → 建草稿单; 缺啥补啥
2. F1.2/F1.3/F1.4 → 落到 E11 / E17 / E18 现有 story 编号下
3. 阶段二/三/四 → 按上方"与现有 Epics 映射"表新增或扩展 story
