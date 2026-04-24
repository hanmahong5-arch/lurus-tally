# UX Benchmarks: 顶级 SaaS 产品设计原则
# 面向 Lurus Tally 智能进销存 Web 端

**模式**: technical  
**生成日期**: 2026-04-23  
**技术栈**: Next.js 14 (App Router) + shadcn/ui + Tailwind CSS + Framer Motion + TanStack Table v8 + Recharts + Tremor

---

## 1. Executive Summary

2026 年顶级 SaaS 产品的设计核心收敛为三个关键词：**速度感知、信息密度可控、键盘优先**。Linear 证明 keyboard-first 不是开发者专属，Stripe 证明数据密度与美观可以共存，Shopify Admin 证明进销存场景有成熟的列表/详情范式可以直接借鉴。对于目标客群（从管家婆/速达迁移的中国中小企业用户），体验代差本身就是产品差异化——他们从未见过这个级别的产品，第一印象的震撼效果远比功能完整性更重要。shadcn/ui + Framer Motion 的组合在技术层面完全能支撑 Linear 级别的体验，关键在于执行纪律：不做骨架，做完整。

---

## 2. 顶级 SaaS 产品深度拆解

### 2.1 Linear — 键盘优先的工程典范

Linear 的 UI 设计哲学来自一个核心命题：**每一个鼠标操作都应该有一个更快的键盘路径**。在实现层面体现为：悬停任何界面元素 2 秒后，会弹出提示 banner 告知对应快捷键，而不是强迫用户去查文档。这是"温柔引导"——用户在慢速路径中自然学到快速路径。

颜色系统：Linear 在 2025 年 UI 重设计中从 HSL 迁移到 **LCH 色彩空间**，实现"等亮度颜色在视觉上等亮"的感知均匀性。新主题系统只需三个变量（base、accent、contrast）即可生成全套 98 个色值，且自动支持高对比度无障碍主题。对 Tally 的启示：不要用 HSL 调暗黑模式，应使用 Tailwind CSS v4 的 OKLCH 色彩空间（shadcn/ui 2025 年已迁移）。[来源](https://linear.app/now/how-we-redesigned-the-linear-ui)

排版：重设计引入 **Inter Display** 用于标题以增加表达力，保留 Inter 用于正文。通过字体选型建立层级，而非依赖粗细变化。[来源](https://linear.app/now/how-we-redesigned-the-linear-ui)

速度哲学：Linear 的六周重设计过程采用"设计师-工程师每日配对"而非传统工作坊。路由切换、Panel 滑入、状态变更全部有动效但不超过 300ms。这是"速度感知"而非实际加载速度的管理。[来源](https://linear.app/now/how-we-redesigned-the-linear-ui)

### 2.2 Stripe Dashboard — 数据密集型的参考教材

Stripe Dashboard 是高密度数据 UI 的黄金标准。四张 KPI 卡片（Revenue、Charges、Payouts、Disputes）每张只有三个元素：数字、趋势箭头+百分比、Sparkline。标签不写"当前周期总收入"，只写"Revenue"。这是**信息压缩**的最高境界。

表格设计：行高根据密度分三档——condensed 40px、regular 48px、relaxed 56px。文字左对齐，数字右对齐，状态 Badge 居中对齐。表头对齐方式必须和列内容一致。Hover 时浮现 checkbox 和行内操作按钮，避免常态 UI 过度拥挤。[来源](https://www.pencilandpaper.io/articles/ux-pattern-analysis-enterprise-data-tables)

过滤与排序：默认按最新时间倒序，或按"最需要处理"优先。过滤器不以弹窗呈现，以 inline chip 呈现，选中状态清晰可见，一键清空。

金额展示：等宽数字字体（tabular-nums）是财务场景的必须项，避免数字列在不同行之间的视觉跳动。

### 2.3 Vercel Dashboard — 空状态与卡片信息架构

Vercel 新版 Dashboard 的核心贡献是**可折叠侧边栏 + 卡片网格**的组合：侧边栏给予跨模块持久导航，卡片网格混合 KPI/图表/列表，页面首屏 4-6 个最重要 KPI 排列在视口顶部 80-120px 区域，不用欢迎语浪费这块黄金地产。[来源](https://medium.com/design-bootcamp/vercels-new-dashboard-ux-what-it-teaches-us-about-developer-centric-design-93117215fe31)

空状态设计：区分两类空状态——初始空状态（Presentational）和零结果空状态（Zero Results）。前者用图标+标题+描述+CTA，CTA 用主动语态（"创建第一个商品"而非"暂无商品"）；后者用插画+清晰说明。[来源](https://supabase-design-system.vercel.app/design-system/docs/ui-patterns/empty-states)

### 2.4 Shopify Admin — 进销存的行业范式

Shopify Admin 是进销存/电商管理最成熟的列表/详情范式参考。核心交互：商品列表支持批量编辑（一次性修改多个 SKU 库存/状态），库存追踪到变体级别，多仓库库存调拨直接在 Admin 内完成。订单列表支持按金额、重量过滤和排序，配合自动化 tag 规则（高价值订单、特殊物流要求）。[来源](https://www.shopify.com/retail/inventory-management)

对 Tally 的核心借鉴：商品/SKU 列表必须支持批量操作（批量修改价格、库存预警线），订单列表必须支持多维度过滤（时间、金额区间、状态、供应商），进货单/出货单必须能从列表直接执行简单动作（确认收货、打印）而不必进入详情页。

### 2.5 Notion — 内联编辑与块状内容

Notion 的贡献是证明了**内联编辑**（点击即编辑，失焦即保存）可以大幅减少用户跳转次数。商品备注、SKU 描述、采购备注这类字段完全可以用内联编辑取代"进入编辑模式"按钮。配合 TanStack Table 的 `onCellEdit` 回调 + TanStack Query 乐观更新，可以实现零跳转的数据修改体验。

### 2.6 Raycast — Command Palette 的极致

Raycast 的 Command Palette 支持"Palette in Palette"——在搜索结果中再次触发子命令，形成嵌套的操作链。用户可以为常用命令分配 ⌘1-9 快捷键。对 Tally 的启示：Command Palette 不只是"搜索页面"，它应该是整个系统的操作入口——"创建采购单"、"查找 SKU:XX001"、"查看低库存预警"都应该可以从 ⌘K 直接触达。[来源](https://clonepartner.com/blog/ultimate-guide-attio-crm-2025)

### 2.7 Attio CRM — 灵活数据模型的现代设计语言

Attio 以 Objects（对象）/Records（记录）/Attributes（属性）三层架构为核心，UI 上体现为极度灵活的列视图和自定义字段。对 Tally 的借鉴：商品 SKU 的自定义属性字段（行业/规格/产地）可以参考 Attio 的 inline attribute 编辑模式，而非固定表单结构。[来源](https://www.saasui.design/application/attio)

### 2.8 Plain — 工单场景的精致设计

Plain 将客服工单的核心理念带入进销存：每个需要"处理"的事项（低库存预警、待确认采购单、异常订单）都是一张"卡片"，有明确的状态机（Open → In Progress → Resolved），支持 assign、comment、resolve 操作。Tally 的"待办"模块可以参照这个思路，将需要用户决策的事项结构化为可处理的卡片流。

### 2.9 Height — 项目管理的另一种现代设计语言

Height 的贡献是**多视图切换**（List、Board、Calendar、Spreadsheet）对同一份数据集的不同呈现。Tally 中，采购计划同样可以支持 List 视图（按供应商）和 Calendar 视图（按预计到货日期），这不需要额外的数据结构，只是同一份数据的不同 Projection。

---

## 3. 15+ 设计原则（可直接落地）

### P1 — 详情页用 Slide-over Sheet，不跳转

**陈述**: 点击列表行不做全页跳转，从右侧滑入 Sheet，背景保持可见。  
**学自**: Linear Issue 详情、Stripe Customer 详情  
**落地**: 商品 SKU 列表点击某行 → 右侧滑出 600px 宽 Sheet，显示 SKU 详细信息（库存曲线、近期进出记录、价格历史）。按 Esc 或点击背景关闭。操作按钮（编辑、调拨、设预警）在 Sheet 顶部右侧。  
**组件**: shadcn `<Sheet side="right">` + Framer Motion `x: "100%" → 0` 缓动 `[0.32, 0.72, 0, 1]` duration 0.3s  

### P2 — ⌘K Command Palette 是全局操作入口

**陈述**: 每个页面都可以用 ⌘K 唤起 Command Palette，支持页面跳转、实体搜索、快捷操作。  
**学自**: Linear、Raycast、Vercel  
**落地**: 输入"采购"→ 显示"创建采购单 / 查看采购记录 / 采购报表"；输入"SKU001"→ 直接跳转该商品详情；输入"盘点"→ 启动库存盘点流程。Command 列表分组：导航/操作/搜索。  
**组件**: `cmdk` + shadcn `<CommandDialog>` + 全局 `useEffect` 监听 `metaKey+k`。结构化为 `commands: CommandGroup[]`，每次打开重新 fetch 最近访问记录。

### P3 — 表格行高三档密度，用户可选

**陈述**: 默认 Regular（48px），支持切换 Compact（40px）和 Relaxed（56px），设置持久化到 localStorage。  
**学自**: Stripe Dashboard、Linear Issue List  
**落地**: 商品 SKU 列表和采购单列表都提供密度切换按钮（三个小图标放在表格右上角）。操作员偏好 Compact 看更多行，老板偏好 Relaxed 看得更清楚。  
**组件**: TanStack Table 配置 `rowHeight` 变量 + Zustand `useTableStore` 持久化。Tailwind: `h-10`(40px) / `h-12`(48px) / `h-14`(56px)

### P4 — 数字右对齐，等宽字体，金额加千分位

**陈述**: 所有数量、金额列右对齐，使用 `font-variant-numeric: tabular-nums`，金额必须千分位分隔。  
**学自**: Stripe Dashboard（金额展示标准）  
**落地**: 库存数量列（`text-right font-mono`）、采购金额列（`¥1,234,567.00`）、库存价值列。中文金额大写在打印视图/导出时提供（`壹百贰拾叁万肆仟伍佰陆拾柒元整`）。  
**组件**: Tailwind `tabular-nums` class，自定义 `formatCNY(amount: number): string` 工具函数处理千分位和大写转换。

### P5 — Hover 时浮现行内操作，不常显

**陈述**: 表格行操作按钮（编辑、删除、打印、调拨）仅在 hover 时显示，常态 UI 保持干净。  
**学自**: Stripe、Shopify Admin  
**落地**: 采购单列表 → hover 行尾显示"确认收货 / 查看详情 / 打印"三个图标按钮。库存列表 → hover 显示"调拨 / 设预警"。批量选中时，顶部浮现批量操作 toolbar（batch delete、batch export）。  
**组件**: `group` Tailwind class + `group-hover:opacity-100 opacity-0 transition-opacity` 控制按钮显隐。

### P6 — 乐观更新 + Undo Toast，不打断用户

**陈述**: 状态变更（确认采购、调整库存）立即在 UI 反映，后台异步提交，失败时 Toast 提供 Undo。  
**学自**: Linear（Issue 状态切换）、Notion（块状态）  
**落地**: 采购单点击"确认收货"→ 状态立即变为"已入库"（乐观更新），底部 Toast 显示"已确认入库 [撤销]"，3s 后消失。网络失败 → Toast 变红提示失败，状态回滚。  
**组件**: TanStack Query `useMutation` 的 `onMutate` + `onError` 回滚 + `sonner` Toast library。

### P7 — 空状态主动引导，不消极展示

**陈述**: 空状态用主动语态 CTA，初始空状态和零结果分别设计，绝不用"暂无数据"结束对话。  
**学自**: Vercel Dashboard、Supabase  
**落地**: 新用户商品列表空状态："还没有商品 → [批量导入商品] [手动添加第一个]"；过滤器无结果："找不到匹配的采购单 → [清除过滤条件] [调整日期范围]"。  
**组件**: 自定义 `<EmptyState icon title description actions>` 组件，shadcn `<Button>` CTA。

### P8 — 骨架屏优先，Spinner 兜底

**陈述**: 数据加载时展示与真实布局形状匹配的骨架屏，只有明确耗时操作（导出、批量操作）用 Spinner。  
**学自**: Linear、Vercel  
**落地**: 商品列表页首次加载：骨架屏显示 8 行表格占位（灰色矩形，宽度与列定义匹配）；KPI 卡片骨架屏匹配卡片尺寸。报表生成按钮点击后：按钮内显示 Spinner，禁止重复点击。  
**组件**: shadcn `<Skeleton>` + Tailwind `animate-pulse`。Skeleton 组件按页面类型复用。

### P9 — 侧边栏可折叠，内容最大化

**陈述**: 主导航侧边栏支持折叠到 icon-only 模式（48px），展开为 220px，状态持久化。  
**学自**: Vercel 新 Dashboard、Linear  
**落地**: 折叠时显示图标 + Tooltip；展开时显示图标 + 文字 + 徽章（待处理数量）。折叠/展开有平滑过渡动画。移动端自动收起为 Drawer。  
**组件**: Zustand `useSidebarStore`，Framer Motion `width: 220 ↔ 48` animate，shadcn `<Tooltip>` 在 icon-only 模式。

### P10 — 键盘快捷键提示随 Hover 自然显示

**陈述**: 任何有快捷键的按钮/菜单项，hover 时在 Tooltip 中显示快捷键，不额外教程。  
**学自**: Linear（hover 2s 显示 banner）  
**落地**: 创建采购单按钮 Tooltip：`创建采购单 (⌘N)`；保存按钮：`保存 (⌘S)`；搜索框：`搜索 (⌘/)` 或 `打开命令面板 (⌘K)`。侧边栏 icon-only 模式 Tooltip 也显示快捷键。  
**组件**: shadcn `<TooltipContent>` 内包含 `<kbd>` 元素，Tailwind `font-mono text-xs bg-muted px-1 rounded`。

### P11 — 多步表单用 Stepper 布局，不跨页

**陈述**: 创建采购单等多步骤操作在同一页面用 Stepper 呈现，步骤状态可见，支持返回上一步。  
**学自**: Stripe（支付流程）、Shopify Admin（订单创建）  
**落地**: 创建采购单三步：[1. 选择供应商] → [2. 添加商品明细] → [3. 确认与提交]。Stepper 在页面顶部，当前步骤高亮。步骤 2 用行内表格输入（商品/数量/单价），实时汇总金额。  
**组件**: 自定义 `<Stepper>` 组件（shadcn 无内置，基于 `<ol>` + Tailwind），`useFormContext` (React Hook Form) 跨步骤共享数据，Zod 分步校验。

### P12 — AI 助手固定在右侧 Drawer

**陈述**: AI 助手作为常驻 Drawer 固定在页面右侧，不遮挡主内容，支持收起。  
**学自**: Notion AI sidebar、Linear AI  
**落地**: 右上角 AI 图标按钮 → 从右侧滑出 380px Drawer（不覆盖，主内容区 margin-right 响应式缩窄）。Drawer 内：对话历史 + 输入框。AI 可直接操作数据（"帮我查一下 SKU001 本月库存变化"→ 直接返回图表）。  
**组件**: shadcn `<Sheet>` 非遮罩模式 + `useChat` (Vercel AI SDK) 流式输出，Framer Motion `x: "100%" → 0`。

### P13 — 报表页 KPI 卡 + 图表，数据可下钻

**陈述**: 报表页顶部 4-6 个 KPI 卡（当月销售额/毛利/库存周转率/欠款），点击卡片可下钻到该指标的详细报表。  
**学自**: Stripe Dashboard、Vercel Analytics  
**落地**: KPI 卡显示：当前值 + 较上月涨跌（颜色+箭头）+ Spark 折线图。点击"毛利"→ 展开毛利明细表（按商品类别/供应商分组）。时间范围选择器（本周/本月/本季/自定义）全局控制所有图表联动。  
**组件**: Tremor `<Card>` + `<BadgeDelta>` + `<SparkAreaChart>`；Recharts `<AreaChart>` 用于详情图表；shadcn `<DateRangePicker>`。

### P14 — 搜索实时过滤，不需要按回车

**陈述**: 表格顶部搜索框 debounce 300ms 实时过滤，不需要按 Enter，清空按钮常驻。  
**学自**: Linear Issue 搜索、Attio Record 搜索  
**落地**: 商品列表搜索框（⌘/ 聚焦）输入"台灯"→ 300ms 后表格实时过滤（前端过滤 <1000 条，后端 API 过滤 >1000 条）。搜索框右侧 X 按钮清空。过滤条件以 chip 形式展示在搜索框下方，可逐个删除。  
**组件**: shadcn `<Input>` + `useDebounce` hook，TanStack Table `columnFilters` state，Tailwind chip: `bg-secondary text-secondary-foreground px-2 py-1 rounded-full text-sm`。

### P15 — 打印/导出独立视图，不套用屏幕样式

**陈述**: 采购单、库存报表的打印导出有独立的打印专用 CSS，显示中文大写金额、公司抬头、页码。  
**学自**: Stripe Dashboard（Invoice 打印）  
**落地**: 采购单详情页"打印"按钮 → `window.print()` 触发 `@media print` 样式，隐藏侧边栏/导航/操作按钮，显示公司名称/采购单号/二维码/中文大写金额。每页打印添加页脚"第 X 页/共 Y 页"。  
**组件**: Tailwind `print:hidden` / `print:block` 工具类，自定义 print stylesheet，`react-to-print` 库。

### P16 — 状态机颜色语义化，红色只用于必须处理

**陈述**: 颜色严格遵循语义：绿=健康/已完成、黄=待处理/预警、红=紧急/错误、灰=归档/无效。红色只用于"现在必须处理"的场景。  
**学自**: Stripe（Dispute 红色警告）、Linear（状态颜色系统）  
**落地**: 库存预警：库存量 < 安全库存 50% → 黄色；< 安全库存 20% → 红色（缺货风险）。采购单状态：草稿=灰、待确认=黄、已确认=蓝、已入库=绿、已取消=灰删除线。  
**组件**: shadcn `<Badge>` variant 扩展（warning/danger/success/neutral）+ Tailwind 语义 class map。

### P17 — 路由切换 < 100ms，Next.js prefetch 全开

**陈述**: 侧边栏导航所有链接默认 prefetch，页面切换无白屏，用 Framer Motion page transition 遮盖网络延迟感知。  
**学自**: Vercel Dashboard、Linear  
**落地**: `<Link prefetch={true}>` 用于侧边栏所有导航项。页面级 `<motion.div initial={opacity:0} animate={opacity:1} transition={duration:0.15}>` 淡入过渡遮盖延迟感。Server Component 优先，Client Component 仅在需要交互的叶子节点。  
**组件**: Next.js `<Link>` prefetch，`next/navigation` router，Framer Motion page wrapper。

---

## 4. 核心页面设计建议

### 4.1 仪表盘 (Dashboard)

```
┌─────────────────────────────────────────────────────────────────┐
│  [≡ Tally]  商品  采购  销售  库存  报表  [⌘K搜索]  [🔔3]  [AI▶] │
├──────────┬──────────────────────────────────────────────────────┤
│          │ ┌──────┐ ┌──────┐ ┌──────┐ ┌──────┐                 │
│ 侧边栏   │ │月销售│ │毛利率│ │库存值│ │欠款  │  ← KPI 卡片行    │
│          │ │¥128k │ │23.4% │ │¥89k  │ │¥34k  │                 │
│ 首页     │ │↑12%  │ │↓1.2% │ │ 稳定 │ │⚠3客户│                 │
│ 商品     │ └──────┘ └──────┘ └──────┘ └──────┘                 │
│ 采购     │ ┌───────────────────────┐ ┌──────────────────────┐   │
│ 销售 ●3  │ │ 库存趋势 (30天)       │ │ 待办事项             │   │
│ 库存     │ │ [Recharts AreaChart]  │ │ ⚠ 库存预警 x5        │   │
│ 供应商   │ │                       │ │ ○ 待确认采购单 x2    │   │
│ 客户     │ └───────────────────────┘ │ ○ 待开票销售单 x1    │   │
│ 报表     │ ┌───────────────────────┐ │                      │   │
│          │ │ 近期动态              │ └──────────────────────┘   │
│ ──────── │ │ [时间线列表]          │                            │
│ 设置     │ └───────────────────────┘                            │
└──────────┴──────────────────────────────────────────────────────┘
```

- KPI 卡片点击可下钻，趋势用 Tremor `<SparkAreaChart>`
- 待办事项用颜色区分紧急度，点击直接跳转对应页面+高亮目标行
- AI 按钮在右上角，点击展开右侧 Drawer

### 4.2 商品/SKU 列表（高密度表格）

```
┌─────────────────────────────────────────────────────────────────┐
│  商品管理                         [+新建商品] [批量导入] [导出]  │
│  ┌─────────────────┐ [状态▼] [类别▼] [库存▼]    [⊞密度] [≡]   │
│  │ 🔍 搜索商品名/SKU│                                           │
│  └─────────────────┘                                           │
│  ○  图片  商品名称          SKU       库存    成本价   售价  状态│
│  ○  [□]  台灯 A款           TD-A001   ⚠ 15   ¥38.00  ¥89  ●上架│
│  ○  [□]  台灯 B款 USB      TD-B001   124    ¥42.00  ¥99  ●上架 │
│  ○  [□]  充电宝 20000mAh   CB-001    🔴 3   ¥68.00  ¥149 ●上架 │
│  ○  [□]  蓝牙耳机 Pro       BT-001    89     ¥95.00  ¥299 ○下架 │
│                                                                 │
│  [← 上一页]  第 1/24 页  [下一页 →]  每页 20 ▼   共 478 个商品  │
└─────────────────────────────────────────────────────────────────┘
```

- 库存列：绿=充足，黄=预警（⚠），红=紧急缺货（🔴数字）
- 行 hover 时尾部浮现：[编辑] [调拨] [停售] 三个 icon 按钮
- 批量选中后顶部浮现 action bar：[批量改价] [批量调拨] [批量导出] [删除]
- >1000 行时启用 TanStack Virtual 虚拟滚动

### 4.3 商品详情 Slide-over Sheet

```
主内容区                    │  商品详情                     ×
（列表仍可见，半透明遮罩）   │  台灯 A款 TD-A001            [编辑]
                             │  ─────────────────────────────
                             │  [基本信息] [库存记录] [价格历史]
                             │
                             │  当前库存
                             │  15 件  ⚠ 低于安全库存(50件)
                             │  [库存趋势 Sparkline - 30天]
                             │
                             │  成本价: ¥38.00
                             │  建议售价: ¥89.00
                             │  毛利率: 57.3%
                             │
                             │  供应商: 广州台灯厂
                             │  最近入库: 2026-04-15 (50件)
                             │
                             │  [发起采购] [查看完整记录]
```

- 宽度固定 600px，高度全屏
- Sheet 内 Tab 切换（基本信息/库存记录/价格历史），不跳转页面
- Framer Motion: `x: "100%"` → `x: 0`，`[0.32, 0.72, 0, 1]`，250ms

### 4.4 采购单创建（单页 Stepper，不跨页）

```
  [1. 选择供应商] ──── [2. 添加商品明细] ──── [3. 确认提交]
                                ↑ 当前步骤

  供应商: 广州台灯厂              预计到货: [2026-05-10]
  ─────────────────────────────────────────────────────
  商品名称          SKU      数量    单价       小计
  [搜索添加商品...] ________  ____   ________   ─────
  台灯 A款          TD-A001  [100]  [¥35.00]   ¥3,500
  台灯 B款 USB      TD-B001  [ 50]  [¥40.00]   ¥2,000
  [+ 添加一行]
  ─────────────────────────────────────────────────────
                              合计金额:         ¥5,500.00
  备注: [______________________________]

                              [← 上一步]  [下一步 →]
```

- 步骤 2 的商品明细表行内可编辑（数量/单价直接点击输入），实时汇总
- 保存草稿按钮常驻（⌘S），不必完成所有步骤
- 提交后乐观更新 → Toast "采购单 PO-20260423-001 已创建"

### 4.5 库存盘点流程（Web 端）

```
┌─────────────────────────────────────────────────────────────────┐
│  库存盘点  2026年4月盘点  [导出差异报告]                          │
│  进度: 247 / 478 已盘点  ████████████░░░░░░░ 51%               │
│  ─────────────────────────────────────────────────────────────  │
│  SKU 搜索/扫码输入:  [________________] → [确认盘点]            │
│                                                                 │
│  商品名称        SKU      账面库存  实盘数量  差异               │
│  台灯 A款        TD-A001  50       [48   ]   ▼-2 (输入中)       │
│  台灯 B款 USB    TD-B001  124      124       ✓ 无差异           │
│  充电宝 20000mAh CB-001   25       [  ]      待盘点             │
└─────────────────────────────────────────────────────────────────┘
```

- 实盘数量列直接行内 input，Tab 键跳下一行，Enter 确认
- 差异列实时计算，红色高亮差异大的行
- 进度条让用户知道盘点进度
- 扫码输入框（PC 端条码枪输入）自动定位到对应行

### 4.6 报表页

```
┌──── 时间范围: [本月▼] ──────────────────────────────────────────┐
│ ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐            │
│ │ 销售额    │ │ 毛利     │ │ 库存周转 │ │ 应收欠款 │            │
│ │ ¥128,450 │ │ ¥29,943  │ │ 3.2次/月 │ │ ¥34,200  │            │
│ │ ↑12% 环比│ │ 23.3%    │ │ ↑0.3次   │ │ ⚠3客户超期│           │
│ └──────────┘ └──────────┘ └──────────┘ └──────────┘            │
│                                                                 │
│ [销售趋势 - Recharts AreaChart, 30天]                           │
│                                                                 │
│ [商品销量排行 Top10 - Recharts BarChart] │ [库存结构 PieChart]  │
└─────────────────────────────────────────────────────────────────┘
```

### 4.7 AI 助手对话面板（右侧固定 Drawer）

```
主内容区 (margin-right: 380px)  │  AI 助手                  ×
                                 │  ─────────────────────────
                                 │  你好！我可以帮你：
                                 │  • 查询库存和销售数据
                                 │  • 分析进货策略
                                 │  • 生成盘点计划
                                 │
                                 │  [用户] 台灯 A款快断货了怎么办
                                 │
                                 │  [AI] 台灯 A款(TD-A001)当前
                                 │  库存 15 件，低于安全库存
                                 │  50 件。按过去 30 天日均
                                 │  销量 3.2 件，预计 4-5 天
                                 │  售罄。
                                 │
                                 │  建议立即向广州台灯厂补货
                                 │  100 件（上次采购价 ¥35）。
                                 │  [→ 创建采购单]
                                 │  ─────────────────────────
                                 │  [输入消息...          发送]
```

- Drawer 不覆盖主内容，主内容 `margin-right` 过渡动画
- AI 回复支持流式输出（Vercel AI SDK `useChat`）
- 回复中可嵌入操作按钮（[创建采购单]），点击直接执行

### 4.8 ⌘K Command Palette

```
┌─────────────────────────────────────────────────────────────────┐
│  ╔═══════════════════════════════════════════════════════════╗   │
│  ║ 🔍 输入命令或搜索...                                      ║   │
│  ╠═══════════════════════════════════════════════════════════╣   │
│  ║ 最近访问                                                  ║   │
│  ║   📦 台灯 A款 TD-A001                           商品      ║   │
│  ║   📋 PO-20260420-003 广州台灯厂                 采购单    ║   │
│  ║ 快捷操作                                                  ║   │
│  ║   ➕ 创建采购单                                  ⌘N       ║   │
│  ║   📊 打开报表                                    ⌘R       ║   │
│  ║   🔔 查看预警                                             ║   │
│  ║ 导航                                                      ║   │
│  ║   🏠 首页仪表盘                                           ║   │
│  ║   📦 商品管理                                             ║   │
│  ╚═══════════════════════════════════════════════════════════╝   │
└─────────────────────────────────────────────────────────────────┘
```

---

## 5. 动效与交互细节清单

### 缓动曲线标准

| 场景 | 曲线 | 时长 |
|------|------|------|
| Sheet / Drawer 滑入 | `[0.32, 0.72, 0, 1]` (iOS spring 近似) | 250-300ms |
| Dialog 弹出 | `easeOut` | 200ms |
| 元素进入视口 | `[0.22, 1, 0.36, 1]` | 300ms |
| 数值变化 | `easeInOut` | 150ms |
| Toast 滑入 | `easeOut` | 180ms |
| Page transition | `opacity 0→1` | 150ms |

### 列表 Stagger 动画

```ts
// 列表项入场，stagger ≤ 80ms
const container = {
  visible: {
    transition: { staggerChildren: 0.06, delayChildren: 0.05 }
  }
}
const item = {
  hidden: { opacity: 0, y: 8 },
  visible: { opacity: 1, y: 0, transition: { duration: 0.25, ease: [0.22, 1, 0.36, 1] } }
}
```

超过 20 行的表格**不做** stagger，直接显示，避免用户等待。

### CountUp 数字动画

KPI 卡片数字变化时使用 `react-countup` 或自定义 `useCountUp` hook，duration 1.0-1.5s，easeOut。金额变化（如 ¥128,450 → ¥142,300）视觉上非常抓眼球，增强仪表盘"活"的感知。

### 加载状态策略

```
首次加载:   骨架屏 (Skeleton)
刷新数据:   Stale data 保留显示 + 右上角细线 loading bar (nprogress 风格)
操作提交:   按钮内 Spinner + 禁用状态
页面跳转:   Prefetch 完成则无感知，未完成则顶部 loading bar
导出/批量:  全屏 overlay + 进度百分比
```

### 错误状态

- 表单字段：红色 border + 字段下方 inline 错误信息（不用 Toast）
- API 失败：Toast（sonner）显示错误原因 + 重试按钮，3-5s 自动消失
- 网络断开：顶部 banner "网络连接已断开，数据可能不是最新" + 黄色背景
- 乐观更新回滚：Toast "操作失败，已恢复" + Undo 按钮 5s 倒计时

### 乐观更新模式（TanStack Query）

```ts
useMutation({
  mutationFn: confirmPurchaseOrder,
  onMutate: async (id) => {
    await queryClient.cancelQueries({ queryKey: ['orders'] })
    const prev = queryClient.getQueryData(['orders'])
    queryClient.setQueryData(['orders'], (old) =>
      old.map(o => o.id === id ? { ...o, status: 'confirmed' } : o)
    )
    return { prev }
  },
  onError: (err, id, ctx) => {
    queryClient.setQueryData(['orders'], ctx.prev)
    toast.error('操作失败，已恢复原状态')
  },
  onSettled: () => queryClient.invalidateQueries({ queryKey: ['orders'] })
})
```

---

## 6. 中国本地化清单

### 字体栈

```css
/* 全局字体栈，优先系统字体 */
font-family:
  "PingFang SC",         /* macOS / iOS */
  "HarmonyOS Sans SC",   /* 华为设备 */
  "Hiragino Sans GB",    /* macOS 旧版后备 */
  "Microsoft YaHei",     /* Windows */
  "Noto Sans SC",        /* Linux / 跨平台后备 */
  -apple-system,
  BlinkMacSystemFont,
  sans-serif;
```

使用 `next/font` 加载 Noto Sans SC 作为可控字体（避免 FOUT），同时提供系统字体回退。CJK 文本行高设为 **1.6-1.8**，比英文宽松，避免中文阅读疲劳。[来源](https://www.az-loc.com/choose-best-chinese-fonts-for-websites/)

### 数字与金额格式

| 场景 | 格式 | 示例 |
|------|------|------|
| 普通金额 | 千分位 + 两位小数 | ¥1,234,567.89 |
| 大额整数金额 | 中文大写（打印/导出） | 壹百贰拾叁万肆仟伍佰元整 |
| 数量 | 整数千分位 | 12,345 件 |
| 百分比 | 一位小数 | 23.4% |
| 表格中金额 | `tabular-nums` 等宽 | 防列跳动 |

```ts
// utils/format.ts
export const formatCNY = (amount: number) =>
  new Intl.NumberFormat('zh-CN', {
    style: 'currency', currency: 'CNY', minimumFractionDigits: 2
  }).format(amount)

export const toCNYCapital = (amount: number): string => {
  // 中文大写金额转换，用于打印单据
  // ... 实现省略，使用 nzh 库
}
```

### 日期格式

| 场景 | 格式 | 示例 |
|------|------|------|
| 列表/表格 | YYYY-MM-DD | 2026-04-23 |
| 详情页 | YYYY年M月D日 | 2026年4月23日 |
| 含时间 | YYYY-MM-DD HH:mm | 2026-04-23 14:30 |
| 相对时间 | X天前 / X小时前 | 3天前 |

使用 `dayjs` + `dayjs/locale/zh-cn` 处理所有日期格式，不依赖浏览器本地化。

### 表格列宽适配

中文字符宽度是英文字母的约 1.5-2 倍。列宽设计时：
- 商品名称列：min-width 200px（中文名称通常 4-10 个汉字）
- 状态 Badge 列：min-width 80px（"待确认"比"Pending"宽）
- 备注列：flex 1 自适应，文字截断 + tooltip

### 图标库补充

Lucide React 覆盖大部分场景。以下场景需关注：
- 微信/支付宝支付图标：使用 SVG inline（Lucide 无内置）
- 中国特色单据图标（印章、盖章）：自定义 SVG
- 建议不引入完整 Ant Design Icons（包体积过大），按需 SVG inline

---

## 7. 性能预算与优化清单

### 性能预算目标

| 指标 | 目标 | 测量工具 |
|------|------|---------|
| LCP (首屏最大内容) | < 1.5s | Lighthouse / Vercel Analytics |
| FID / INP | < 100ms | Web Vitals |
| CLS | < 0.1 | Lighthouse |
| 路由切换 | < 100ms 感知 | 自测 |
| 大表格滚动 | 60 FPS | Chrome DevTools |
| JS Bundle (首屏) | < 150kb gzip | Bundle Analyzer |

### 渲染策略原则

```
Server Component (RSC) 用于:
  - 数据列表页（不需要交互的初始渲染）
  - 报表页骨架
  - SEO 相关内容（如有）

Client Component ("use client") 用于:
  - 表格（TanStack Table 依赖浏览器状态）
  - 图表（Recharts/Tremor 使用 ResizeObserver）
  - 表单（React Hook Form）
  - 动效组件（Framer Motion）
  - Command Palette
  - AI 对话面板
```

### 大表格虚拟滚动

```ts
// 超过 1000 行启用 TanStack Virtual
import { useVirtualizer } from '@tanstack/react-virtual'

const rowVirtualizer = useVirtualizer({
  count: rows.length,
  getScrollElement: () => tableContainerRef.current,
  estimateSize: () => 48, // 固定行高 Regular 档
  overscan: 10,
})
```

aria-rowcount 必须设置，否则屏幕阅读器无法感知总行数。[来源](https://medium.com/@ashwinrishipj/building-a-high-performance-virtualized-table-with-tanstack-react-table-ced0bffb79b5)

### 字体优化

```ts
// app/layout.tsx
import { Noto_Sans_SC } from 'next/font/google'
const notoSansSC = Noto_Sans_SC({
  subsets: ['chinese-simplified'],
  weight: ['400', '500', '600', '700'],
  display: 'swap', // 避免 FOUT
  preload: true,
})
```

### 图片优化

- 所有图片（商品图）使用 `next/image`，自动 WebP 转换 + lazy loading
- 商品缩略图尺寸：列表 48x48，详情 200x200，统一从 MinIO 取

### 路由预取

```tsx
// 侧边栏所有链接
<Link href="/products" prefetch={true}>商品管理</Link>

// 列表行（hover 时预取详情）
<Link href={`/products/${id}`} prefetch="intent">
```

---

## 8. 暗黑模式方案

### 主题配置

使用 shadcn/ui **Zinc** 色板作为暗黑主题基础（Linear 风格中性灰），OKLCH 色彩空间确保亮度感知均匀（Tailwind CSS v4 默认）。[来源](https://ui.shadcn.com/docs/theming)

```css
/* app/globals.css */
:root {
  --background: oklch(1 0 0);           /* 白色 */
  --foreground: oklch(0.145 0 0);       /* 几乎黑 */
  --primary: oklch(0.205 0 0);
  --primary-foreground: oklch(0.985 0 0);
  --muted: oklch(0.961 0 0);
  --muted-foreground: oklch(0.556 0 0);
  --border: oklch(0.922 0 0);
  --ring: oklch(0.708 0 0);
  /* 语义色 */
  --success: oklch(0.60 0.15 142);      /* 绿 */
  --warning: oklch(0.75 0.15 85);       /* 黄 */
  --danger: oklch(0.55 0.20 27);        /* 红 */
}

.dark {
  --background: oklch(0.145 0 0);       /* 深灰，非纯黑 */
  --foreground: oklch(0.985 0 0);
  --primary: oklch(0.922 0 0);
  --muted: oklch(0.245 0 0);
  --muted-foreground: oklch(0.708 0 0);
  --border: oklch(0.268 0 0);
  --ring: oklch(0.439 0 0);
}
```

注意：暗黑模式背景**不用纯黑**（`#000000`），用 zinc-900 近似值（`oklch(0.145)`），参考 Linear 做法。[来源](https://blog.logrocket.com/ux-design/linear-design/)

### next-themes 接入

```tsx
// app/layout.tsx
import { ThemeProvider } from 'next-themes'

export default function RootLayout({ children }) {
  return (
    <html lang="zh-CN" suppressHydrationWarning>
      <body>
        <ThemeProvider
          attribute="class"
          defaultTheme="system"
          enableSystem
          disableTransitionOnChange
        >
          {children}
        </ThemeProvider>
      </body>
    </html>
  )
}
```

`disableTransitionOnChange` 防止切换时整页闪烁；`suppressHydrationWarning` 防止服务端/客户端 class 不一致警告。

### 图表暗黑适配

Tremor 和 Recharts 的图表需要手动传入颜色值，不会自动响应 CSS 变量。方案：

```ts
const useChartColors = () => {
  const { theme } = useTheme()
  return theme === 'dark'
    ? { text: '#e4e4e7', grid: '#3f3f46', ... }
    : { text: '#18181b', grid: '#e4e4e7', ... }
}
```

---

## 9. 推荐 npm 依赖清单

| 包名 | 版本约束 | 用途 |
|------|---------|------|
| `next` | `^14.2.x` | 框架核心 |
| `@radix-ui/react-*` | 跟 shadcn 锁定 | Headless 组件原语 |
| `tailwindcss` | `^4.x` | 样式（OKLCH 支持） |
| `framer-motion` | `^11.x` | 动效 |
| `@tanstack/react-table` | `^8.x` | 表格逻辑 |
| `@tanstack/react-virtual` | `^3.x` | 虚拟滚动 |
| `@tanstack/react-query` | `^5.x` | 数据请求 / 缓存 |
| `zustand` | `^4.x` | 全局轻量状态 |
| `react-hook-form` | `^7.x` | 表单状态 |
| `zod` | `^3.x` | Schema 校验 |
| `cmdk` | `^1.x` | Command Palette 原语 |
| `sonner` | `^1.x` | Toast 通知 |
| `next-themes` | `^0.4.x` | 暗黑模式切换 |
| `recharts` | `^2.x` | 图表（Recharts） |
| `@tremor/react` | `^3.x` | Dashboard 组件 |
| `dayjs` | `^1.x` | 日期处理 |
| `nzh` | `^1.x` | 中文大写金额转换 |
| `react-countup` | `^6.x` | KPI 数字动画 |
| `react-to-print` | `^3.x` | 打印视图控制 |
| `lucide-react` | `^0.400.x` | 图标库 |
| `@tanstack/react-query-devtools` | `^5.x` | 开发调试（仅 dev） |

---

## 10. 风险与反直觉陷阱

### R1: 键盘优先对目标用户是双刃剑

目标用户（从管家婆迁移的中小企业操作员）可能从未知道 ⌘K 是什么。Command Palette 必须通过以下方式让它**被发现**：搜索框 placeholder 写 `按 ⌘K 打开命令面板`；首次使用提示 tooltip；而非假设用户会主动探索。如果 Command Palette 只有 5% 用户用到，也没关系——它是给 Power User 的加速器，不是门槛。

### R2: 动效过多会让低配 Windows 电脑卡顿

中国中小企业普遍使用低配 Windows PC（i5 4 核 8G）。Framer Motion 的 transform 动画通常无问题（GPU 加速），但复杂的 blur/filter 动画（如毛玻璃效果）会触发重绘，在这类机器上明显卡顿。**原则：只用 transform（translate/scale/opacity），不用 filter/blur/backdrop-filter。**使用 `prefers-reduced-motion` media query 让用户可以全局关闭动效。

### R3: Slide-over Sheet 在小屏幕上的体验退化

600px 宽的 Sheet 在 1280px 显示器上占比 46%，在 1366px 宽屏（国内仍常见分辨率）上仍可接受。但在 1024px 屏幕上（部分旧笔记本）会遮挡大部分内容。方案：屏幕宽度 < 1200px 时，Sheet 改为全屏 Dialog（`<Dialog>` 替代 `<Sheet>`）。使用 `useMediaQuery` 动态切换。

### R4: 中文信息密度超过英文 UI 的假设

中文字符信息密度高于英文，但中文 UI 组件（特别是 Badge、Tab、Button）宽度需要比英文版预留更多空间。"待确认"比"Pending"宽，"已部分收货"比"Partial"宽 3 倍。不要从英文原型直接套用列宽和按钮尺寸——必须用真实中文内容测试布局是否破裂。

### R5: 乐观更新在进销存财务场景需要谨慎

进销存数据有财务意义（库存价值、应收账款）。乐观更新适合状态变更（确认收货/取消采购），但不适合涉及金额计算的操作（成本核算、期末盘点）。后者必须等待服务端确认再更新 UI，否则用户会看到临时错误的财务数字。在实现层面：mutation 的 `onSettled` 必须 invalidate 相关 queries，不能只依赖乐观更新的本地状态。

### R6: Tremor 和 Recharts 的图表需要显式处理 "use client"

Next.js 14 App Router 默认服务端组件，Recharts 使用 `ResizeObserver` 和 `window` 对象，必须用 `"use client"` 标记包含图表的组件。不标记会导致服务端渲染崩溃。建议将所有 Chart 组件统一放入 `components/charts/` 目录，该目录下所有文件都加 `"use client"`，避免遗漏。[来源](https://www.tremor.so/docs/getting-started/installation/next)

### R7: 虚拟滚动与表格行内编辑存在冲突

TanStack Virtual 只渲染可见行。如果在虚拟滚动表格中做行内编辑，滚动后编辑状态的行可能被卸载，导致未保存的输入丢失。解决方案：行内编辑触发时，将该行的 edit state 提升到表格级别 store（Zustand），而非行组件本地 state；或者行内编辑时临时锁定滚动位置。

---

## 11. 引用

- [Linear UI 重设计](https://linear.app/now/how-we-redesigned-the-linear-ui) — LCH 色彩空间、Inter Display、设计-工程配对
- [Linear 设计趋势分析](https://blog.logrocket.com/ux-design/linear-design/) — 线性设计哲学、暗黑模式
- [Stripe Apps 设计模式](https://docs.stripe.com/stripe-apps/patterns) — 官方 pattern 文档
- [企业数据表格 UX 分析](https://www.pencilandpaper.io/articles/ux-pattern-analysis-enterprise-data-tables) — 行高、对齐、hover 操作
- [Dashboard UX 最佳实践](https://blog.logrocket.com/ux-design/dashboard-ui-best-practices-examples/) — KPI 卡片、信息架构
- [Dashboard 设计模式 2026](https://artofstyleframe.com/blog/dashboard-design-patterns-web-apps/) — 现代 dashboard 通用模式
- [Vercel 新 Dashboard UX 分析](https://medium.com/design-bootcamp/vercels-new-dashboard-ux-what-it-teaches-us-about-developer-centric-design-93117215fe31) — 侧边栏、开发者中心 UX
- [空状态设计规范](https://supabase-design-system.vercel.app/design-system/docs/ui-patterns/empty-states) — 初始/零结果空状态
- [Shopify 库存管理](https://www.shopify.com/retail/inventory-management) — 批量编辑、多仓库、自动化
- [Shopify 订单管理 UX](https://www.linnworks.com/blog/shopify-order-management/) — 订单列表范式
- [Shopify UX/UI 设计趋势 2025](https://netkodo.com/blog/shopify-and-e-commerce-ux-ui-design-strategies-best-practices-and-trends-for-2025) — 电商管理 UX
- [Command Palette UX 模式](https://medium.com/design-bootcamp/command-palette-ux-patterns-1-d6b6e68f30c1) — 可发现性、分组、快捷键
- [Attio CRM UI 模式](https://www.saasui.design/application/attio) — 现代 CRM 设计语言
- [shadcn/ui Next.js 最佳实践](https://insight.akarinti.tech/best-practices-for-using-shadcn-ui-in-next-js-2134108553ae) — 组件目录组织
- [shadcn/ui 主题文档](https://ui.shadcn.com/docs/theming) — CSS 变量、OKLCH
- [shadcn/ui 暗黑模式 (Next.js)](https://ui.shadcn.com/docs/dark-mode/next) — next-themes 接入
- [shadcn Command 组件](https://ui.shadcn.com/docs/components/radix/command) — cmdk 官方封装
- [Framer Motion 缓动函数](https://www.framer.com/motion/easing-functions/) — 标准曲线参考
- [Framer Motion Stagger](https://motion.dev/docs/stagger) — 列表交错动画
- [TanStack Virtual 高性能表格](https://medium.com/@ashwinrishipj/building-a-high-performance-virtualized-table-with-tanstack-react-table-ced0bffb79b5) — 虚拟滚动实现
- [TanStack Virtual 官方文档](https://tanstack.com/virtual/latest) — 官方 API 参考
- [Tremor Dashboard 组件](https://www.tremor.so/) — KPI 卡片、Sparkline
- [Tremor Next.js 安装](https://www.tremor.so/docs/getting-started/installation/next) — "use client" 要求
- [中文字体最佳实践](https://www.az-loc.com/choose-best-chinese-fonts-for-websites/) — PingFang SC、跨平台字体栈
- [CSS 中文字体家族设置](https://github.com/pluwen/cn-css-font-family) — 推荐 font-family 组合
