# 垂直行业版开版配方(Vertical Edition Recipe)

> 本文档是只读配方手册,记录 2b-svc-tally 作为"platform+newhub 底座长出垂直 AI 应用"的活参考实现,供下一个垂直行业版("X 业版")照单开版。**本文档不改动任何既有代码**,所有锚点均为 file:line 引用。

## 1. 配方总览

Tally 的 AI 垂直应用架构由五个可复制的层构成:

1. **后端编排(orchestrator)**:一个持有 `system prompt` 契约的 `Orchestrator`,驱动"LLM ↔ 工具"多轮循环,直到拿到最终答案或产出需要用户确认的"Plan"。
2. **工具注册(tool registry)**:一份通用 function-calling 协议风格的 `tools` 数组 + 一个按工具名分发的 `Registry.Dispatch`,把"读库存/查数据"和"改价/建单/调库存"两类工具用 SAFE/DESTRUCTIVE 严格分开。
3. **前端对话抽屉**:一个全局命令面板(⌘K)+ 一个右侧抽屉(⌘J),两者通过一个 DOM 自定义事件桥接,让"搜索"和"问 AI"共用一个入口,又不互相耦合。
4. **多租户识别**:生产走网关注入的身份头 + JWT,开发态用可信 HTTP 头直接注入,同一套 `middleware.GetTenantID` 读取路径对两种模式透明。
5. **计费/网关接线**:AI 端点走一个可插拔的 entitlement gate(有价才有权),LLM 调用统一走配置化的网关 base_url/key,不硬编码任何厂商端点。

可复制的原因:这五层彼此解耦、依赖抽象接口(`ProductRepo`/`StockRepo`/`PlanExecutor`/`AuditWriter`),换一个行业只需要换"工具的业务实现"和"system prompt 里的领域知识+触发词",编排循环、Plan 确认机制、前端抽屉、租户识别、计费接线四件事原样搬走即可。

## 2. 后端编排配方 — `internal/app/ai/orchestrator.go`

### 2.1 system prompt 契约(tool-first policy)

锚点:`internal/app/ai/orchestrator.go:100-117`

```go
const systemPrompt = `You are an expert inventory management assistant for Tally, a Chinese B2B ERP system.
...
CRITICAL — tool-first policy: if any available tool can plausibly answer the user's question, you MUST call it in this same turn BEFORE writing any reply text. Never respond with a menu of options, a feature list, or a clarifying question when a tool call would produce a real answer — call the tool first, then explain the result using the returned data. Only ask the user a clarifying question after a tool call has already run and its result was empty or genuinely ambiguous.

Common Chinese phrasing → tool mapping (resolve intent to a tool call, do not ask which one the user means):
- 补货 / 该进多少货 / 复库 / 缺货预警 / 库存不够 → list_low_stock (then propose_create_purchase_draft if the user wants an order placed)
- 滞销 / 呆滞 / 库存积压 / 卖不动 → list_dead_stock
- 毛利 / 利润率 最低 / 最高 → gross_margin_summary
- 畅销 / 爆款 / 排行 / 卖得最好 → recent_sales_top
- 库存总体情况 / 仓库概况 → get_stock_summary
- ABC分类 / 帕累托 → abc_classify
...`
```

**为什么这一段是配方的核心**:2026-07-06 的 E2E 复测发现,计算/排序类中文问题("帮我算A仓补货""毛利最低的商品有哪些?")会被模型回复成一段"菜单式"文字(零工具调用),而查询类问题("上月哪些SKU滞销?")能正确触发工具。根因不是架构缺陷,而是 prompt 对"何时必须调工具"交代不够硬;修复是纯 prompt/description 层面的(见 `internal/app/ai/tool_first_policy_test.go:8-16` 的注释说明),没有改任何调用循环代码。**换行业时,这条"tool-first policy" + "中文短语→工具名映射表"必须原样保留结构,只替换领域短语和工具名。**

对应回归测试锚点(未提交,新建文件):`internal/app/ai/tool_first_policy_test.go:24-33`(断言 prompt 含 "tool-first policy" / "MUST call it in this same turn" / "Never respond with a menu of options")、`:40-58`(断言中文触发词→工具名映射表存在)、`:65-89`(断言每个工具的 `Description` 字段里也嵌了同样的中文关键词,双重兜底)。

### 2.2 多轮工具调用循环

锚点:`internal/app/ai/orchestrator.go:143-244`(`Chat`,非流式)与 `:248-353`(`StreamChat`,SSE)

关键结构(以 `Chat` 为例,`:157-241`):
- `maxToolRounds = 6`(`:139`)防止死循环。
- 每轮先调 LLM(`o.llm.Chat`),若 `choice.Message.ToolCalls` 为空 → 已是终态答案,直接返回(`:178-195`)。
- 否则把完整的 assistant 消息(含 `tool_calls`)原样 append 回消息列表(`:198`,注释标注"reasoning_content must survive"),再逐个 `o.registry.Dispatch` 执行工具(`:201-231`),destructive 工具返回的 `Plan` 会被 `planStore.SavePlan` 持久化(`:212-223`),工具结果以 `role: "tool"` 消息 append 回去,进入下一轮。

`ConfirmPlan`(`:370-419`)是 Plan 确认后的执行入口:先把状态翻成 `Confirmed` 当并发锁(`:392-396`),执行失败落终态 `Failed` 而非回退 `Pending`(`:404-416`,注释解释了为什么不能允许立即重试——部分行可能已经改过,重试会二次生效)。

## 3. 工具注册套路 — `internal/app/ai/tools.go`

### 3.1 工具定义模式

锚点:`ToolDefs()` `internal/app/ai/tools.go:101-229`

每个 SAFE 工具的写法套路(以 `list_low_stock` 为例,`:123-132`):
```go
{Type: "function", Function: llmclient.FunctionDef{
    Name:        "list_low_stock",
    Description: "Lists SKUs where current quantity is below the re-order point (ROP). Returns product name, qty, ROP, and days of supply. Call this for 补货/该进多少货/复库/缺货预警/库存不够 questions — it IS the replenishment calculation.",
    Parameters: mustJSON(map[string]interface{}{
        "type": "object",
        "properties": map[string]interface{}{
            "threshold_days": map[string]interface{}{"type": "integer", "description": "Days of supply threshold (default 7)"},
        },
    }),
}},
```
套路要点:①`Description` 英文技术定义之外必须直接嵌入中文触发词(不是只放系统提示里),让模型的语义工具选择也被这些关键词吸引;②`Parameters` 用 `mustJSON` 包一层 map→JSON Schema,参数都给默认值兜底,减少模型漏填参数的失败面。

DESTRUCTIVE 工具的写法套路(以 `propose_price_change` 为例,`:184-195`):`Description` 前缀写死 `"DESTRUCTIVE: ..."` + 明示"返回 plan_id,不会立即执行",配合 system prompt 里 `:115` 那句"For DESTRUCTIVE operations ... you MUST call the propose_* tools ... you do NOT execute them directly",双重约束模型不能绕过确认直接改数据。

### 3.2 分发(handler 接法)

锚点:`Registry.Dispatch` `internal/app/ai/tools.go:241-281`

一个 `switch call.Function.Name` 把工具名路由到对应私有方法(`:247-271`),SAFE 工具只返回 `(string, error)`(如 `:285` 的 `searchProducts`),DESTRUCTIVE 工具返回 `(*domainai.Plan, string, error)`(如 `:687` 的 `proposePriceChange`)。`Registry` 本身只依赖四个只读接口 `ProductRepo`/`StockRepo`/`SaleRepo`/`ExchangeRateRepo`(`:57-80`)——**换行业时,新建一个新的 Registry(或在同一个 Registry 上新增方法),把这四个接口换成新领域的只读仓储接口,`ToolDefs()`/`Dispatch()` 的骨架照抄。**

## 4. 前端三件

### 4.1 命令面板 ⌘K 直进 AI 态 — `web/components/command-palette/Palette.tsx`

- ⌘K 快捷键绑定,打开即把 `aiMode` 设为 `true`(直接进入"AI 提问态",不需要 Tab 切换):`web/components/command-palette/Palette.tsx:71-82`
- 空态下展示可点击的"试试这样问"起始问题清单 `AI_STARTERS`:`web/components/command-palette/Palette.tsx:28-33`
- 结果排序:AI 问答项(`AI_ASK_ACTION`)永远排在实体搜索结果和静态页面/操作之前:`web/components/command-palette/Palette.tsx:143-154`
- 选中 AI 问答项后关闭面板并回调 `onAIQuery`:`web/components/command-palette/Palette.tsx:193-199`

### 4.2 桥接层 — `web/components/ai-assistant/GlobalAI.tsx`

Palette 和 Drawer 之间**不做 prop drilling**,而是用一个 DOM `CustomEvent("tally:ai-query")` 桥接:`web/components/ai-assistant/GlobalAI.tsx:33-47`(`handleAIQuery` 把 query 塞进事件 detail 广播出去,`CommandPalette`/`AIDrawer` 各自独立挂载,互不感知对方存在)。

### 4.3 AI 对话抽屉 — `web/components/ai-assistant/Drawer.tsx`

- ⌘J 快捷键切换抽屉开关:`web/components/ai-assistant/Drawer.tsx:63-70`
- 监听 `tally:ai-query` 事件,收到后打开抽屉并自动发送该 query(`pendingAutoSend`):`web/components/ai-assistant/Drawer.tsx:72-83`,自动发送的执行逻辑在 `:95-174`(与手动 `sendMessage` 重复实现,注释说明是为规避 state 闭包时序问题)。
- 手动发送入口 `sendMessage`(`:193-282`):append 用户消息 → append 一条 `isStreaming: true` 的空 assistant 消息占位 → 调 `streamChat` 传 `onChunk`/`onPlan`/`onDone`/`onError` 四个回调,分别把增量文本拼进最后一条消息、把 Plan 卡片塞进 `plans` 数组、标记流结束、兜底展示错误文案。
- 会话历史落 `localStorage`(键 `tally_ai_history`,上限 50 条):`web/components/ai-assistant/Drawer.tsx:14-35`。

### 4.4 消息渲染 + SSE 流式 — `web/components/ai-assistant/MessageList.tsx` / `web/lib/api/ai.ts`

- SSE 传输层(裸 `fetch` + `ReadableStream`,绕开常规 `apiFetch`):`web/lib/api/ai.ts:4-8`,事件类型 `chunk|plan|done|error`:`web/lib/api/ai.ts:65-70`。
- 消息列表渲染:流式中显示打字机光标,流结束后把纯文本升级成富结果卡片(`AssistantContent`):`web/components/ai-assistant/MessageList.tsx:79-102`;destructive 工具产生的 Plan 在消息气泡下方渲染为 `PlanCard`,带确认/取消回调:`web/components/ai-assistant/MessageList.tsx:104-116`。
- 空态起始问题清单(展示"库存/畅销/呆滞/毛利"四类只读能力,引导首次用户"就地问"而不是学查询语法):`web/components/ai-assistant/MessageList.tsx:27-32`。

## 5. 多租户 + 计费接线

### 5.1 租户识别

- 生产态:`OIDCIssuer` 非空时用真实 OIDC/JWT 中间件(config 注释见 `internal/pkg/config/config.go:58-62`)。
- 开发态(`TALLY_DEV_MODE=true` 才允许,见 `internal/pkg/config/config.go:161-167` 的 `devMode` 校验):服务信任 `X-IDP-Subject`/`X-Email`/`X-Display-Name`/`X-Tenant-ID` 四个 HTTP 头直接注入身份,**绝不会在生产触达**(`internal/lifecycle/app.go:553-563` 的分支注释明确写了"NEVER reachable in prod (OIDC_ISSUER is always set there)")。
- 具体注入逻辑:`internal/lifecycle/app.go:564-589` —— 有 `X-Tenant-ID` 头就直接 `uuid.Parse`;没有则退化为按 `X-IDP-Subject` 查 `user_identity_mapping` 表解析租户,保证开发态免手动传 tenant 也能用。
- 路由层的对应说明注释:`internal/adapter/handler/router/router.go:124-126`。

### 5.2 LLM 网关接线

配置项(env var 名,不含真实值,见 `internal/pkg/config/config.go:78-89`):

| 用途 | 环境变量 | 说明 |
|---|---|---|
| 经网关配置的模型服务地址 | `NEWAPI_BASE_URL` | 默认值指向内部网关的 `/v1` 路径,留空则 AI 路由整体返回 501 |
| 网关鉴权凭证 | `NEWAPI_API_KEY` | 经 secret 注入的 bearer token,**不落库不落 git** |
| 默认模型名 | `DEFAULT_AI_MODEL` | 经网关配置的模型标识,默认值见 `internal/app/ai/orchestrator.go:44-46`(`NewOrchestrator` 里 `model == ""` 时回退的常量) |
| Plan 有效期 | `AI_PLAN_TTL_SECONDS` | destructive plan 从生成到过期的秒数,默认 1800 |
| 记忆召回(可选) | `MEMORY_BASE_URL` / `MEMORY_API_KEY` | 两者任一为空则记忆功能静默关闭,AI 主链路不受影响(`internal/pkg/config/config.go:85-89`) |

### 5.3 计费/权限接线(entitlement gate)

AI 端点通过一个可插拔的 `entGate` 中间件接入 platform 的"按订阅计划开放能力"体系:`internal/adapter/handler/ai/handler.go:69-76`(`WithEntitlementGate`,对应 `ai_assistant` entitlement)、路由挂载点 `internal/adapter/handler/ai/handler.go:78-90`(`entGate` 非空才 `ai.Use(h.entGate)`,为空则 AI 端点不设防,匹配"platform 未接线时优雅降级"的一贯姿势)。**换行业时同一个 gate 模式直接复用,只需要把 entitlement key 换成新行业版对应的 SKU 标识。**

## 6. 开 X 业版 checklist

> 目标:从 clone 到第一个领域工具跑通 E2E,以周计。以下步骤基于本仓当前代码结构提炼,不是脚手架代码。

1. **起隔离测试环境**:用 `docker-compose.dev.yml` 拉起本地 Postgres(pgvector)/Redis/NATS 三件套(不占用共享 STAGE 环境),设置 `TALLY_DEV_MODE=true` + `DATABASE_DSN`/`REDIS_URL`/`NATS_URL` 后 `go run ./cmd/server` 起服务(命令与端口 `:18200` 详见仓库根说明文档的 Commands 段)。
2. **定义领域只读仓储接口**:参照 `internal/app/ai/tools.go:56-80` 的 `ProductRepo`/`StockRepo`/`SaleRepo`/`ExchangeRateRepo` 写法,给新行业的核心实体(如制造业的"工单"、教育业的"课时包")各写一个最小只读接口。
3. **新增/改写 `Registry`**:参照 `internal/app/ai/tools.go:82-98` 的 `Registry` 结构与 `NewRegistry` 构造函数,注入上一步的领域仓储。
4. **写领域工具定义**:在 `ToolDefs()`(`internal/app/ai/tools.go:101-229`)里新增工具,SAFE 工具照 `list_low_stock` 套路(`:123-132`)写,DESTRUCTIVE 工具照 `propose_price_change` 套路(`:184-195`)写——**`Description` 里必须嵌中文领域触发词**,不能只写英文技术定义。
5. **在 `Dispatch` 里挂路由**:`internal/app/ai/tools.go:247-271` 的 `switch` 新增 `case`,分发到新方法。
6. **改写 system prompt**:`internal/app/ai/orchestrator.go:100-117` 的 persona 段换成新行业专家人设,**必须保留** `:105` 的 tool-first policy 整句(逐字保留"MUST call it in this same turn"/"Never respond with a menu of options"这两句断言锚点,回归测试会字符串匹配),`:107-113` 的映射表换成新行业的中文短语→工具名映射。
7. **补对应的静态回归测试**:参照 `internal/app/ai/tool_first_policy_test.go` 的三个测试(断言 prompt 含 tool-first 指令、断言中文映射表存在、断言每个工具 `Description` 也带中文关键词),新领域一样补一份,防止未来有人删了触发词却没人发现。
8. **前端接入**:`GlobalAI.tsx`(`web/components/ai-assistant/GlobalAI.tsx:33-47`)、`Palette.tsx`(⌘K)、`Drawer.tsx`(⌘J)三件原样复用,只需要改 `MessageList.tsx:27-32` 的 `STARTER_PROMPTS` 和 `Palette.tsx:28-33` 的 `AI_STARTERS` 为新行业的示例问题。
9. **接线计费**:复用 `internal/adapter/handler/ai/handler.go:69-76` 的 `WithEntitlementGate` 模式,把 entitlement key 换成新行业版的 SKU 标识。
10. **跑通首个 E2E**:本地起服务后,用命令面板/抽屉手动发一条领域触发词问题("以周计"的验收线是:输入触发词 → 首轮响应即为工具调用而非菜单文字 → 工具返回真实数据 → 助手用数据作答;destructive 场景则再加一步"Plan 卡片确认后侧写生效")。参照本次 2026-07-06 复测方法:同一批中文触发词分别过一遍"查询类"和"计算/排序类"两种问法,确保两类都首轮触发工具(菜单式回复视为不通过)。

### 未提交(WIP)文件清单 — 本 checklist 引用的锚点中,以下文件在写文档时 `git status` 显示为未 commit,读者对照本文档验证时请注意工作区状态可能与远端 HEAD 不一致:

```
M internal/adapter/handler/ai/handler.go
M internal/app/ai/orchestrator.go
M internal/app/ai/tools.go
M web/components/ai-assistant/Drawer.tsx
M web/components/ai-assistant/MessageList.tsx
M web/components/command-palette/Palette.tsx
M internal/pkg/config/config.go
M internal/lifecycle/app.go
M internal/adapter/handler/router/router.go
?? internal/app/ai/tool_first_policy_test.go   (新建,未跟踪)
?? web/components/ai-assistant/AssistantContent.tsx  (新建,未跟踪)
```

`web/components/ai-assistant/GlobalAI.tsx` 未出现在 `git status` 变更列表中(已是仓库既有已提交文件)。
