# Tally 改进计划（2026-10-10）

> 目标：从「技术 8 分 / 商业 1 分」走到第一个付费客户，再到满足升 PROD 门槛（30 天稳定 + 5 客户 + 0 事故 + 真付费）。
> 原则：先让客户第一天不卡，再做采购/销售人员的日常，最后才是锦上添花。AI 熟客记忆冻结，不再加功能。

## 现状打分

| 维度 | 分 | 说明 |
|---|---|---|
| 工程质量 | 8 | Go 全量单测绿、迁移 55、RLS 全表、客户安装包两次实装验收 |
| 功能完整度 | 7 → 7.5 | 本轮修好单据选商品/往来单位；低库存阈值仍无写入口 |
| 上线就绪 | 5 | 只在 STAGE；看板健康检查地址失效；认证域名待定 |
| 商业验证 | 1 | H1–H3 全 pending，截止期 W36 已过；快照 6/30 起全部 inconclusive |

## P0 — 本周：让第一个客户第一天不卡

### 已完成（2026-10-10，工作树未提交）
- [x] 单据「搜索选择，找不到就新建」选择器：商品 / 供应商 / 客户；新建即写入对应档案（`web/components/entity-combobox.tsx`、`entity-pickers.tsx`）
- [x] 采购单补供应商（必填）；销售单客户改为档案选择，草稿模式带 `partner_id`（原先草稿模式会丢客户）
- [x] 采购单商品下拉原先永远为空、销售单商品栏要手填 UUID —— 均已改为选择器
- [x] 单据详情页显示商品名（原显示 UUID）
- [x] 客户档案：后端 `GET/POST /api/v1/customers`、`GET /customers/:id`（落 `tally.partner`）；前端客户列表页 + 侧边栏入口
- [x] 编码自动生成：商品 P / 供应商 S / 客户 C / 仓库 W / 单位 U / 项目 PJ，格式 `前缀+yymmdd+4位`，冲突自动重试（`internal/pkg/codegen`）
- [x] 本地开发登录页增加离线开发登录（仅开发环境可见）

### 待做
- [ ] 快速收银也写客户 `partner_id`（后端 quick checkout 目前只把客户名写进备注）
- [ ] 单据详情页显示供应商/客户名称；客户编辑/删除/恢复接口与页面
- [ ] 低库存预警阈值写入口（产品决策：自动学习 vs 显式配置）
- [ ] 启动顺序：outbox worker 在迁移完成前启动，全新库首次启动报一次「表不存在」
- [ ] 苗木字典 seed 文件 189 种，启动日志只写入 9 行 —— 核实
- [ ] 开发登录把 `profileType` 写死为 `cross_border`，应取租户真实业务类型
- [ ] 前端 `SupplierItem` 用 camelCase（`tenantId/createdAt`），后端返回 snake_case —— 对齐
- [ ] 编码日期用 UTC，北京时间 0–8 点生成的编码日期是前一天 —— 决定是否改东八区（单据号同理）
- [ ] 统一定价：定价页 ¥199/月、PRD 零售 ¥99–300/年、跨境 ¥500–5000/月、H1 ¥3000/年，四处统一为一个
- [ ] 修正看板健康检查地址（`/api/health` 在 STAGE 返回 404）

### 交付形态改造轮（2026-10-15，10/10 工作包落代码，全仓质量门全绿，工作树未提交）

> 依据：治理仓 `doc/decisions/0038-tally-delivery-forms-and-pricing.md`（原定编号 0037 已被占用）。结论：SaaS Web + PWA 为主交付，不换栈不建新端；私有化只诚实化；小程序/原生 App 不做（解冻条件见 ADR）。本轮全部由小模型实现、主控逐项核实（AllowNegative 只来自 pos_offline 内部路径、能力标志落点、Guard nil 安全、迁移 SQL、飞鹅官方文档核对接口地址与签名）。

| 包 | 内容 | 验收 |
|---|---|---|
| M1 手机可用 | `data-table` 加 `mobileCard`（视口单份 DOM，挂载后按 matchMedia 只留一份）+ `data-card.tsx`；库存/商品/客户/供应商/收付款/补货/销售/采购 8 页接入；开单明细抽 `line-fields.tsx` 行卡片（inputMode=decimal、16px）；`modal` 加 `fullscreenOnMobile`；收银手机粘底「合计+结账」条 | vitest 全绿；375px 真机待种子期实测 |
| M2 扫码 | 后端模糊查加 `attributes->>'barcode' = q` 精确命中；条码正则放宽 ≥6 位字母数字；`barcode-scanner.tsx` + `use-scan-input.ts`：BarcodeDetector → `zxing-wasm`（wasm 自托管 `public/zxing/`，已提交进仓库，因 Dockerfile 不跑 postinstall）→ 手输；接入收银搜索、盘点、ProductPicker、商品列表页 | sqlmock + vitest；真机 EAN-13/Code128 三端待测 |
| M3 微信入口 | `wechat-env.ts` + `wechat-open-hint.tsx`（复制链接/在浏览器打开）挂 dashboard/pos/login；安装提示微信分支改文字；登录页「微信登录」按钮由 `NEXT_PUBLIC_WECHAT_LOGIN=true` 显示，透传 `idp_hint`（`NEXT_PUBLIC_WECHAT_IDP_HINT`） | 平台侧微信登录未就绪，按钮默认隐藏 |
| M4 发布与版本 | CI `tags: ['v[0-9]*']` + semver 镜像标签 + Release job；`cliff.toml` + `scripts/release/cut.sh`（不 push）；`prod-render-gate`（PROD 只钉 `v*`，当前 prod overlay 钉 `main-530950a` 会红=预期）；写死域名改配置：`NEXT_PUBLIC_TALLY_API_PUBLIC_URL`、`SSO_CONSOLE_URL`、`auth.ts` 去 issuer 兜底、`config.go` 四个平台 URL 默认空=降级（值搬到 `deploy/k8s/base/configmap.yaml`），通知 HTTP 路径空 URL 返回 `ErrNotifyHTTPDisabled` | gofmt/vet/build/config 与 lifecycle 测试绿；grep 仅剩注释与 sidebar 支持链接 |
| M5 定价 | `docs/pricing.md` 真源（建议 free/¥588/¥1980，待裁决）；`plans.ts` 数字与 caps 同步，`SEED_TRIAL=true` 不动；prd/roadmap/strategy-synthesis 改为指向 | 平台 025 种子仍旧价，需平台 148 改价迁移（ledger 未预留） |
| M6 诚实化 | `deploy/customer/INSTALL.md` 当前形态说明；`.env.customer.example` DEV_MODE 仅排障；治理仓 `lurus.yaml`（surfaces bundle CONDITIONAL、desc Web+PWA、status stage、out_of_scope 改小程序/原生 App）、`_BOARD.md`、ADR 0038、0029 备注、ledger 000060/000061 预留、contracts「交付形态改造接口」、service-status、iter.py 变更记录 | yaml 校验通过 |
| P3 云打印 | `domain/print` 58/80 列排版（golden）；`adapter/print/feie.go`（`/Api/Open/`，`sha1(user+UKEY+stime)`，空账号→nil 降级）；`app/print` + `handler/print` 三接口经 `WithExtraRoutes` 挂载；权限 `print.receipt`/`print.settings`；打印机配置存 `settings->print->printer` 无 migration；前端设置卡、结账自动云打印失败回落浏览器打印、60 秒重打确认、今日结单/销售详情按钮；小票含付款方式（最近付款记录）与收银员（成员显示名）；`docs/ops/cloud-print.md` | Go httptest + golden + handler；vitest；真机飞鹅待种子期 |
| P4a 目录缓存 | `catalog-cache.ts`（IDB `tally-catalog`，每页 500、上限 5000、10 分钟刷新）；断网/NetworkError 时本地搜索并标「缓存价（x 分钟前）」 | vitest |
| P4b 离线台账 | 前端 `checkout-queue.ts`/`use-checkout-queue.ts`/`pending-checkouts.tsx`：仅「请求从未发出」（`NetworkError.sent=false`）入队，`client_ref=<设备8位>-<序号>`，回放串行、409 已存在→done、其它 4xx→failed、网络错/5xx/401→pending 指数退避；退出登录拦截。后端迁移 **000060**（`client_ref`、`occurred_at`、`source` 扩 20、部分唯一索引）、quick-checkout 接受三字段、409 `checkout_already_exists`、`bill_date/pay_date` 按 `occurred_at` 归日、`AllowNegative` 仅 pos_offline、`audit_log` `stock.negative` + `/todo/summary.negative_stock`、`/me.offline_queue_available`、幂等 TTL 24h | Go 全绿（本机无 PG，SQL 未 up）；vitest |
| P4c 离线壳 | `@serwist/next`：`app/sw.ts` 预缓存构建产物 + `/offline` 兜底，`/api/**` NetworkOnly，导航 NetworkFirst 3s；`public/sw.js` 改为构建生成（已 gitignore）；离线小票 `receipt-sheet` 支持本机台账（印 `OFF-xxxx-n`、「离线单，待上传」）；回放成功后按店铺设置补发云打印一次（`cloud_printed`）；今日结单离线视图；收银页预取 `cloud_print_available`/`auto_print` | `bun run build` 成功生成 sw.js（47KB）；飞行模式真机待测 |
| P5 限额影子 | `entitlement.Limit`（60s 缓存，-1 不限，fail-open）+ `Guard`（`PLAN_LIMITS=off|shadow|enforce`，默认 shadow）落团队邀请/加入、建仓、建商品、导入；403 `plan_limit_reached`；`/subscription/state.limits`；订阅页用量表 | Go + vitest；未接入：AI 建商品、示例数据、Shopify 同步 |

**质量门（主控复跑）**：`gofmt -l ./internal` 空（已把并行编辑引入的 CRLF 归一为 LF）、`go vet ./...`、`CGO_ENABLED=0 GOOS=linux go build ./...`、`go test ./...` 全绿；web `bun run lint`、`bunx tsc --noEmit`、`bun run build`、`bun run test` 全绿（`lib/role-requests.test.tsx` 与 build 并行时会因 5s 超时抖动，单独跑 169/169）。

**待负责人裁决（不阻塞落地）**：定价数字与客群；2b-app-tally 归档或保留 FREEZE；微信登录平台侧路径（Zitadel 通用 OAuth vs 中转）；离线负库存「允许为负 + 待办」是否接受；付款日期是否随 `occurred_at` 归日（当前是）；负库存是否计入待办总数（当前计入）。

**本轮新增落地项（并入下方清单，编号续 19–24）**：
19. 迁移 000060 随 000056–059 一起上 R6（先 pg_dump）；上后核 `bill_head_tenant_client_ref_uidx` 存在。
20. 飞鹅：平台注册开发者账号，`FEIE_USER/FEIE_UKEY` 写入 tally secret；网络放行出站 `api.feieyun.cn:80`；用一台 58mm 真机走「设置→云打印机→测试页→结账自动出票→拔线→上线补打一张」。
21. configmap 新键：`PLATFORM_BASE_URL`、`PLATFORM_NOTIFY_URL`、`PLAN_LIMITS=shadow`、`FEIE_API_URL`（base 已加；stage overlay 若覆盖 configmap 需同步）。
22. 离线真机验收：飞行模式打开 `/pos` → 扫码 2 笔结账并打离线小票 → 恢复网络自动上传 → 今日结单 2 笔不重复 → 库存不足那笔变负并出现在待办「负库存」→ 隔天重放不重复。
23. 发布：15 轮 + 本轮合入后打 `v0.1.0`（`scripts/release/cut.sh v0.1.0`，需安装 git-cliff），推送由负责人；`v1.0.0` 留给首个付费客户 + PROD。
24. 手机/扫码真机：375px 过库存/商品/开单/盘点/收银；EAN-13 与 Code128 在 Chrome Android、iOS Safari、微信内各扫一遍（微信内摄像头权限单独测）。

### 第一轮多 agent 改进（2026-10-10，已验收，工作树未提交）
- [x] 审核不再 500：无仓库自动用默认仓；单位无换算返回 422
- [x] 采购/销售单列表接口：时间区间（Asia/Shanghai）、往来单位、状态、收付款状态、关键字、只看我的，返回往来单位名与汇总
- [x] 客户按名称+手机号查重（409），选择器同名直接选中已有档案
- [x] 请求被主动取消时不再误报「网络异常」；开发登录留空租户自动用已有租户与真实业务类型
- [~] 金额正确性（超额实收、找零、重复审核、外币汇率）——外币草稿编辑仍按 1:1，第二轮返修
- [~] 导出按筛选、列表页筛选与汇总、开单页仓库选择与幂等重试、收付款页服务端筛选——第二轮返修
- 全仓质量门：Go 全绿；前端 tsc/lint 零问题，vitest 327/327

### 第二轮（2026-10-10，14/14 验收通过，工作树未提交）
- [x] R1–R8 遗留问题全部返修（外币草稿汇率、搜索转义、导出筛选同口径、列表页时间预设/汇总/导出、开单页仓库选择与幂等、收付款页服务端筛选、客户查重细节）
- [x] 退货审核修复（migration 000056，stock_movement 引用类型加 return）；退货无仓库用默认仓
- [x] 收银台错误映射 422；AI 未配置返回 503 `ai_not_configured`，前端友好空态
- [x] 草稿卸载/切页落盘，按租户+用户隔离并迁移旧 key
- [x] 发票登记第一期（migration 000057）：登记/列表筛选/作废/红冲，单据列表与导出带开票状态并可筛选，`/invoices` 页面与单据详情关联
- 质量门：Go 107 包全绿；前端 tsc/lint 零问题，vitest 384/384；隔离副本生产构建通过

### 第三轮（2026-10-10，7/8 验收通过，工作树未提交）
- [x] 商品售价落库（默认 SKU），收银台带价、AI 改价真正生效
- [x] 退货入库成本取原出库成本；Shopify 取消/退款封顶与并发去重
- [x] 销售退货接口（按行封顶、冲减应收）与详情页退货对话框
- [x] 应收应付按往来单位汇总 + 账龄，客户/供应商欠款列
- [x] 待办汇总接口、首页「今日要事」、导航红点，失败不再误报「通关」
- [x] 开单带出上次成交价、复制为新单、单位下拉只列可用单位
- [x] 幂等键绑定请求指纹（同 key 不同内容 422）、导出修复、单号东八区、发票按分取整
- [~] AI 录单数字归一与规则校验、小模型按任务路由（默认关）——数量对调与改口漏检两处阻断，第四轮修
- 质量门：Go 108 包全绿；前端 vitest 465/465；隔离副本生产构建通过

### 第五轮（7/8 通过）
苗木能注册（000058）、钱和库存四处修正（采购退货按原单价、退款计入退款当天、盘点空值拦截与零账面按进价入库、拒绝赊账当实收）、打印补齐、月底对账页、中文报错防回退、收银模式、AI 确认闭环。清除示例的报错「处理办法」到不了界面，第六轮修。质量门：Go 114 包、vitest 731/731、生产构建通过。

### 第十五轮（6/6 通过；收钱链路补漏，全部小模型执行）
独立评审（10-14）判定代码可交付种子客户，但到期只读有两处该先修的点，故开本轮：
- **V1**：Shopify 管理路由组补挂到期守卫（原先漏挂，到期后仍可新增/解绑店铺）；新增护栏测试 `router/subscription_guard_routes_test.go`：/api/v1 下任何写路由要么在白名单，要么锁定时必须返回 402，含反例子测试。Shopify 订单 webhook 到期后仍入库为有意豁免（主控代定，种子期保住外部已发生的销售），已写进注释与契约。主控另给 auditGroup 挂了同一守卫。
- **V2**：续费即时恢复：`Invalidate`、`GET /subscription/state?refresh=1`（每店 10 秒最多刷一次，响应带 refreshed）、订阅成功后清缓存、平台出错时旧锁定最多沿用 30 分钟、同店并发查询合并；横幅加「我已续费，刷新状态」，支付回跳自动刷新一次。
- **V3**：收银台挂到期横幅，结算/退款按钮禁用并提示；SEED_TRIAL 下「升级套餐」「去订阅页」「充值」残留入口隐藏，订阅页放回「当前套餐」标记。
- **V4**：全量导出中途出错写「导出失败.txt」，说明.txt 末行「完整性：成功/失败」，错误日志带租户与 request id；采购明细单价两位小数。
- **V5**：`backup-freshness.sh` 过期或失败时可通知飞书（`FEISHU_WEBHOOK_URL`，`--dry-run`），runbook 增加定时执行一节，落地清单第 18 条（Netdata 告警由运维配置）。
- **V6**：客户导入区分「名称重复」与「手机号重复」（`skipped_by_name/skipped_by_phone`）；新增 `docs/ops/support-restore.md` 客服找回手册（只读 SQL + restore 接口）。
质量门：Go 全绿；vitest 178 文件 / 1381 用例；隔离副本生产构建通过；自编译后端实测 402、refresh、导出完整性通过。
非阻断遗留：Invalidate 与进行中的平台查询有极小竞态（最长再沿用 5 分钟，可手动刷新）；支付回跳只自动刷新一次；`json_escape` 未处理制表符；横幅文案与规格原文措辞不同。
评审给出、需负责人或平台侧决定的收入缺口（未做）：种子期后的付费理由（员工数/仓库数/商品数限制，需平台下发权益键）；店主付款记录与发票入口（需平台新增按账号的内部订单接口）；一人多店与内部客服接口（规模化前再定）。
**主控结论：收钱链路的代码侧缺口已补完，停止功能迭代，转入落地（落地清单 1–18）。**

### 第十四轮（7/7 通过；主题：收钱链路、数据进出、可观测、可安装、备份）
负责人 10-14 要求继续推进并自行决策，本轮清单由主控按「离持续收入还差什么」定：
- **W1 套餐到期只读**：订阅状态 expired/cancelled/suspended 时后端拦住所有写操作（402 `subscription_expired`，中文提示），查看、导出、登录、本人资料、加入店铺等白名单放行；平台不可用或无订阅绝不锁；每店缓存 5 分钟；新增所有成员可读的 `GET /api/v1/subscription/state`，前端常驻横幅（老板「续费」、其他角色「请联系老板续费」）。开发桩 `TALLY_DEV_SUBSCRIPTION_STATUS=expired`。
- **W6 操作记录**：`GET /api/v1/audit`（新权限 `audit.view`：老板、店长、财务），设置 → 操作记录页，采购/销售详情页折叠区；动作、对象全部中文，操作人按成员名显示。
- **W2 一键导出全部数据**：`GET /api/v1/exports/all.zip`（report.view），11 个中文表头 CSV + 说明.txt，逐表分页流式；无 cost.view 的角色在 SQL 层就不读成本列；账户中心「导出全部数据」卡片。
- **W3 客户、供应商表格导入**：模板下载（带 BOM）、GBK 自动识别、预览、同名（全角半角归一）跳过、行级中文错误；公共组件 `csv-import-dialog.tsx`；店员可导。
- **W4 前端错误上报**：页面崩溃、未捕获异常、接口 5xx 经 `web_error` 事件进后端日志（带租户、路由、request id），60 秒去重，每租户每分钟 30 条限流；代理透传 X-Request-Id。
- **W5 可安装到手机桌面**：manifest、图标（脚本生成、无新依赖）、最小 service worker、账户抽屉「安装到桌面」；middleware 放行 manifest/sw/icons 并有回归断言。
- **W7 备份演练**：`scripts/ops/backup-freshness.sh`、`scripts/ops/backup-restore-drill.sh`（本地 PG 真跑一次：版本 59、行数与源库一致）、`docs/ops/backup-runbook.md`。发现并写进手册：按 schema 导出的备份不含 CREATE SCHEMA 和扩展（pgcrypto、vector、pg_trgm 装在 tally schema），恢复前要先建；演练账号须为超级用户；RLS 下非超级用户查询会静默 0 行。
质量门：Go 全绿；vitest 173 文件 / 1369 用例；隔离副本生产构建通过；自编译后端对本地 PG 实测 402/导出/审计/导入通过。质量门顺手把此前各轮 agent 写成 CRLF 的 708 个文件统一成 LF（HEAD 为 LF，`git diff` 与 `--ignore-cr-at-eol` 结果一致，没有引入行尾噪音）。
事故：本轮中途子代理配额用尽，W2/W5 验收与 W3 实现中断，用小模型续跑（14b）完成。
非阻断遗留（等试用反馈后排）：导出中途出错时客户拿到不完整 zip 且无提示；采购明细「单价」列格式与其他金额列不一致；客户导入手机号重复按「跳过」计数但文案只说名称重复；`middleware.ts` 的 icons 为前缀匹配；收银台（POS）布局未挂到期横幅，收银只会收到一次 402 提示；缓存过期瞬间无 single-flight；AI 未开通提示仍写「升级套餐」。
本地 dev 库实测数据待清理：客户「质检客户甲」「质检客户乙」、`w3v-` 前缀的客户与供应商、数字 subject 880101 开的零售店及其成员 880102。
产品码核实：后端用 `tally`，平台第 100 号迁移已把 `lurus-tally` 归一为 `tally`，一致。

### 第十三轮（5/5 通过；复审判定代码层面可试用，停止功能迭代、转入落地）
种子客户说明与契约文字改正；今日结单在「全店」下按收银员拆分（老板、店长可看，收银仍只看本人）；种子期订阅页不收费（`web/lib/billing/plans.ts` 的 `SEED_TRIAL=true`，横幅「试用期全部功能免费，正式收费前会提前通知」），计费报错改中文；共用设备改为退出成功后才清痕迹、发广播；文案与口径统一（店员仪表盘显示今日销售、hybrid 店不能邀请收银、苗木字典去掉无用的「默认单位」列）。说明终稿（第 45 行退出失败行为、手机端列表说明、交付前填写提示）由主控完成。质量门：Go 全绿、vitest 1334/1334、隔离副本生产构建通过。
未做：第十二、十三轮后端改动没在真实库上跑过——本机 WSL 卡死、PG 不响应（按规定由负责人重启 WSL），待恢复后或在 R6 UAT 时补测（按收银员拆分含代退、hybrid 邀请收银 422、`invalid_role` 中文提示、店员 `/todo/summary` 的今日销售、Y8 团队路径脚本）。
留到试用反馈后再排（P2）：退出时用 `getSession()` 确认会话已结束并防连点；`/api/auth/session` 去掉 accessToken；收银员列表显示退款、销售单列表按开单人过滤；「升级套餐」「去订阅页」「充值」等种子期残留入口与「当前套餐」标记；`rewriteFailed` 清掉 X-Request-Id。

### 第十二轮（8/8 通过；质量门由主控补跑全绿）
角色按店铺类型过滤（苗木店不能邀请收银）、后台与打印页响应退出广播（退出失败可恢复购物车）、加入页显示开店店名、护栏与契约收尾、文案与卖点按套餐口径修改、无权按钮隐藏与 403 中文提示、根布局会话不含登录凭证、种子客户使用说明 `docs/seed-customer-guide.md` 与 UAT 用例。质量门：Go 全绿、vitest 1312/1312、隔离副本生产构建通过。
事故与处理：治理仓变更记录脚本默认会 `git add`，第十一轮误暂存了 2 条碎片，已撤回为未跟踪；之后一律带 `--no-git`。本机内存不足导致本地后端（18200）被系统停止，未自动重启。

### 第十二轮（原计划）
角色按店铺类型过滤（苗木店不能邀请收银，最后一处「试用前必须」）；后台与打印页响应退出广播；加入页显示开店店名；护栏与契约收尾；文案与卖点按「种子期不限人数、只写实际执行的限制」改；无权按钮隐藏、403 显示后端中文提示；根布局会话不含登录凭证；种子客户使用说明 `docs/seed-customer-guide.md` 与 UAT 用例。

### 第十一轮（6/7 通过，X8 验收因接口超时未完成，第十二轮补验）
加入页显示当前账号并可换账号、本店邀请链接返回 already_member_here 且不消耗邀请；换人标记 30 天；契约与台账登记（含收款口径）；开放写路由护栏（未登记写路由测试即失败）；店员草稿与文案收尾。质量门：Go 122 包、vitest 1281/1281、生产构建通过。

### 第十一轮（原计划，代码层面最后两处 + 契约登记）
加入页显示当前账号并可换账号、老板打开本店链接不再弹「放弃空店」；换人标记 30 天；契约与台账登记（含收款口径：能看销售单的成员可见收款状态与今日销售额）；开放写路由护栏；店员草稿、文案与测试收尾。

### 第十轮（8/8 通过）
待办与每周摘要按角色裁剪 + 数据驱动的开放读接口护栏（36 条实测无成本泄露）；收银换人收尾；根布局带初始会话；收银交班后端强制本人；单据恢复边角；采购审核与店员草稿体验；文案去术语；开发工具与测试补齐。质量门：Go 全绿、vitest 1244/1244、生产构建通过。

### 第十轮（原计划）
待办与每周摘要按角色裁剪（第九轮复审发现店员在「待办」能看到全店采购金额）+ 数据驱动的开放读接口护栏测试；共用收银机换人收尾；根布局带初始会话；收银交班后端强制本人；单据恢复边角；采购审核与店员草稿体验；文案去术语；开发工具与测试补齐。

### 第九轮（7/8 通过；V1 收银换人需上 R6 实测）
零单价采购审核拦截（422 列出商品）、收银交班按人、店员盘点无进价商品提示、身份解析统一、AI 关闭时不露入口并清除代码中模型名、团队与平台边角（并发返回 410）、员工页面 403 尾巴。质量门：Go 全绿、vitest 1209/1209、生产构建、平台测试全绿。

### 第九轮（原计划）
共用收银机换人（退出时清购物车并强制重新输入账号）；零单价采购草稿审核拦截（422，列出商品，可按赠品确认）；收银交班按人；店员盘点无进价商品的提示；剩余 4 处身份解析统一；AI 关闭时不露入口、代码中清除模型名；团队与平台边角（并发解散返回 410、已解散组织过滤）；员工页面最后几处 403。

### 上线落地清单（负责人决定上线时执行；汇总自第六至八轮复审）
1. **分支与 PR**：Tally 按主题拆分（迁移 000056–58+退货发票 / 第五六轮体验 / 团队与权限 000059+各轮 / AI），各用 `_worktrees/<名>` 逐路径 stage；`go.mod` 有他人改动，先与其负责人协调。平台从 `_worktrees/platform-members` 提 147 及加固、解散接口。
2. **迁移顺序**：平台 146（他人分支）→ 147 先上 R6 并确认 RLS 生效；再 Tally 000056→000059（上前 pg_dump 备份 tally schema，确认 version=59 且未 dirty）。
3. **平台密钥**：给 Tally 签作用域 key（service_name=tally，带 org:write、account:read），写入 R6 上 Tally 的 secret；用老板账号在团队页实测一次邀请。
4. **AI 关闭**：种子环境置空模型与网关密钥配置（`deploy/k8s/base/configmap.yaml` 中的模型默认值移到需要的 overlay），确认 `/todo/summary` 返回 `ai_enabled=false`、`/ai/chat` 返回 503。
5. **NATS**：确认 Tally 与平台用同一套 NATS，能收到 `identity.org.>` 成员事件。
6. **存量回填**：R6 现有租户的平台组织由老板打开团队页时懒回填，抽查 GetMember 返回 owner。
7. **契约登记**：contracts.md 补平台对内组织接口、成员事件、dissolve；Tally 的 team 接口与错误码、clear-demo、收银模式、收银退货与店员草稿语义、零单价 422、pos-summary mine、payments.csv 新列；lurus.yaml identity 消费者加 tally；service-status、变更记录（`python scripts/iter/iter.py change`）同步；台账状态改为实际。
8. **UAT**：tally-uat 全套；R6 上查 `user_identity_mapping.zitadel_sub` 格式；两个真实收银账号验证换人与「本人当天」退货；店员草稿→老板直接审核被拦；收银班超 8 小时后的登录过期体验；移除成员 60 秒内失效。
9. **清理**：本地验收租户（各轮报告中列出的 30 余个）、dev_org_member/dev_account 测试行、platform_test 库与临时目录。
10. **给种子客户的一页说明**：邀请员工一定用链接；一个账号一个店；收银下班点「退出登录」；收银只退当天自己的小票；店员采购草稿不填价、老板补价后审核；新商品先由老板填进价；AI 试用期不开放。
11. **说明终稿**：以 `docs/seed-customer-guide.md` 为准，交付前填客服联系方式（第 3 行与末尾）；负责人确认线上登录方式后统一说法。
12. **订阅口径**：种子期保持 `SEED_TRIAL=true`，R6 上确认订阅页、定价页没有付款按钮；正式收费前补专业版、企业版的真实卖点并核对 plan_code 与平台 025 种子数据一致。
13. **删除找回**：仓库、供应商、苗木、项目删除后只能按 id 调 `POST /api/v1/{warehouses,suppliers,nursery-dict,projects}/:id/restore` 找回；按负责人决定写客服操作流程（含只读查 id 的 SQL），或加回「已删除」列表与恢复按钮。
14. **备份**：确认 R6 上 tally 的每日备份任务在跑、备份可见，并恢复到临时库演练一次（种子客户数据即真实数据）。
15. **实库补测**：第十三轮 Z2/Z5 仍待补测（本地 PG 已恢复，可随 UAT 一起做）；第十四轮各项已在本地 PG 实测。
16. **到期只读上线前**：Tally 的平台作用域 key 须能调 `GetAccountOverview`（account:read）；在 R6 用一个真实到期账号验证 `/subscription/state`、402 与横幅；老板续费后 5 分钟内恢复。
17. **备份演练**：按 `docs/ops/backup-runbook.md` 在 R6 跑一次 `backup-freshness.sh` 与 `backup-restore-drill.sh latest`（演练账号须为超级用户，mc 别名以 `mc alias ls` 为准）。另：`CLAUDE.md` 第 3 行「Migration head 55」改为 59；清理第十二轮实测数据（y3v-、y4v-、Y5验收仓、PO-20261012-0054/0055/0056、SL-20261012-0039/0040、RT-20261012-0019、y7v-、uat-y8-v18507-、v2-、y8v-）。
18. **告警**：就绪探针失败、5xx 率、迁移失败由运维在 Netdata 配置告警，Tally 业务代码不改；备份新鲜度失败的飞书通知见 `docs/ops/backup-runbook.md`「每日定时执行与飞书通知」。

### 第八轮（8/8 通过）
创建人身份稳定化（数字登录编号派生稳定 UUID）、收银台入口按角色与退出登录、员工零 403（权限表驱动夹具）、平台解散空店组织、采购草稿改单位折价与外币强制 CNY、示例转正尾巴、销售单撤销取消、契约与收银 PIN 收尾、配置默认值去模型名。质量门：Go 122 包、vitest 1144/1144、生产构建、平台 PG 集成 8/8 全绿。

### 第八轮（原计划）
创建人身份稳定化（非 UUID 的登录编号派生稳定 UUID，保证「本人」判断在线上成立）；收银台入口按角色、收银台内可退出登录；员工零 403（由权限表驱动的前端夹具）；平台解散空店组织；采购草稿改单位折价、外币强制 CNY；示例转正尾巴；销售单撤销取消、报表截至日期；契约与收银 PIN 收尾；配置默认值去掉模型名。

### 第七轮（5/7 通过）
后端越权收口（PAT 仅老板、店员仅本人采购草稿无金额、收银仅退当天本人小票、老板不因平台异常被清店）、平台加固（破窗 key、并发接受幂等、用户面成员事件）、清除示例闭环（示例转真实商品）、财务对账尾巴、收银模式只在本设备生效、员工加入与放弃空店。遗留：登录编号非 UUID 时「本人」判断失效、侧栏收银入口未按角色、放弃空店后平台组织未解散——第八轮修。质量门：Go 121 包全绿、vitest 1008/1008、生产构建通过、平台 PG 集成测试 76/76。

### 第七轮（原计划）
前端按角色收口（无 403 入口、非老板不见计费、店员不见进价、收银直达收银台）；收银模式只在本设备生效；员工加入不走死胡同 + 放弃误开空店；后端越权收口（PAT 仅老板、店员盘点不写成本、店员仅见本人采购草稿且无金额、收银只退当天本人小票、老板不因平台异常被清店）；平台加固（破窗 key 判定、并发接受幂等、成员事件全覆盖、PG 集成测试实跑）；清除示例闭环；财务对账尾巴。AI 种子期关闭。

### 第六轮（7/10 通过，阻断项已在质量门修复）
报错最后一段、金额到分、收付款可对账、苗木对齐、收银加固、补打小票与补记退款；**多成员与角色第一期**（平台 147 + Tally 000059）：邀请/加入/改角色/移除、权限矩阵与成本裁剪，多成员全流程实测 28/28。质量门：Go 全绿、vitest 868/868、生产构建通过；平台工作树 build/vet/相关测试通过。

### 第六轮（原计划）
报错最后一段（显示处理办法、500 中文）、金额统一到分、收付款明细可对账、苗木卡片与能力对齐、收银模式加固、补打小票与补记退款、AI 收尾；**多成员与角色第一期**：平台 migration 147（成员产品角色 + 邀请 + 对内写接口 + 成员事件，独立工作树 `_worktrees/platform-members`），Tally 000059（店对应平台组织、团队接口、邀请加入、权限矩阵与成本裁剪、团队成员页）。契约见 `doc/decisions/2026-10-10-tally-members-and-roles.md`。

### 第四轮（「试用前必须」）
收付款闭环（采购可付款、收款后显示正确）；期初建账（商品 CSV 导入含 GBK、期初库存、盘点）；收银台可上岗（条码、赊账绑定客户、幂等、今日结单）；打印（小票、送货单、入库单）；新手路径真实化与去术语、默认日期北京时间；按客户整笔核销与对账单；草稿可改、采购退货；AI 两处阻断与服务端逐行确认。

### 负责人决定（2026-10-10）
- **多成员账号 → 平台先开放成员与角色**，Tally 消费；收银模式保留为界面简化，后续与角色联动。设计调研进行中。
- **暂不提交**：继续多轮改进，工作树保持未提交，待负责人决定落地时机。

### 待负责人决定
- **上线前核实**：改动前已导入的 Shopify 订单，之后来的退款会跳过回库——需查 STAGE 上是否有已绑定店铺。

## P1 — 两周：采购 / 销售人员的日常

### 报表（按时间区间）
- [ ] 采购明细 / 销售明细：时间区间 + 往来单位 + 状态筛选，含收付款状态（已收/部分/未收/逾期）与发票状态列
- [ ] 收付款汇总：按往来单位的应收应付与账龄
- [ ] 导出 Excel
- 依赖：单据列表接口加 `date_from/date_to/partner_id/status/payment_status`；发票状态依赖下方发票模块

### 发票管理（E29）
结论：**Tally 自建，不复用 platform 的发票表**。platform 的发票是 Lurus 作为唯一卖方开给订阅用户的，一单一票、只有销项；Tally 是租户作为卖方/买方，要分进项销项、多单合票/一单拆票、联动应收应付，且每个租户有自己的开票主体。
- [ ] `tally.invoice`（进项/销项）+ 单据关联（多对多）+ 状态 待开/已开/已红冲/作废 + 票号唯一 + 文件（PDF/OFD/XML）存储
- [ ] 第一期做「登记型」：线下在税局开票，系统登记票号并上传文件（与 platform 现状一致）
- [ ] 往来单位开票抬头/税号（`tally.partner.tax_no` 已有列，未映射）
- [ ] 可复用：platform 的 PDF 渲染（`invoicedoc`）与票据文件校验思路
- [ ] 开票服务商（诺诺/百望/乐企）对接：与 platform 一起选型，做成共享适配层，两边各为调用方

### 语音录入
实测结论（30 句苗木/五金口述、Tally 苗木字典 189 种 + 客户名，合成语音，留出 20 句只跑一次）：

| 方案 | 商品/客户名准确率（安静 / 加噪） |
|---|---|
| SenseVoice 不纠错 | 62% / 59% |
| **SenseVoice + Tally 词库拼音纠错** | **100% / 98.5%**，0 误造 |
| whisper-medium（转简体）不纠错 | 41% / 41% |
| whisper-medium + 拼音纠错 | 95.5% / 93.9% |
| Handy 自带的自定义词纠错 | 与不纠错相同（按空格切词 + Soundex，对中文无效） |
| 给 whisper 预提示词库 | 原始准确率无提升，且会误造原句没有的品种名 |

- [ ] 不集成 Handy 本体（桌面应用，零售店主主要用手机）；用户若在收银电脑上想用，可自行安装当输入法，零代码
- [ ] 借鉴其流水线：浏览器录音 → 后端识别（SenseVoice，CPU 一句约 0.1 秒）→ **按租户商品/客户/供应商词库拼音纠错** → AI 解析 → 确认卡片
- [ ] 数量/金额必须二次确认：实测出现「各十棵」→「各1棵」
- [ ] 商用前核对 SenseVoice 模型许可证
- [ ] 上线前用真人录音（含方言口音、店内噪声）重测；合成语音结果偏乐观

## P2 — 商业验证（与 P0/P1 并行，优先级最高）
- [ ] 选定主攻客户群（建议苗木：已有专属功能包与字典）
- [ ] 找 3–5 家种子客户试用，每周一对一记录
- [ ] H1–H3 填负责人、重设截止期；快照全 inconclusive 时停掉或改为人工周报
- [ ] 第一笔付费 → 开始 30 天稳定期计时 → 满足门槛后迁 PROD
